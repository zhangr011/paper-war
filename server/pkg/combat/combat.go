package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// CombatSystem handles auto-targeting and damage application using the
// type-aware damage matrix, smart targeting priorities, and cannon splash.
type CombatSystem struct {
	Sh *spatial.Hash

	posPool   *ecs.ComponentPool[component.PositionComponent]
	healthPool *ecs.ComponentPool[component.HealthComponent]
	attackPool *ecs.ComponentPool[component.AttackComponent]
	boidPool   *ecs.ComponentPool[component.BoidComponent]
	ownerPool  *ecs.ComponentPool[component.OwnerComponent]
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent]
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
}

func (s *CombatSystem) Tick(w *ecs.World, tick uint32) {
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

		// Auto-acquire target using smart targeting (4 priority tiers)
		if ac.TargetID == 0 || !s.isTargetValid(e, ac, pos) {
			ac.TargetID = s.findTarget(e, pos, ac.Range, weapon)
		}

		if ac.TargetID == 0 {
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
			ac.TargetID = 0
			return
		}

		// Calculate damage using the damage matrix
		targetHealth, ok := s.healthPool.GetPtr(targetEntity)
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

		// Apply damage to primary target
		targetHealth.HP -= dmg

		// Cannon splash: apply 50% damage to units within 2 tiles
		if weapon == component.WeaponCannon {
			s.applySplash(targetPos, dmg, e, targetEntity)
		}

		ac.LastAttack = tick
	})
}

// isTargetValid checks if the current target is still alive and in range.
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
	dx := targetPos.X - pos.X
	dy := targetPos.Y - pos.Y
	distSq := (dx*dx + dy*dy) >> 12
	rangeSq := (ac.Range * ac.Range) >> 12
	return distSq <= rangeSq
}

// findTarget implements smart auto-targeting with 4 priority tiers:
// 1. Closest enemy Commander in range
// 2. Closest enemy the weapon is strong against (dmg >= 100)
// 3. Closest enemy the weapon can damage (dmg > 0)
// 4. Closest enemy regardless
func (s *CombatSystem) findTarget(attacker ecs.Entity, pos component.PositionComponent, range_ int64, weapon component.WeaponType) uint32 {
	ids := s.Sh.Query(pos.X, pos.Y, range_)
	selfID := uint64(attacker)

	type candidate struct {
		id       uint32
		distSq   int64
		priority int // 1=commander, 2=strong, 3=canDamage, 4=any
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

		// Skip dead targets
		if hp, ok := s.healthPool.Get(entity); !ok || hp.HP <= 0 {
			continue
		}

		tp, ok := s.posPool.Get(entity)
		if !ok {
			continue
		}
		dx := tp.X - pos.X
		dy := tp.Y - pos.Y
		ds := (dx*dx + dy*dy) >> 12

		// Determine priority
		priority := 4 // any enemy
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(entity); ok {
				dmg := component.DamageMultiplier(weapon, ut.Armor)
				if dmg >= 100 {
					priority = 2 // strong against
				} else if dmg > 0 {
					priority = 3 // can damage
				}
			}
		}

		// Check if commander (tier 1)
		if s.boidPool != nil {
			if bc, ok := s.boidPool.Get(entity); ok && bc.Role == component.RoleCommander {
				priority = 1
			}
		}

		candidates = append(candidates, candidate{id: uint32(id), distSq: ds, priority: priority})
	}

	if len(candidates) == 0 {
		return 0
	}

	// Sort by priority (lower is better), then by distance
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.priority < best.priority || (c.priority == best.priority && c.distSq < best.distSq) {
			best = c
		}
	}

	return best.id
}

// applySplash deals 50% damage to enemy units within 2 tiles of the impact point,
// excluding the primary target (by entity ID) and the attacker's own faction.
func (s *CombatSystem) applySplash(targetPos component.PositionComponent, baseDmg int32, attacker ecs.Entity, primaryTarget ecs.Entity) {
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

		// Apply splash damage
		if hp, ok := s.healthPool.GetPtr(entity); ok {
			hp.HP -= splashDmg
		}
	}
}
