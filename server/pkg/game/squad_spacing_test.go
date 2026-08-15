package game

// Squad-spacing spec: every friendly unit must stay within 3× the collision
// contact distance (r_i + r_j, ADR-0030) of its nearest friendly neighbor,
// both at idle and while marching. The bound is a tightness invariant on the
// packed cluster — collision (ADR-0030) sets the floor (units never overlap),
// this sets the ceiling (the cluster never loosens beyond 3× hard spacing).

import (
	"math"
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// spacingBoundFor returns the max allowed nearest-neighbor distance for a
// squad of the given unit type: 3 × (r_i + r_j) = 6 × radius (homogeneous squad).
func spacingBoundFor(t *testing.T, ut component.CombatUnitType) float64 {
	t.Helper()
	r := fixed.ToFloat(component.CombatUnitTypeTable[ut].Radius)
	return 3 * (r + r)
}

// maxNearestNeighbor returns the largest per-unit nearest-neighbor distance
// among all alive squad members (commander included), in tiles.
func maxNearestNeighbor(gs *GameSession, squadID uint32) float64 {
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	type uv struct{ x, y float64 }
	var units []uv
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			units = append(units, uv{fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y)})
		}
	})
	if len(units) < 2 {
		return 0
	}
	worst := 0.0
	for i, u := range units {
		best := math.MaxFloat64
		for j, v := range units {
			if i == j {
				continue
			}
			dx, dy := u.x-v.x, u.y-v.y
			if d := math.Sqrt(dx*dx + dy*dy); d < best {
				best = d
			}
		}
		if best > worst {
			worst = best
		}
	}
	return worst
}

// newSpacingSession builds a no-objective session with one spawned squad.
func newSpacingSession(t *testing.T, n int, ut component.CombatUnitType) (*GameSession, uint32) {
	t.Helper()
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement
	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}
	gs.SetSessionRNG(rand.New(rand.NewSource(1)))
	const squadID uint32 = 1
	gs.SpawnSquadWithType(1, squadID,
		fixed.FromFloat(float64(DefaultMapWidth)/2),
		fixed.FromFloat(float64(DefaultMapHeight)/2), n, ut)
	return gs, squadID
}

// TestSquadSpacingIdleWithinThreeContactDistances: an idle squad (spawn
// target = own position → zero flow) must settle and STAY settled with every
// unit within 3× contact distance of its nearest squadmate, checked every
// tick so a transient violation cannot slip through between checkpoints.
func TestSquadSpacingIdleWithinThreeContactDistances(t *testing.T) {
	gs, squadID := newSpacingSession(t, 16, component.UnitLightInfantry)
	bound := spacingBoundFor(t, component.UnitLightInfantry)

	worst := 0.0
	for tick := 1; tick <= 100; tick++ {
		gs.World.Tick(uint32(tick))
		if d := maxNearestNeighbor(gs, squadID); d > worst {
			worst = d
		}
	}

	if worst > bound {
		t.Errorf("idle squad loosened beyond 3× contact distance: worst nearest-neighbor %.3f tiles > bound %.3f",
			worst, bound)
	}
}

// TestSquadSpacingMarchWithinThreeContactDistances: a squad marching toward a
// distant target must keep every unit within 3× contact distance of its
// nearest squadmate, checked every tick across the whole transit.
func TestSquadSpacingMarchWithinThreeContactDistances(t *testing.T) {
	gs, squadID := newSpacingSession(t, 16, component.UnitLightInfantry)
	bound := spacingBoundFor(t, component.UnitLightInfantry)

	// March order toward the far corner, exactly like handleMoveSquad: set
	// the path target on every squad member and let the flow field drive.
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	const tx, ty = 28.0, 45.0
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if path, ok := pathPool.GetPtr(e); ok {
			path.TargetX = fixed.FromFloat(tx)
			path.TargetY = fixed.FromFloat(ty)
		}
	})

	worst := 0.0
	worstTick := 0
	for tick := 1; tick <= 300; tick++ {
		gs.World.Tick(uint32(tick))
		if d := maxNearestNeighbor(gs, squadID); d > worst {
			worst, worstTick = d, tick
		}
	}

	if worst > bound {
		t.Errorf("marching squad loosened beyond 3× contact distance: worst nearest-neighbor %.3f tiles > bound %.3f at tick %d",
			worst, bound, worstTick)
	}
}
