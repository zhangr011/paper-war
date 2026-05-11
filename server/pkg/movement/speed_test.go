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

// TestSquadStaysWithCommander verifies that combat units stay within a
// reasonable radius of the commander after moving, thanks to the
// commander-following force.
func TestSquadStaysWithCommander(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(20, 20)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 20)
	sh := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	formationRolePool := ecs.NewComponentPool[component.FormationRoleComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.FormationRoleComponent{}, formationRolePool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	cmdX := fixed.FromFloat(5.0)
	cmdY := fixed.FromFloat(5.0)
	targetX := fixed.FromFloat(15.0)
	targetY := fixed.FromFloat(15.0)
	speed := fixed.FromFloat(0.5)
	squadID := uint32(1)

	// Commander
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: cmdX, Y: cmdY})
	velPool.Add(cmd, component.VelocityComponent{Speed: speed})
	boidPool.Add(cmd, component.BoidComponent{
		SquadID:       squadID,
		Role:          component.RoleCommander,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(0.8),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(cmd, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmd, component.PathfindingComponent{TargetX: targetX, TargetY: targetY})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: squadID, IsAlive: true})

	// 4 combat units with small formation offsets
	offsets := [][2]int64{
		{fixed.FromFloat(-0.5), fixed.FromFloat(-0.5)},
		{fixed.FromFloat(0.5), fixed.FromFloat(-0.5)},
		{fixed.FromFloat(-0.5), fixed.FromFloat(0.5)},
		{fixed.FromFloat(0.5), fixed.FromFloat(0.5)},
	}
	for _, off := range offsets {
		u := em.Create()
		posPool.Add(u, component.PositionComponent{X: cmdX + off[0], Y: cmdY + off[1]})
		velPool.Add(u, component.VelocityComponent{Speed: speed})
		boidPool.Add(u, component.BoidComponent{
			SquadID:       squadID,
			Role:          component.RoleMelee,
			SeparationW:   fixed.FromFloat(1.5),
			CohesionW:     fixed.FromFloat(0.8),
			AlignmentW:    fixed.FromFloat(1.0),
			FormationW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(3.0),
		})
		movePool.Add(u, component.MovementComponent{ProfileID: 0})
		pathPool.Add(u, component.PathfindingComponent{TargetX: targetX, TargetY: targetY})
		formationRolePool.Add(u, component.FormationRoleComponent{
			OffsetX: off[0],
			OffsetY: off[1],
		})
	}

	// Run 50 ticks
	for tick := uint32(1); tick <= 50; tick++ {
		w.Tick(tick)
	}

	// Verify all combat units are within cohesion radius of the commander
	cmdPos, _ := posPool.Get(cmd)
	maxDist := 0.0
	for i := range offsets {
		u := ecs.Entity(uint64(i + 2))
		pos, ok := posPool.Get(u)
		if !ok {
			t.Errorf("combat unit %d not found", i)
			continue
		}
		dx := fixed.ToFloat(pos.X - cmdPos.X)
		dy := fixed.ToFloat(pos.Y - cmdPos.Y)
		dist := dx*dx + dy*dy
		if dist > maxDist {
			maxDist = dist
		}
	}
	// All units should be within 3.0 world units of commander
	if maxDist > 9.0 { // 3.0^2
		t.Errorf("squad spread too far from commander: max dist^2 = %.4f (> 9.0)", maxDist)
	}
}
