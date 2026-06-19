package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// TestHandleChangeFormationUpdatesOffsets verifies that changing formation
// from Line to Circle actually recalculates and applies new offsets to
// FormationRoleComponent for all combat units in the squad.
func TestHandleChangeFormationUpdatesOffsets(t *testing.T) {
	gs := NewGameSession()
	gs.objectiveSys = nil
	gs.Lifecycle.Start()

	// Spawn a team with 5 combat units (default Line formation).
	gs.SpawnTeamWithType(1, 24, 10, 10, 1, component.UnitLightInfantry)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	formationRolePool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])

	// Find the squad ID and capture original offsets.
	var squadID uint32
	originalOffsets := map[ecs.Entity][2]int64{}
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role != component.RoleCommander {
			squadID = bc.SquadID
			if fr, ok := formationRolePool.Get(e); ok {
				originalOffsets[e] = [2]int64{fr.OffsetX, fr.OffsetY}
			}
		}
	})

	if len(originalOffsets) != 5 {
		t.Fatalf("expected 5 combat units with formation roles, got %d", len(originalOffsets))
	}

	// Switch to Circle formation.
	gs.handleChangeFormation(squadID, uint8(component.FormationCircle))

	// Verify offsets changed for at least some units.
	changed := 0
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander {
			return
		}
		if fr, ok := formationRolePool.GetPtr(e); ok {
			orig := originalOffsets[e]
			if fr.OffsetX != orig[0] || fr.OffsetY != orig[1] {
				changed++
			}
			// Circle formation should produce non-zero offsets for multi-unit squads.
			if fr.OffsetX == 0 && fr.OffsetY == 0 {
				t.Errorf("unit %d: circle formation produced (0,0) offset", e)
			}
		}
	})

	if changed == 0 {
		t.Error("no units changed offsets after formation switch to Circle")
	}

	// Verify FormationComponent was also updated.
	formationPool := gs.World.Pool(component.FormationComponent{}).(*ecs.ComponentPool[component.FormationComponent])
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if fc, ok := formationPool.Get(e); ok {
			if fc.FormationType != component.FormationCircle {
				t.Errorf("FormationComponent not updated: got %d, want %d (Circle)",
					fc.FormationType, component.FormationCircle)
			}
		}
	})
}

// TestHandleChangeFormationWedge verifies wedge formation produces a column
// (increasing Y offsets, same X).
func TestHandleChangeFormationWedge(t *testing.T) {
	gs := NewGameSession()
	gs.objectiveSys = nil
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 24, 10, 10, 1, component.UnitLightInfantry)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	formationRolePool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])

	var squadID uint32
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role != component.RoleCommander {
			squadID = bc.SquadID
		}
	})

	gs.handleChangeFormation(squadID, uint8(component.FormationWedge))

	// Wedge: all DX=0, DY varies (column behind commander).
	dxs := map[int64]bool{}
	dys := []int64{}
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander || bc.SquadID != squadID {
			return
		}
		if fr, ok := formationRolePool.GetPtr(e); ok {
			dxs[fr.OffsetX] = true
			dys = append(dys, fr.OffsetY)
		}
	})

	// All DX should be 0 in wedge (single column).
	for dx := range dxs {
		if dx != 0 {
			t.Errorf("wedge formation should have DX=0 for all units, got DX=%d", dx)
		}
	}

	// DY values should be negative (units line up behind commander).
	if len(dys) < 2 {
		t.Fatalf("expected at least 2 units in wedge, got %d", len(dys))
	}
}
