package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestMovementDebug(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement test
	gs.objectiveSys = nil
	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	spawnX := fixed.FromFloat(24.0)
	spawnY := fixed.FromFloat(48.0)
	targetY := fixed.FromFloat(70.0)

	gs.SpawnTeamWithType(1, 1, spawnX, spawnY, 3, component.UnitLightInfantry)

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	// Record commander position
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	_ = uint32(0) // placeholder

	gs.handleMoveSquad(1, spawnX, targetY)

	for tick := 0; tick < 200; tick++ {
		gs.Tick()

		if tick%50 == 0 {
			// Log positions
			cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
				if !cmd.IsAlive { return }
				if pos, ok := posPool.Get(e); ok {
					t.Logf("tick %3d commander Y=%.3f", tick, fixed.ToFloat(pos.Y))
				}
			})
			boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
				if bc.SquadID != 1 || bc.Role == component.RoleCommander { return }
				if pos, ok := posPool.Get(e); ok {
					t.Logf("tick %3d unit %d Y=%.3f formationW=%d", tick, uint32(e), fixed.ToFloat(pos.Y), bc.FormationW)
				}
			})
		}
	}

	// Final check
	var cmdFinalY float64
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive { return }
		if pos, ok := posPool.Get(e); ok {
			cmdFinalY = fixed.ToFloat(pos.Y)
		}
	})

	if cmdFinalY < 50.0 {
		t.Errorf("commander barely moved after 200 ticks: Y=%.2f (started at 48, target at 70)", cmdFinalY)
	}
}
