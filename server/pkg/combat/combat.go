package combat

import (
	"github.com/user/paper-war/server/pkg/ai"
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
	// strongholdPool marks Stronghold entities. Phase 1A (#54): strongholds are
	// spawned as entities but not yet targetable — capture-by-flip is Phase 1B.
	// findTarget/isTargetValid skip them so units don't fire on invulnerable
	// buildings. Nil when no strongholds are present.
	strongholdPool *ecs.ComponentPool[component.StrongholdComponent]
	MapW, MapH     int32
	TerrainFn    func(x, y int32) component.TerrainType // tile terrain lookup
	// ElevationFn returns the Hill elevation band (0/1/2) of a tile, 0 off-map
	// or when no map is present. Any height advantage over the target adds a
	// flat +1 tile to effective attack range (high ground outranges low).
	// ADR-0029. Nil → flat map, no bonus (preserves legacy test behavior).
	ElevationFn func(x, y int32) uint8
	// TileDamageFn forwards tile-position damage to the TerrainSystem so cannon
	// / AoE splash destroys destructible doodads (Rock/Forest/Wall/Bridge).
	// Set from session.go after the TerrainSystem is constructed. Nil → terrain
	// is invulnerable (preserves legacy test behavior). Phase 3.
	TileDamageFn func(x, y int32, dmg int32)
	// StateLookup returns the AI state for a squad (0 if unknown or not
	// AI-driven). Used to suppress auto-pursuit for squads in StateGuard
	// so they hold ground instead of chasing out-of-range targets. Set
	// by session.go after both CombatSystem and AISystem are constructed.
	// CombatSystem resolves the entity→squad mapping via its boidPool
	// before calling this. Issue #52.
	StateLookup func(squadID uint32) uint8

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

// AttackFreezeTicks is how many ticks a unit's movement is suppressed after
// firing (#52, moved server-side). 5 ticks = 500ms at 10Hz — matches the
// median attack cooldown so the unit "plants to fire" between shots.
const AttackFreezeTicks uint32 = 5

// Range Tolerance constants (ADR-0031). A unit may fire RangeTolerance past its
// base Range when a same-Squad spotter lies within SpotterRadius. Both are 12.4
// fixed-point int64, matching ac.Range. Expressed via fixed.One (1<<12) so they
// remain compile-time constants equal to fixed.FromFloat(1.0) / fixed.FromFloat(2.0).
const (
	RangeTolerance = fixed.One        // 1.0 tiles
	SpotterRadius  = fixed.One * 2    // 2.0 tiles
)

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
	if p := w.Pool(component.StrongholdComponent{}); p != nil {
		s.strongholdPool = p.(*ecs.ComponentPool[component.StrongholdComponent])
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

	// Range Tolerance spotter pre-pass (ADR-0031). Build the set of units
	// currently engaging a valid target within their BASE Range — "spotters."
	// Same-Squad squadmates within SpotterRadius of a spotter fire ~1 tile past
	// their own Range this tick, but only once the spotter's engagement tenure
	// meets the follower's per-unit stagger threshold (1-2 ticks) — the opening
	// fire-stagger. This reuses last tick's resolved targeting state
	// (ac.TargetID), so there is one tick of latency — intended and consistent
	// with the aura spatial query. O(N) over the attack pool.
	spotters := s.buildSpotterSet()

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

		// Effective range for this tick. Range Tolerance (ADR-0031): a unit may
		// fire RangeTolerance past its base Range when a same-Squad spotter is
		// within SpotterRadius AND that spotter's engagement tenure meets this
		// unit's per-follower stagger threshold (1-2 ticks) — the opening fire-
		// stagger. effRange feeds both target acquisition and the fire-range
		// check so the overshoot actually lets the unit fire. Garrisoned units
		// neither grant nor receive tolerance — they fire from a Stronghold, not
		// the field formation.
		effRange := ac.Range
		if s.boidPool != nil {
			if bc, ok := s.boidPool.Get(e); ok && bc.GarrisonedIn == 0 {
				if s.hasNearbySpotter(pos, bc.SquadID, uint32(e), spotters) {
					effRange = ac.Range + RangeTolerance
				}
			}
		}

		// High-ground acquisition extension (ADR-0029): the fire check below
		// grants +1 tile for any height advantage, but acquisition previously
		// used only base(+tolerance) range — targets sitting in the bonus band
		// were never acquired, so the bonus only fired against targets first
		// caught via chase range. Query out to the max fire range (base + the
		// flat +1, when the attacker stands on ANY raised ground) so targets
		// the unit can actually hit are acquirable; the per-target bonus is
		// still applied exactly at the fire check.
		acqRange := effRange
		if s.ElevationFn != nil {
			if s.ElevationFn(int32(pos.X>>12), int32(pos.Y>>12)) > 0 {
				acqRange = effRange + fixed.One
			}
		}

		// Auto-acquire target using smart targeting with focus-fire on wounded enemies.
		// Pass currentTarget for hysteresis — findTarget only switches to a new
		// target if it's meaningfully better (lower priority tier, or same tier
		// but wounded while current isn't). Prevents DPS waste from flip-flopping.
		if ac.TargetID == 0 || !s.isTargetValid(e, ac, pos) {
			ac.TargetID = s.findTarget(e, pos, acqRange, weapon, 0)
			// If no target in attack range, try chase range (2x effective range)
			// so units close the gap instead of standing idle.
			if ac.TargetID == 0 {
				chaseRange := acqRange * 2
				ac.TargetID = s.findTarget(e, pos, chaseRange, weapon, 0)
			}
		} else {
			// Already have a valid target — opportunistically switch to a
			// meaningfully better one (e.g., a wounded enemy appeared in range).
			// Hysteresis inside findTarget prevents frivolous switching.
			newID := s.findTarget(e, pos, acqRange, weapon, ac.TargetID)
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
						if s.boidPool != nil {
							if bc, ok := s.boidPool.GetPtr(e); ok {
								bc.FreezeUntilTick = tick + AttackFreezeTicks
							}
						}
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
		// Effective range gains a flat +1 tile when the attacker holds ANY
		// elevation advantage over the target (peak over low no longer adds
		// +2 — too strong next to the halved base ranges). Shooting uphill
		// never shortens range — the defender already keeps hill cover.
		// ADR-0029. effRange already carries the Range Tolerance unlock from
		// above (ADR-0031) and is extended further by high ground here.
		fireRange := effRange
		if s.ElevationFn != nil {
			ax, ay := int32(pos.X>>12), int32(pos.Y>>12)
			tx, ty := int32(targetPos.X>>12), int32(targetPos.Y>>12)
			if s.ElevationFn(ax, ay) > s.ElevationFn(tx, ty) {
				fireRange = effRange + fixed.One // +1 tile, any height advantage
			}
		}
		rangeSq := (fireRange * fireRange) >> 12

		if distSq > rangeSq {
			// Target is out of attack range but still viable.
			// Default: pursue by setting a pathfinding destination so the
			// movement system closes the gap. Issue #52: a squad in
			// StateGuard holds ground — skip the pursue so the unit stays
			// planted. ADR-0027: a squad in StateApproach is being moved
			// by the AI to its CommitRange point — skip the pursue too,
			// otherwise CombatSystem would overwrite the AI's closing
			// destination with the enemy tile (double-move / overshoot).
			// StateLookup returns the AI state for this entity (0 if
			// unknown / not AI-driven — those still pursue).
			if s.StateLookup != nil && s.boidPool != nil {
				if bc, ok := s.boidPool.Get(e); ok {
					if st := s.StateLookup(bc.SquadID); st == ai.StateGuard || st == ai.StateApproach {
						return
					}
				}
			}
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

		// Get target's armor type. Strongholds use Building armor (only
		// Cannon/Missile damage them — enforced in findTarget, so a non-siege
		// weapon never reaches here for a stronghold). #54 1B.
		armor := component.ArmorLight // default
		isStronghold := false
		if s.strongholdPool != nil {
			if _, ok := s.strongholdPool.Get(targetEntity); ok {
				armor = component.ArmorBuilding
				isStronghold = true
			}
		}
		if !isStronghold && s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(targetEntity); ok {
				armor = ut.Armor
			}
		}

		dmgMultiplier := component.DamageMultiplier(weapon, armor)
		dmg := ac.Damage * dmgMultiplier / 100
		if dmgMultiplier > 0 && dmg < 1 {
			dmg = 1 // only clamp when the weapon can actually damage this armor
		}

		// Apply damage to primary target, reduced by the defender's terrain
		// cover (Forest/Hill/Rock/Brush). Issue #55.
		effectiveDmg := dmg
		if s.TerrainFn != nil {
			tx := int32(fixed.ToFloat(targetPos.X))
			ty := int32(fixed.ToFloat(targetPos.Y))
			defPct := terrainDefensePct(s.TerrainFn(tx, ty))
			if defPct > 0 {
				effectiveDmg = effectiveDmg * (100 - defPct) / 100
				if effectiveDmg < 1 {
					effectiveDmg = 1
				}
			}
		}

		// Stronghold damage split (#54 1B): the garrison absorbs a level-scaled
		// share (divided evenly), the stronghold takes the rest. No garrison →
		// the stronghold takes the full hit.
		tid := uint32(targetEntity)
		if isStronghold && s.strongholdPool != nil {
			if sh, ok := s.strongholdPool.GetPtr(targetEntity); ok && len(sh.Garrison) > 0 {
				share := component.StrongholdGarrisonShare(sh.Level)
				garrisonDmg := effectiveDmg * share / 100
				perUnit := garrisonDmg / int32(len(sh.Garrison))
				for _, g := range sh.Garrison {
					pg := pending[uint32(g)]
					pg.damage += perUnit
					pg.attacker = uint32(e)
					pending[uint32(g)] = pg
				}
				effectiveDmg = effectiveDmg - garrisonDmg // stronghold absorbs the remainder
			}
		}

		// Defer damage application (double-buffered)
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
		// Server-side attack freeze (#52): plant the unit for 5 ticks
		// (500ms at 10Hz) so it doesn't slide during the swing. This is
		// server-authoritative — the client sees no position change during
		// the freeze, eliminating the teleport on attack→move transitions.
		if s.boidPool != nil {
			if bc, ok := s.boidPool.GetPtr(e); ok {
				bc.FreezeUntilTick = tick + AttackFreezeTicks
			}
		}
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
	// Garrisoned units can't be targeted directly (they're sheltered inside a
	// stronghold — damage reaches them via the stronghold's split, #54 1B).
	if s.boidPool != nil {
		if bc, ok := s.boidPool.Get(targetEntity); ok && bc.GarrisonedIn != 0 {
			return false
		}
	}
	_ = targetPos // position existence verified above; range checked in main loop
	return true
}

// spotterInfo carries a spotter's squad and consecutive-engagement tenure
// through one tick of the Range Tolerance unlock check (ADR-0031 stagger).
type spotterInfo struct {
	SquadID uint32
	// Tenure is the count of consecutive ticks this spotter has been engaging
	// (as of this pre-pass). Followers compare it against their per-unit
	// stagger threshold to decide when the tolerance unlock fires.
	Tenure uint32
}

// buildSpotterSet computes the spotter set for Range Tolerance (ADR-0031) and
// updates each unit's SpotterTenure. A unit is a spotter this tick iff: it has
// a non-zero TargetID, that target is valid (alive, enemy, not a garrisoned
// defender) AND within the unit's BASE Range (re-validated against current
// positions), the unit is not garrisoned, and it is alive. Frozen units
// (mid-swing) MAY be spotters — they are engaging and their position is valid,
// so they are not excluded. While a unit qualifies, its ac.SpotterTenure is
// incremented; the instant it stops qualifying, tenure resets to 0 — so each
// fresh contact re-ripples the stagger naturally. Returns a map of entity
// ID → spotterInfo. Reuses last tick's resolved ac.TargetID, so there is one
// tick of latency — intended.
//
// The ac pointer handed to Each is the pool's backing storage (&p.data[i]),
// the same canonical pointer GetPtr returns — mutating ac.SpotterTenure here
// persists, matching how the main Tick loop mutates ac through Each.
func (s *CombatSystem) buildSpotterSet() map[uint32]spotterInfo {
	spotters := make(map[uint32]spotterInfo)
	if s.boidPool == nil {
		return spotters
	}
	s.attackPool.Each(func(e ecs.Entity, ac *component.AttackComponent) {
		if !s.qualifiesAsSpotter(e, ac) {
			ac.SpotterTenure = 0
			return
		}
		ac.SpotterTenure++
		bc, _ := s.boidPool.Get(e)
		spotters[uint32(e)] = spotterInfo{
			SquadID: bc.SquadID,
			Tenure:  ac.SpotterTenure,
		}
	})
	return spotters
}

// qualifiesAsSpotter reports whether unit e meets the spotter criteria THIS
// tick — the conjunction evaluated by buildSpotterSet. Factored out so the
// tenure pre-pass reads as a single predicate. Frozen units pass (engaging).
func (s *CombatSystem) qualifiesAsSpotter(e ecs.Entity, ac *component.AttackComponent) bool {
	if ac.TargetID == 0 {
		return false
	}
	bc, ok := s.boidPool.Get(e)
	if !ok || bc.GarrisonedIn != 0 {
		return false
	}
	hp, ok := s.healthPool.Get(e)
	if !ok || hp.HP <= 0 {
		return false
	}
	pos, ok := s.posPool.Get(e)
	if !ok {
		return false
	}
	// isTargetValid checks alive / faction / garrisoned-target but NOT range —
	// we re-check the BASE Range here against current positions.
	if !s.isTargetValid(e, ac, pos) {
		return false
	}
	targetPos, ok := s.posPool.Get(ecs.Entity(ac.TargetID))
	if !ok {
		return false
	}
	dx := targetPos.X - pos.X
	dy := targetPos.Y - pos.Y
	distSq := (dx*dx + dy*dy) >> 12
	rangeSq := (ac.Range * ac.Range) >> 12
	return distSq <= rangeSq
}

// spotterThreshold returns the per-follower stagger delay (in ticks) before
// this unit may fire via Range Tolerance off a squadmate's spotter. Stable per
// unit (derived from entity ID, no RNG) and either 1 or 2, so a Squad ripples
// into the fight instead of volleying at once. ADR-0031 opening fire-stagger.
// Deterministic by design — CombatSystem has no RNG; do not reach for math/rand.
func spotterThreshold(entityID uint32) uint32 {
	return 1 + (entityID % 2)
}

// hasNearbySpotter reports whether a same-Squad spotter lies within
// SpotterRadius of the unit at pos AND whose SpotterTenure has reached this
// unit's per-follower stagger threshold (ADR-0031 stagger). It reuses the same
// spatial hash findTarget uses for target search (s.Sh.Query) — no second
// broad-phase structure. selfID excludes the acquiring unit itself and selects
// the stagger threshold; a spotter must be a squadmate. The spatial Query
// already filters by radius, so no extra distance check.
func (s *CombatSystem) hasNearbySpotter(pos component.PositionComponent, squadID uint32, selfID uint32, spotters map[uint32]spotterInfo) bool {
	threshold := spotterThreshold(selfID)
	ids := s.Sh.Query(pos.X, pos.Y, SpotterRadius)
	for _, id := range ids {
		if id == uint64(selfID) {
			continue
		}
		info, ok := spotters[uint32(id)]
		if !ok {
			continue
		}
		if info.SquadID == squadID && info.Tenure >= threshold {
			return true
		}
	}
	return false
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

		// Strongholds are targetable (capture-by-flip, #54 1B) but only by
		// siege weapons — they have Building armor, which only Cannon/Missile
		// damage. Garrisoned units can't be targeted directly (shelter).
		if s.strongholdPool != nil {
			if _, ok := s.strongholdPool.Get(entity); ok {
				if weapon != component.WeaponCannon && weapon != component.WeaponMissile {
					continue
				}
			} else if s.boidPool != nil {
				if bc, ok := s.boidPool.Get(entity); ok && bc.GarrisonedIn != 0 {
					continue
				}
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

	// Phase 3 — AoE damages destructible terrain at the impact epicenter.
	// This is the single hook covering both ground-attack splash (CmdAttackGround)
	// and cannon target-splash: in StarCraft only siege/AoE breaches doodads, so
	// direct fire is intentionally not routed here. Nil → terrain invulnerable
	// (legacy tests). Damage applies once per splash event.
	if s.TileDamageFn != nil {
		tx := int32(targetPos.X >> fixed.FractionBits)
		ty := int32(targetPos.Y >> fixed.FractionBits)
		s.TileDamageFn(tx, ty, splashDmg)
	}

	for _, id := range ids {
		if id == uint64(attacker) || id == uint64(primaryTarget) {
			continue
		}
		entity := ecs.Entity(id)

		// Garrisoned units are sheltered inside a stronghold — splash doesn't
		// reach them. Their only damage source is the stronghold's split (#54 1B).
		if s.boidPool != nil {
			if bc, ok := s.boidPool.Get(entity); ok && bc.GarrisonedIn != 0 {
				continue
			}
		}

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

// terrainDefensePct returns the terrain-based damage-reduction % for a
// defender on the given terrain (Forest/Hill/Rock/Brush cover). 0 = none.
// Strongholds no longer contribute here — they're entities now (ADR-0023 /
// issue #54), and their defense bonus moves to garrisoned units in Phase 1B.
// Issue #55 phase 1.
func terrainDefensePct(terrain component.TerrainType) int32 {
	return TerrainCoverBonus(terrain)
}
