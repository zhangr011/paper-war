package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestCombatUnitsMoveForward verifies that combat units move toward
// the target when given a move command, not backward (bug 04 regression).
func TestCombatUnitsMoveForward(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement test
	gs.objectiveSys = nil

	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	// Spawn team 1 at Y=48, move target at Y=70 (forward = +Y direction)
	spawnX := fixed.FromFloat(float64(DefaultMapWidth) / 2)
	spawnY := fixed.FromFloat(float64(DefaultMapHeight) / 2)
	targetY := fixed.FromFloat(70.0)

	// Use roster spawn path (SpawnTeamFromRoster-like, which was the buggy path)
	gs.SpawnTeamWithType(1, 1, spawnX, spawnY, 3, component.UnitLightInfantry)

	// Record starting positions of all team 1 units
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	type unitPos struct {
		id uint32
		x  float64
		y  float64
	}
	var before []unitPos
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			before = append(before, unitPos{
				id: uint32(e),
				x:  fixed.ToFloat(pos.X),
				y:  fixed.ToFloat(pos.Y),
			})
		}
	})

	if len(before) < 2 {
		t.Fatalf("expected at least 2 combat units, got %d", len(before))
	}

	// Issue move command toward +Y
	gs.handleMoveSquad(1, spawnX, targetY)

	// Tick 300 times (30 seconds) — enough for 300s cross-map speed
	for i := 0; i < 300; i++ {
		gs.Tick()
	}

	// Record positions after ticks
	var after []unitPos
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			after = append(after, unitPos{
				id: uint32(e),
				x:  fixed.ToFloat(pos.X),
				y:  fixed.ToFloat(pos.Y),
			})
		}
	})

	// Check that each unit moved toward +Y (not backward)
	backwardCount := 0
	for _, a := range after {
		for _, b := range before {
			if a.id == b.id {
				deltaY := a.y - b.y
				t.Logf("unit %d: Y %.2f -> %.2f (delta=%.2f)", a.id, b.y, a.y, deltaY)
				if deltaY < -0.1 {
					t.Errorf("unit %d moved BACKWARD: %.2f -> %.2f (delta=%.2f)", a.id, b.y, a.y, deltaY)
					backwardCount++
				}
				break
			}
		}
	}

	if backwardCount > 0 {
		t.Errorf("%d/%d units moved backward instead of forward", backwardCount, len(before))
	}

	// Also verify average Y increased (units are generally moving forward)
	avgBefore := 0.0
	for _, b := range before {
		avgBefore += b.y
	}
	avgBefore /= float64(len(before))

	avgAfter := 0.0
	for _, a := range after {
		avgAfter += a.y
	}
	avgAfter /= float64(len(after))

	t.Logf("avgY: %.2f -> %.2f (delta=%.2f)", avgBefore, avgAfter, avgAfter-avgBefore)
	if avgAfter <= avgBefore {
		t.Errorf("avg Y did not increase: %.2f -> %.2f (units not moving forward)", avgBefore, avgAfter)
	}
}

// TestCombatUnitsFollowCommander verifies that the AttractionW=2.0 attraction
// force works — combat units should converge toward their commander over time.
func TestCombatUnitsFollowCommander(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement test
	gs.objectiveSys = nil

	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	spawnX := fixed.FromFloat(float64(DefaultMapWidth) / 2)
	spawnY := fixed.FromFloat(float64(DefaultMapHeight) / 2)

	gs.SpawnTeamWithType(1, 1, spawnX, spawnY, 3, component.UnitLightInfantry)

	// Move commander to a new location and check units follow
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	// Find commander position
	var cmdX, cmdY float64
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			cmdX = fixed.ToFloat(pos.X)
			cmdY = fixed.ToFloat(pos.Y)
		}
	})

	// Measure initial average distance from commander
	avgDistBefore := 0.0
	combatCount := 0
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 || bc.Role == component.RoleCommander {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			ux := fixed.ToFloat(pos.X)
			uy := fixed.ToFloat(pos.Y)
			dx := ux - cmdX
			dy := uy - cmdY
			avgDistBefore += dx*dx + dy*dy
			combatCount++
		}
	})

	if combatCount == 0 {
		t.Fatal("no combat units found")
	}
	avgDistBefore /= float64(combatCount)

	// Move to new target
	newTargetY := fixed.FromFloat(60.0)
	gs.handleMoveSquad(1, spawnX, newTargetY)

	// Tick 50 times
	for i := 0; i < 50; i++ {
		gs.Tick()
	}

	// Measure new average distance from commander
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			cmdX = fixed.ToFloat(pos.X)
			cmdY = fixed.ToFloat(pos.Y)
		}
	})

	avgDistAfter := 0.0
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 || bc.Role == component.RoleCommander {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			ux := fixed.ToFloat(pos.X)
			uy := fixed.ToFloat(pos.Y)
			dx := ux - cmdX
			dy := uy - cmdY
			avgDistAfter += dx*dx + dy*dy
		}
	})
	avgDistAfter /= float64(combatCount)

	t.Logf("avg dist from commander: %.2f -> %.2f", avgDistBefore, avgDistAfter)

	// Units should be reasonably close to commander (< 3 tiles avg distance)
	// This proves AttractionW is pulling them toward the commander
	if avgDistAfter > 9.0 {
		t.Errorf("units too far from commander after 50 ticks: avgDistSq=%.2f", avgDistAfter)
	}
}