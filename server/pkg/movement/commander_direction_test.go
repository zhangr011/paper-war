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

// TestCommanderMovesTowardTarget_YAxis is a regression test for bug #02:
// "Commander steers in wrong direction from pathfinding target".
//
// Before the fix at movement.go:132-134 (excluding same-squad neighbors
// from the commander's separation force), spawning a commander + combat
// units at (10,10) and issuing a move to (10,20) caused the commander's
// Y to DECREASE on the first tick — its own squad members behind/beside
// it pushed it backward via separation, overwhelming the flow-field force.
//
// This test reproduces the scenario and asserts forward progress.
func TestCommanderMovesTowardTarget_YAxis(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(64, 64)
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

	// Commander at (10, 10).  Move target is (10, 20) — pure +Y direction.
	cmdX, cmdY := fixed.FromFloat(10.0), fixed.FromFloat(10.0)
	targetX, targetY := fixed.FromFloat(10.0), fixed.FromFloat(20.0)

	cmdEntity := em.Create()
	posPool.Add(cmdEntity, component.PositionComponent{X: cmdX, Y: cmdY})
	velPool.Add(cmdEntity, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(cmdEntity, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleCommander,
		AttractionW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(2.0),
	})
	movePool.Add(cmdEntity, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmdEntity, component.PathfindingComponent{TargetX: targetX, TargetY: targetY})

	// Surround the commander with 4 combat units in a tight cluster so
	// the separation force is non-trivial.  Without the same-squad
	// exclusion, these would shove the commander backward.
	for i := 0; i < 4; i++ {
		e := em.Create()
		ox := []int64{fixed.FromFloat(-0.5), fixed.FromFloat(0.5), fixed.FromFloat(-0.5), fixed.FromFloat(0.5)}[i]
		oy := []int64{fixed.FromFloat(-0.5), fixed.FromFloat(-0.5), fixed.FromFloat(0.5), fixed.FromFloat(0.5)}[i]
		posPool.Add(e, component.PositionComponent{X: cmdX + ox, Y: cmdY + oy})
		velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
		boidPool.Add(e, component.BoidComponent{
			SquadID:       1,
			Role:          component.RoleMelee,
			AttractionW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(2.0),
		})
		movePool.Add(e, component.MovementComponent{ProfileID: 0})
		pathPool.Add(e, component.PathfindingComponent{TargetX: targetX, TargetY: targetY})
	}

	// Tick once and check the commander advanced toward +Y (not retreated).
	w.Tick(1)
	pos, _ := posPool.Get(cmdEntity)
	newY := fixed.ToFloat(pos.Y)
	if newY <= 10.0 {
		t.Errorf("commander Y did not increase toward target (10→20): got Y=%.4f (delta=%+.4f)",
			newY, newY-10.0)
	}

	// Long-run: with Speed=0.5 and PositionDivisor=10, max progress is
	// ~0.05 tiles/tick → ~5 tiles in 100 ticks.  The original bug (#02)
	// made only 0.007 tiles of progress in 200 ticks (commander was
	// actively retreating from same-squad separation).  Requiring >=2
	// tiles of forward progress definitively rules out the regression
	// while tolerating force-balance slowdowns.
	for tick := uint32(2); tick <= 100; tick++ {
		w.Tick(tick)
	}
	pos, _ = posPool.Get(cmdEntity)
	finalY := fixed.ToFloat(pos.Y)
	if finalY < 12.0 {
		t.Errorf("commander failed to make meaningful forward progress toward Y=20 after 100 ticks: Y=%.4f (progress=%.4f tiles, bug #02 would give ~0.007)",
			finalY, finalY-10.0)
	}
}

// TestCommanderMovesTowardTarget_XAxis is the same scenario on the X axis
// to catch any axis-asymmetric regression.
func TestCommanderMovesTowardTarget_XAxis(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(64, 64)
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

	cmdX, cmdY := fixed.FromFloat(10.0), fixed.FromFloat(10.0)
	targetX, targetY := fixed.FromFloat(20.0), fixed.FromFloat(10.0)

	cmdEntity := em.Create()
	posPool.Add(cmdEntity, component.PositionComponent{X: cmdX, Y: cmdY})
	velPool.Add(cmdEntity, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(cmdEntity, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleCommander,
		AttractionW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(2.0),
	})
	movePool.Add(cmdEntity, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmdEntity, component.PathfindingComponent{TargetX: targetX, TargetY: targetY})

	w.Tick(1)
	pos, _ := posPool.Get(cmdEntity)
	newX := fixed.ToFloat(pos.X)
	if newX <= 10.0 {
		t.Errorf("commander X did not increase toward target (10→20): got X=%.4f (delta=%+.4f)",
			newX, newX-10.0)
	}
}
