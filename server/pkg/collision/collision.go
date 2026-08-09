// Package collision implements hard positional collision among friendly
// CombatUnits (ADR-0030). After MovementSystem integrates forces, overlapping
// same-faction units are pushed apart by a positional correction — NOT a
// repulsion force. Enemy units do not collide (combat is ranged). Garrisoned
// units are fully excluded; attack-frozen units are immovable obstacles.
package collision

import (
	"sort"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// CollisionSystem runs at priority 65 — after MovementSystem (60) integrates
// forces and before CombatSystem (80) reads positions. It clears + rebuilds
// the shared spatial hash from POST-MOVE positions (MovementSystem leaves it
// holding pre-move positions), then resolves overlaps in one pass.
type CollisionSystem struct {
	// Sh is the shared spatial hash. It is cleared and repopulated each tick
	// from post-move positions, so the prior contents (pre-move) are discarded.
	Sh *spatial.Hash

	// Iterations is the number of relaxation passes run per tick. A single
	// pass under-resolves dense clusters (collective cohesion attraction
	// compacts the pile faster than one push-out removes); each extra pass
	// re-resolves overlaps created by the previous push. <=0 treated as 1.
	Iterations int

	posPool       *ecs.ComponentPool[component.PositionComponent]
	boidPool      *ecs.ComponentPool[component.BoidComponent]
	ownerPool     *ecs.ComponentPool[component.OwnerComponent]
	collisionPool *ecs.ComponentPool[component.CollisionComponent]
}

func (s *CollisionSystem) Name() string  { return "CollisionSystem" }
func (s *CollisionSystem) Priority() int { return 65 }

func (s *CollisionSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.CollisionComponent{}); p != nil {
		s.collisionPool = p.(*ecs.ComponentPool[component.CollisionComponent])
	}
}

// queryRadius is the broad-phase neighborhood radius. It must be ≥ 2× the
// largest collision radius (max radius 0.40 tile → 0.80 tile); 1.0 tile gives
// slack so the spatial-hash cell sweep (3×3 cells of size 2.0) never misses a
// pair. Pairs are filtered by the narrow-phase distance test afterward.
const queryRadius = 1 << 12 // fixed.FromFloat(1.0)

func (s *CollisionSystem) Tick(w *ecs.World, tick uint32) {
	if s.collisionPool == nil {
		return
	}

	// 1. Rebuild the spatial hash from POST-MOVE positions. MovementSystem's
	//    hash holds pre-move positions; reusing it would resolve stale pairs.
	s.Sh.Clear()

	// Collect participating entities (combat units with a collision radius).
	// Garrisoned units are excluded entirely — neither pushed nor an obstacle.
	type entry struct {
		e   ecs.Entity
		pos *component.PositionComponent
		bc  *component.BoidComponent
		col component.CollisionComponent
		own component.OwnerComponent
	}
	entries := make([]entry, 0, 64)
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.GarrisonedIn != 0 {
			return
		}
		pos, ok := s.posPool.GetPtr(e)
		if !ok {
			return
		}
		col, ok := s.collisionPool.Get(e)
		if !ok {
			return
		}
		own, ok := s.ownerPool.Get(e)
		if !ok {
			return
		}
		entries = append(entries, entry{e: e, pos: pos, bc: bc, col: col, own: own})
		s.Sh.Insert(uint64(e), pos.X, pos.Y)
	})

	// 2. Resolve in entity-ID order (deterministic).
	sort.Slice(entries, func(i, j int) bool {
		return uint64(entries[i].e) < uint64(entries[j].e)
	})

	iters := s.Iterations
	if iters < 1 {
		iters = 1
	}
	for iter := 0; iter < iters; iter++ {
		for i := 0; i < len(entries); i++ {
			a := entries[i]
			aFrozen := tick < a.bc.FreezeUntilTick

			ids := s.Sh.Query(a.pos.X, a.pos.Y, queryRadius)
			for _, id := range ids {
				bE := ecs.Entity(id)
				// Process each unordered pair once: only act when B > A.
				if uint64(bE) <= uint64(a.e) {
					continue
				}
				bPos, ok := s.posPool.GetPtr(bE)
				if !ok {
					continue
				}
				bBc, ok := s.boidPool.Get(bE)
				if !ok || bBc.GarrisonedIn != 0 {
					continue
				}
				bCol, ok := s.collisionPool.Get(bE)
				if !ok {
					continue
				}
				bOwn, ok := s.ownerPool.Get(bE)
				if !ok {
					continue
				}

				// Friendly only — enemy units (different faction) pass through.
				if bOwn.Faction != a.own.Faction {
					continue
				}

				bFrozen := tick < bBc.FreezeUntilTick
				// NOTE: a both-frozen pair is NOT skipped. The attack freeze
				// suppresses each unit's OWN steering (#52), not mutual
				// positional correction — and once a squad makes contact most
				// infantry are permanently frozen (cooldown ≤ AttackFreezeTicks
				// = 5, re-extended each shot). Skipping frozen-frozen pairs
				// would lock an overlapping firing line in place for the whole
				// firefight. Instead they dig each other apart, splitting the
				// penetration evenly (below). A LONE frozen unit vs a movable
				// one is still immovable — the movable partner routes around.

				// Narrow phase: distance-squared circle test vs sum of radii.
				dx := a.pos.X - bPos.X
				dy := a.pos.Y - bPos.Y
				distSq := fixed.DistSq(dx, dy)
				rSum := a.col.Radius + bCol.Radius
				rSumSq := fixed.Mul(rSum, rSum)
				// distSq >= rSumSq covers both "no overlap" and "just touching"
				// (distSq == rSumSq). The >= (not >) is what prevents jitter at rest.
				if distSq >= rSumSq {
					continue
				}

				// Unit normal (A away from B) and penetration depth.
				var nx, ny int64
				var pen int64
				if distSq == 0 {
					// Coincident centres: push apart along +X to avoid div-by-zero.
					nx, ny = fixed.One, 0
					pen = rSum
				} else {
					d := fixed.ISqrt(distSq)
					nx = fixed.Div(dx, d)
					ny = fixed.Div(dy, d)
					pen = rSum - d
				}

				// Split the displacement. Commanders are the squad cohesion
				// anchor: never displace them, or the whole squad's reference
				// frame shifts (collision shoving an anchor backward — away from
				// its advancing squad — was breaking the clash march and stalling
				// contact on some seeds). A commander is still an obstacle: the
				// movable partner absorbs the full penetration and routes around.
				// For non-commanders, both-frozen and both-movable pairs split
				// evenly; a lone frozen unit takes none.
				aCmd := a.bc.Role == component.RoleCommander
				bCmd := bBc.Role == component.RoleCommander
				var dispA, dispB int64
				switch {
				case aCmd || bCmd:
					// Commander participates as an immovable obstacle; the
					// non-commander (if any) takes the full penetration. Two
					// commanders (rare, same-faction) simply don't move.
					if aCmd {
						dispA, dispB = 0, pen
					} else {
						dispA, dispB = pen, 0
					}
				case aFrozen && bFrozen:
					dispA = pen >> 1
					dispB = pen - dispA // recovers the dropped LSB for odd pen
				case aFrozen:
					dispA, dispB = 0, pen
				case bFrozen:
					dispA, dispB = pen, 0
				default:
					dispA = pen >> 1
					dispB = pen - dispA // recovers the dropped LSB for odd pen
				}

				a.pos.X += fixed.Mul(nx, dispA)
				a.pos.Y += fixed.Mul(ny, dispA)
				bPos.X -= fixed.Mul(nx, dispB)
				bPos.Y -= fixed.Mul(ny, dispB)
			}
		}
	}
}
