package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// CombatSystem handles auto-targeting and damage application using the
// the type-aware damage matrix, smart targeting priorities, and cannon splash.
type CombatSystem struct {
	Sh *spatial.Hash

	posPool      *ecs.ComponentPool[component.PositionComponent]
	healthPool   *ecs.ComponentPool[component.HealthComponent]
	attackPool   *ecs.ComponentPool[component.AttackComponent]
	boidPool     *ecs.ComponentPool[component.BoidComponent]
	ownerPool    *ecs.ComponentPool[component.OwnerComponent]
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent]
	pathPool     *ecs.ComponentPool[component.PathfindingComponent]
	MapW, MapH   int32
	TerrainFn    func(x, y int32) component.TerrainType // tile terrain lookup

	// AttackRecords collects one entry per attack resolution (cooldown
	// edge) this tick.  The snapshot encoder drains it into
	// EventProjectile events so the client can drive the attack
	// animation as a one-shot per shot rather than looping while the
	// AI is in Attack mode.  Cleared at the start of each Tick.
	// Issue #48.
	AttackRecords []AttackRecord
}

// AttackRecord captures the moment a unit resolved an attack. The client
// stamps its render clock on receipt and plays the attack animation once.
type AttackRecord struct {
	EntityID uint32
	Tick     uint32
}

func (s *CombatSystem) Name() string  { return "CombatSystem" }
func (s *CombatSystem) Priority() int { return 80 }

func (s *CombatSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
	if p := w.Pool(component.PathfindingComponent{}); p != nil {
		s.pathPool = p.(*ecs.ComponentPool[component.PathfindingComponent])
	}
}

// pendingDmg accumulates deferred damage for double-buffered combat.
// Damage is collected during entity iteration and applied simultaneously
// after all attacks resolve, eliminating entity-order first-strike bias.
type pendingDmg struct {
	damage   int32
	attacker uint32
}

func (s *CombatSystem) Tick(w *ecs.World, tick uint32) {
	// Double-buffered damage: collect all attacks first, apply simultaneously
	// after iterating all entities. This eliminates entity-processing-order
	// bias (lower entity IDs getting first-strike advantage every tick).
	pending := make(map[uint32]pendingDmg)
	// Clear last tick's attack-fire records — snapshot gen between ticks
	// has already drained them into events. Issue #48.
	s.AttackRecords = nil

	s.attackPool.Each(func(e ecs.Entity, ac *component.AttackComponent) {
		if ac.Cooldown > 0 && tick-ac.LastAttack < uint32(ac.Cooldown) {
			return
		}

		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}

		// Get attacker's weapon type
		weapon := component.WeaponGun // default
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(e); ok {
				weapon = ut.Weapon
			}
		}

		// Auto-acquire target using smart targeting with focus-fire on wounded enemies.
		// Pass currentTarget for hysteresis — findTarget only switches to a new
		// target if it's meaningfully better (lower priority tier, or same tier
		// but wounded while current isn't). Prevents DPS waste from flip-flopping.
		if ac.TargetID == 0 || !s.isTargetValid(e, ac, pos) {
			ac.TargetID = s.findTarget(e, pos, ac.Range, weapon, 0)
			// If no target in attack range, try chase range (2x attack range)
			// so units close the gap instead of standing idle.
			if ac.TargetID == 0 {
				chaseRange := ac.Range * 2
				ac.TargetID = s.findTarget(e, pos, chaseRange, weapon, 0)
			}
		} else {
			// Already have a valid target — opportunistically switch to a
			// meaningfully better one (e.g., a wounded enemy appeared in range).
			// Hysteresis inside findTarget prevents frivolous switching.
			newID := s.findTarget(e, pos, ac.Range, weapon, ac.TargetID)
			if newID != 0 {
				ac.TargetID = newID
			}
		}

		if ac.TargetID == 0 {
			// Ground attack: if GroundTarget is set and weapon is Cannon/Missile,
			// fire at the ground position (deals splash to any unit in area)
			if ac.GroundTargetX != 0 || ac.GroundTargetY != 0 {
				if weapon == component.WeaponCannon || weapon == component.WeaponMissile {
					dx := ac.GroundTargetX - pos.X
					dy := ac.GroundTargetY - pos.Y
					distSq := (dx*dx + dy*dy) >> 12
					rangeSq := (ac.Range * ac.Range) >> 12
					if distSq <= rangeSq {
						groundPos := component.PositionComponent{X: ac.GroundTargetX, Y: ac.GroundTargetY}
						s.collectSplash(groundPos, ac.Damage, e, ecs.Entity(0), pending)
						ac.LastAttack = tick
						s.AttackRecords = append(s.AttackRecords, AttackRecord{
							EntityID: uint32(e),
							Tick:     tick,
						})
					}
				}
			}
			return
		}

		targetEntity := ecs.Entity(ac.TargetID)
		targetPos, ok := s.posPool.Get(targetEntity)
		if !ok {
			ac.TargetID = 0
			return
		}

		dx := targetPos.X - pos.X
		dy := targetPos.Y - pos.Y
		distSq := (dx*dx + dy*dy) >> 12
		rangeSq := (ac.Range * ac.Range) >> 12

		if distSq > rangeSq {
			// Target is out of attack range but still viable — pursue it
			// by setting pathfinding destination. The movement system will
			// close the gap; once in range, the unit attacks normally.
			if s.pathPool != nil {
				if path, ok := s.pathPool.GetPtr(e); ok {
					path.TargetX = targetPos.X
					path.TargetY = targetPos.Y
				}
			}
			return
		}

		// Calculate damage using the damage matrix
		_, ok = s.healthPool.GetPtr(targetEntity)
		if !ok {
			ac.TargetID = 0
			return
		}

		// Get target's armor type
		armor := component.ArmorLight // default
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(targetEntity); ok {
				armor = ut.Armor
			}
		}

		// Check if target is a building (terrain entity)
		// For now, units are always non-building
		dmgMultiplier := component.DamageMultiplier(weapon, armor)
		dmg := ac.Damage * dmgMultiplier / 100
		if dmg < 1 {
			dmg = 1
		}

		// Apply damage to primary target (with stronghold terrain bonus)
		effectiveDmg := dmg
		if s.TerrainFn != nil {
			tx := int32(fixed.ToFloat(targetPos.X))
			ty := int32(fixed.ToFloat(targetPos.Y))
			terrain := s.TerrainFn(tx, ty)
			shLevel := strongholdLevelFromTerrain(terrain)
			if shLevel > 0 {
				bonusPct := StrongholdDefenseBonus(shLevel)
				effectiveDmg = effectiveDmg * (100 - bonusPct) / 100
				if effectiveDmg < 1 {
					effectiveDmg = 1
				}
			}
		}

		// Defer damage application (double-buffered)
		tid := uint32(targetEntity)
		pd := pending[tid]
		pd.damage += effectiveDmg
		pd.attacker = uint32(e)
		pending[tid] = pd

		// Cannon splash: defer splash damage too
		if weapon == component.WeaponCannon {
			s.collectSplash(targetPos, dmg, e, targetEntity, pending)
		}

		ac.LastAttack = tick
		s.AttackRecords = append(s.AttackRecords, AttackRecord{
			EntityID: uint32(e),
			Tick:     tick,
		})
	})

	// Apply all pending damage simultaneously — no entity gets first-strike
	for targetID, pd := range pending {
		if hp, ok := s.healthPool.GetPtr(ecs.Entity(targetID)); ok {
			hp.HP -= pd.damage
			hp.LastAttacker = pd.attacker
		}
	}
}

// isTargetValid checks if the current target is still alive and an enemy.
// Range is NOT checked here — the main loop handles range separately so
// that out-of-range targets can be pursued via pathfinding.
func (s *CombatSystem) isTargetValid(attacker ecs.Entity, ac *component.AttackComponent, pos component.PositionComponent) bool {
	targetEntity := ecs.Entity(ac.TargetID)
	targetHealth, ok := s.healthPool.Get(targetEntity)
	if !ok || targetHealth.HP <= 0 {
		return false
	}
	targetPos, ok := s.posPool.Get(targetEntity)
	if !ok {
		return false
	}
	// Check faction
	if s.ownerPool != nil {
		selfOwner, ok1 := s.ownerPool.Get(attacker)
		otherOwner, ok2 := s.ownerPool.Get(targetEntity)
		if ok1 && ok2 && selfOwner.Faction == otherOwner.Faction {
			return false
		}
	}
	_ = targetPos // position existence verified above; range checked in main loop
	return true
}

// findTarget implements smart auto-targeting with focus-fire on wounded enemies.
//
// Priority tiers (lower = preferred):
//   1. Closest enemy Commander in range
//   2. Closest wounded enemy (HP < 50% max) in range — focus-fire snowball
//   3. Closest enemy the weapon is strong against (dmg >= 100)
//   4. Closest enemy the weapon can damage (dmg > 0)
//   5. Closest enemy regardless
//
// Within a tier, candidates are ranked by distance (closest first).
//
// Hysteresis: if currentTarget is non-zero and still in range, the function
// only switches to a new target if it is meaningfully better — either in a
// lower priority tier, or wounded while the current target isn't. This
// prevents DPS waste from frivolous flip-flopping between near-equivalent
// targets while still allowing the squad to converge on newly-wounded enemies.
func (s *CombatSystem) findTarget(attacker ecs.Entity, pos component.PositionComponent, range_ int64, weapon component.WeaponType, currentTarget uint32) uint32 {
	ids := s.Sh.Query(pos.X, pos.Y, range_)
	selfID := uint64(attacker)

	type candidate struct {
		id       uint32
		distSq   int64
		priority int // 1=commander, 2=wounded, 3=strong, 4=canDamage, 5=any
		wounded  bool
	}

	var candidates []candidate
	for _, id := range ids {
		if id == selfID {
			continue
		}
		entity := ecs.Entity(id)

		// Skip same faction
		if s.ownerPool != nil {
			selfOwner, ok1 := s.ownerPool.Get(attacker)
			otherOwner, ok2 := s.ownerPool.Get(entity)
			if ok1 && ok2 && selfOwner.Faction == otherOwner.Faction {
				continue
			}
		}

		// Skip dead targets and capture HP for wounded-priority classification
		hp, ok := s.healthPool.Get(entity)
		if !ok || hp.HP <= 0 {
			continue
		}
		// Wounded = below 50% of max. Treats fresh units (>=50%) as full-strength
		// so the squad doesn't constantly retarget during the opening volley.
		wounded := false
		if hp.MaxHP > 0 && hp.HP*2 < hp.MaxHP {
			wounded = true
		}

		tp, ok := s.posPool.Get(entity)
		if !ok {
			continue
		}
		dx := tp.X - pos.X
		dy := tp.Y - pos.Y
		ds := (dx*dx + dy*dy) >> 12

		// Determine priority
		priority := 5 // any enemy
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(entity); ok {
				dmg := component.DamageMultiplier(weapon, ut.Armor)
				if dmg >= 100 {
					priority = 3 // strong against
				} else if dmg > 0 {
					priority = 4 // can damage
				}
			}
		}

		// Wounded enemies jump to tier 2 (above strong-against, below commanders)
		if wounded {
			priority = 2
		}

		// Check if commander (tier 1 — highest priority)
		if s.boidPool != nil {
			if bc, ok := s.boidPool.Get(entity); ok && bc.Role == component.RoleCommander {
				priority = 1
			}
		}

		candidates = append(candidates, candidate{
			id: uint32(id), distSq: ds, priority: priority, wounded: wounded,
		})
	}

	if len(candidates) == 0 {
		return 0
	}

	// Pick best by (priority asc, distSq asc)
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.priority < best.priority || (c.priority == best.priority && c.distSq < best.distSq) {
			best = c
		}
	}

	// Hysteresis: if we have a current target, only switch if the new one is
	// meaningfully better. This is what prevents flip-flopping in crowded battles.
	if currentTarget != 0 {
		var current *candidate
		for i := range candidates {
			if candidates[i].id == currentTarget {
				current = &candidates[i]
				break
			}
		}
		if current != nil && current.id != best.id {
			// Switch only if best is in a strictly lower priority tier, OR
			// same tier but best is wounded while current isn't.
			shouldSwitch := false
			if best.priority < current.priority {
				shouldSwitch = true
			} else if best.priority == current.priority && best.wounded && !current.wounded {
				shouldSwitch = true
			}
			if !shouldSwitch {
				return current.id
			}
		}
	}

	return best.id
}

// collectSplash computes 50% splash damage to enemy units within 2 tiles
// of the impact point, excluding the primary target (by entity ID) and the
// attacker's own faction. Damage is added to the pending map for simultaneous
// application (double-buffered combat).
func (s *CombatSystem) collectSplash(targetPos component.PositionComponent, baseDmg int32, attacker ecs.Entity, primaryTarget ecs.Entity, pending map[uint32]pendingDmg) {
	splashRange := fixed.FromFloat(2.0)
	ids := s.Sh.Query(targetPos.X, targetPos.Y, splashRange)
	splashDmg := baseDmg / 2
	if splashDmg < 1 {
		splashDmg = 1
	}
	for _, id := range ids {
		if id == uint64(attacker) || id == uint64(primaryTarget) {
			continue
		}
		entity := ecs.Entity(id)

		// Skip same faction
		if s.ownerPool != nil {
			selfOwner, ok1 := s.ownerPool.Get(attacker)
			otherOwner, ok2 := s.ownerPool.Get(entity)
			if ok1 && ok2 && selfOwner.Faction == otherOwner.Faction {
				continue
			}
		}

		// Check distance from impact point
		tp, ok := s.posPool.Get(entity)
		if !ok {
			continue
		}
		dx := tp.X - targetPos.X
		dy := tp.Y - targetPos.Y
		distSq := fixed.DistSq(dx, dy)
		splashRangeSq := fixed.DistSq(splashRange, 0)
		if distSq > splashRangeSq {
			continue
		}

		// Defer splash damage
		tid := uint32(entity)
		pd := pending[tid]
		pd.damage += splashDmg
		pd.attacker = uint32(attacker)
		pending[tid] = pd
	}
}

// strongholdLevelFromTerrain returns 1-5 for stronghold terrain types, 0 otherwise.
func strongholdLevelFromTerrain(t component.TerrainType) int {
	switch t {
	case component.TerrainStronghold1:
		return 1
	case component.TerrainStronghold2:
		return 2
	case component.TerrainStronghold3:
		return 3
	case component.TerrainStronghold4:
		return 4
	case component.TerrainStronghold5:
		return 5
	}
	return 0
}
