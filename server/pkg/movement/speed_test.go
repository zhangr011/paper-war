package movement

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestSpeedWithFixedFromFloatMoves verifies that using fixed.FromFloat for Speed
// produces movement even with the /10 divisor (no integer truncation to zero).
func TestSpeedWithFixedFromFloatMoves(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(10, 10)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	e := em.Create()
	startX := fixed.FromFloat(1.0)
	startY := fixed.FromFloat(1.0)
	posPool.Add(e, component.PositionComponent{X: startX, Y: startY})
	velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(e, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleMelee,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(1.0),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(e, component.MovementComponent{ProfileID: 0})
	pathPool.Add(e, component.PathfindingComponent{
		TargetX: fixed.FromFloat(8.0), TargetY: fixed.FromFloat(8.0),
	})

	for tick := uint32(1); tick <= 5; tick++ {
		w.Tick(tick)
	}

	pos, _ := posPool.Get(e)
	if pos.X <= startX && pos.Y <= startY {
		t.Errorf("unit didn't move: pos=(%v, %v)", fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y))
	}
}

// TestSpeedDivisorSlowsMovement verifies the /10 divisor makes units move
// slower than they would without it, by comparing displacement.
func TestSpeedDivisorSlowsMovement(t *testing.T) {
	// Run 5 ticks and verify the unit moved but stayed within a reasonable
	// bound — confirming the divisor is active.
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(10, 10)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	e := em.Create()
	startX := fixed.FromFloat(1.0)
	startY := fixed.FromFloat(1.0)
	posPool.Add(e, component.PositionComponent{X: startX, Y: startY})
	velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(e, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleMelee,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(1.0),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(e, component.MovementComponent{ProfileID: 0})
	pathPool.Add(e, component.PathfindingComponent{
		TargetX: fixed.FromFloat(8.0), TargetY: fixed.FromFloat(8.0),
	})

	for tick := uint32(1); tick <= 10; tick++ {
		w.Tick(tick)
	}

	pos, _ := posPool.Get(e)
	dx := fixed.ToFloat(pos.X - startX)
	dy := fixed.ToFloat(pos.Y - startY)

	// With divisor=10, max displacement per tick is ~0.05 world units.
	// Over 10 ticks, total displacement should be well under 1.0 world units.
	if dx >= 1.0 || dy >= 1.0 {
		t.Errorf("unit moved too far with divisor — divisor may not be applied: dx=%.4f dy=%.4f", dx, dy)
	}
}

func TestTenTimesSpeedMovesTenTimesFarther(t *testing.T) {
	baseDX := runSingleUnitStraightLineMove(t, fixed.FromFloat(0.5))
	fastDX := runSingleUnitStraightLineMove(t, fixed.FromFloat(5.0))

	if baseDX <= 0 {
		t.Fatalf("base unit did not move: dx=%.4f", fixed.ToFloat(baseDX))
	}

	ratio := fixed.ToFloat(fastDX) / fixed.ToFloat(baseDX)
	if ratio < 9.5 || ratio > 10.5 {
		t.Fatalf("10x speed movement ratio = %.2f, want about 10x (base dx %.4f, fast dx %.4f)",
			ratio, fixed.ToFloat(baseDX), fixed.ToFloat(fastDX))
	}
}

func runSingleUnitStraightLineMove(t *testing.T, speed int64) int64 {
	t.Helper()

	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(24, 8)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 24)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	e := em.Create()
	startX := fixed.FromFloat(1.0)
	startY := fixed.FromFloat(1.0)
	posPool.Add(e, component.PositionComponent{X: startX, Y: startY})
	velPool.Add(e, component.VelocityComponent{Speed: speed})
	boidPool.Add(e, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleMelee,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(1.0),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(e, component.MovementComponent{ProfileID: 0})
	pathPool.Add(e, component.PathfindingComponent{
		TargetX: fixed.FromFloat(20.0),
		TargetY: startY,
	})

	for tick := uint32(1); tick <= 10; tick++ {
		w.Tick(tick)
	}

	pos, ok := posPool.Get(e)
	if !ok {
		t.Fatalf("unit missing position after movement")
	}
	return pos.X - startX
}
