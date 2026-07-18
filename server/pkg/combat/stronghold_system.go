package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// StrongholdSystem implements the Phase 1B stronghold gameplay (#54, ADR-0023):
//
//   - Capture-by-flip: when a stronghold's HP reaches 0, it flips to the
//     attacker's Faction and restores to full HP. Repeatable.
//   - Evict-on-flip: the captured stronghold's enemy garrison is ejected
//     (popped to an adjacent tile, GarrisonedIn cleared).
//   - Auto-garrison: a unit that reaches the stronghold's tile joins the
//     garrison (up to Capacity) if its faction owns the stronghold.
//   - Prune: dead/removed garrison entries are cleaned each tick.
//
// Priority 82: runs right after Combat(80) so a flip heals the stronghold
// before Death(90) would remove it.
type StrongholdSystem struct {
	em             *ecs.EntityManager
	strongholdPool *ecs.ComponentPool[component.StrongholdComponent]
	healthPool     *ecs.ComponentPool[component.HealthComponent]
	ownerPool      *ecs.ComponentPool[component.OwnerComponent]
	posPool        *ecs.ComponentPool[component.PositionComponent]
	boidPool       *ecs.ComponentPool[component.BoidComponent]
}

func (s *StrongholdSystem) Name() string  { return "StrongholdSystem" }
func (s *StrongholdSystem) Priority() int { return 82 }

func (s *StrongholdSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.strongholdPool = w.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	if p := w.Pool(component.BoidComponent{}); p != nil {
		s.boidPool = p.(*ecs.ComponentPool[component.BoidComponent])
	}
}

func (s *StrongholdSystem) Tick(w *ecs.World, tick uint32) {
	// Gather strongholds first (we mutate garrison slices / positions below).
	var shEntities []ecs.Entity
	s.strongholdPool.Each(func(e ecs.Entity, _ *component.StrongholdComponent) {
		shEntities = append(shEntities, e)
	})

	for _, shE := range shEntities {
		sh, ok := s.strongholdPool.GetPtr(shE)
		if !ok {
			continue
		}
		hp, ok := s.healthPool.GetPtr(shE)
		if !ok {
			continue
		}

		// 1. Prune dead/missing garrison members.
		sh.Garrison = s.pruneGarrison(shE, sh)

		// 2. Auto-garrison units standing on the stronghold tile (capacity permitting).
		s.autoGarrison(shE, sh)

		// 3. Capture-by-flip when HP hits 0.
		if hp.HP <= 0 {
			s.tryCapture(shE, sh, hp)
		}
	}
}

// pruneGarrison removes entries whose unit is dead or gone, clearing their
// GarrisonedIn flag. Returns the filtered slice.
func (s *StrongholdSystem) pruneGarrison(shE ecs.Entity, sh *component.StrongholdComponent) []ecs.Entity {
	if len(sh.Garrison) == 0 {
		return sh.Garrison
	}
	kept := sh.Garrison[:0]
	for _, g := range sh.Garrison {
		hp, ok := s.healthPool.Get(g)
		if !ok || hp.HP <= 0 {
			s.clearGarrisoned(g)
			continue
		}
		kept = append(kept, g)
	}
	return kept
}

// autoGarrison adds eligible units on the stronghold's tile to the garrison.
func (s *StrongholdSystem) autoGarrison(shE ecs.Entity, sh *component.StrongholdComponent) {
	if s.boidPool == nil {
		return
	}
	shPos, ok := s.posPool.Get(shE)
	if !ok {
		return
	}
	shTileX := int32(shPos.X >> 12)
	shTileY := int32(shPos.Y >> 12)
	owner, hasOwner := s.ownerPool.Get(shE)

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if uint32(len(sh.Garrison)) >= uint32(sh.Capacity) {
			return
		}
		if bc.GarrisonedIn != 0 {
			return
		}
		if bc.Role == component.RoleCommander {
			return // commanders don't garrison
		}
		// Same tile?
		up, ok := s.posPool.Get(e)
		if !ok {
			return
		}
		if int32(up.X>>12) != shTileX || int32(up.Y>>12) != shTileY {
			return
		}
		// Same faction as the stronghold (only the owner may garrison).
		if hasOwner {
			uo, ok := s.ownerPool.Get(e)
			if !ok || uo.Faction != owner.Faction {
				return
			}
		}
		// Garrison: snap onto the stronghold, mark garrisoned.
		if pp, ok := s.posPool.GetPtr(e); ok {
			pp.X = shPos.X
			pp.Y = shPos.Y
		}
		bc.GarrisonedIn = uint32(shE)
		sh.Garrison = append(sh.Garrison, e)
	})
}

// tryCapture flips the stronghold to the attacker's faction (derived from
// LastAttacker), restores HP, and evicts the garrison. No-op if there is no
// valid attacker faction.
func (s *StrongholdSystem) tryCapture(shE ecs.Entity, sh *component.StrongholdComponent, hp *component.HealthComponent) {
	if hp.LastAttacker == 0 || s.ownerPool == nil {
		return
	}
	attackerOwner, ok := s.ownerPool.Get(ecs.Entity(hp.LastAttacker))
	if !ok {
		return
	}
	faction := attackerOwner.Faction
	if faction == component.FactionNeutral {
		return // can't capture for "nobody"
	}
	cur, _ := s.ownerPool.Get(shE)
	if cur.Faction == faction {
		return // already owned by the attacker — nothing to flip
	}

	// Flip ownership.
	if pp, ok := s.ownerPool.GetPtr(shE); ok {
		pp.Faction = faction
		pp.PlayerID = attackerOwner.PlayerID
	}
	// Restore HP.
	hp.HP = component.StrongholdHP(sh.Level)
	hp.LastAttacker = 0

	// Evict the (now-enemy) garrison.
	s.evictGarrison(sh, shE)
}

// evictGarrison pops every garrisoned unit to an adjacent tile and clears
// its GarrisonedIn flag. Used on capture (the garrison belongs to the old
// owner) — evicted units survive with their remaining HP.
func (s *StrongholdSystem) evictGarrison(sh *component.StrongholdComponent, shE ecs.Entity) {
	if len(sh.Garrison) == 0 {
		return
	}
	shPos, ok := s.posPool.Get(shE)
	if !ok {
		// No position to evict to — just release them in place.
		for _, g := range sh.Garrison {
			s.clearGarrisoned(g)
		}
		sh.Garrison = nil
		return
	}
	tileX := int32(shPos.X >> 12)
	tileY := int32(shPos.Y >> 12)
	for i, g := range sh.Garrison {
		if pp, ok := s.posPool.GetPtr(g); ok {
			// Offset each evicted unit to a distinct adjacent tile.
			pp.X = fixed.FromFloat(float64(tileX + 1 + int32(i)))
			pp.Y = fixed.FromFloat(float64(tileY))
		}
		s.clearGarrisoned(g)
	}
	sh.Garrison = nil
}

func (s *StrongholdSystem) clearGarrisoned(unit ecs.Entity) {
	if s.boidPool == nil {
		return
	}
	if bc, ok := s.boidPool.GetPtr(unit); ok {
		bc.GarrisonedIn = 0
	}
}
