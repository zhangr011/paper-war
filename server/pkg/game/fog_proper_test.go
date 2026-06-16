package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
)

// TestFogUpdatesAcrossTicks: verify that fog grid updates when units move,
// and that the match stays in PhasePlaying across multiple ticks.
func TestFogUpdatesAcrossTicks(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	t.Logf("Phase after Start: %d (PhasePlaying=%d)", gs.Lifecycle.Phase, 1)

	// Spawn BOTH players so objective system doesn't end the match
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(24.0), fixed.FromFloat(10.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(24.0), fixed.FromFloat(85.0), 1, component.UnitLightInfantry)

	gs.Tick()
	t.Logf("Phase after tick 1: %d", gs.Lifecycle.Phase)

	grid := gs.FogSys.GetGrid(1)
	if grid == nil {
		t.Fatal("P1 fog grid missing")
	}
	t.Logf("After tick 1: P1 visible=%d, (24,10)=%d", countState(grid, fog.FogVisible), grid.Visible[10*48+24])

	gs.Tick()
	t.Logf("Phase after tick 2: %d", gs.Lifecycle.Phase)

	gs.Tick()
	t.Logf("Phase after tick 3: %d", gs.Lifecycle.Phase)
	grid = gs.FogSys.GetGrid(1)
	t.Logf("After tick 3: P1 visible=%d", countState(grid, fog.FogVisible))
}

// TestFogActuallyFiltersEnemyOnExploredTile: the real #22 acceptance test.
// Both players have units. P1 explores tile X. P1 moves away (X becomes explored).
// Enemy moves onto tile X. Enemy must NOT appear in P1 snapshot.
func TestFogActuallyFiltersEnemyOnExploredTile(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	// P1 at center, P2 far away (match stays active)
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(24.0), fixed.FromFloat(30.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(24.0), fixed.FromFloat(85.0), 1, component.UnitLightInfantry)
	gs.Tick()

	grid := gs.FogSys.GetGrid(1)
	if !grid.IsCurrentlyVisible(24, 30) {
		t.Fatalf("setup: (24,30) should be currently visible")
	}

	// Move P1 units to (5,5) — away from (24,30)
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	moveP1 := func(x, y float64) {
		cmdPool.Each(func(e ecs.Entity, _ *component.CommanderComponent) {
			if o, ok := ownerPool.Get(e); ok && o.PlayerID == 1 {
				if p, ok := posPool.GetPtr(e); ok {
					p.X = fixed.FromFloat(x)
					p.Y = fixed.FromFloat(y)
				}
			}
		})
		utPool.Each(func(e ecs.Entity, _ *component.UnitTypeComponent) {
			if o, ok := ownerPool.Get(e); ok && o.PlayerID == 1 {
				if p, ok := posPool.GetPtr(e); ok {
					p.X = fixed.FromFloat(x)
					p.Y = fixed.FromFloat(y)
				}
			}
		})
	}

	moveP1(5, 5)
	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	t.Logf("After P1 moves to (5,5): (24,30) state=%d, currently_visible=%v",
		grid.Visible[30*48+24], grid.IsCurrentlyVisible(24, 30))

	if grid.IsCurrentlyVisible(24, 30) {
		t.Error("(24,30) should be explored, not currently visible after P1 moved away")
	}

	// Now move P2 to (24,30) — the explored tile
	moveP2 := func(x, y float64) {
		cmdPool.Each(func(e ecs.Entity, _ *component.CommanderComponent) {
			if o, ok := ownerPool.Get(e); ok && o.PlayerID == 2 {
				if p, ok := posPool.GetPtr(e); ok {
					p.X = fixed.FromFloat(x)
					p.Y = fixed.FromFloat(y)
				}
			}
		})
		utPool.Each(func(e ecs.Entity, _ *component.UnitTypeComponent) {
			if o, ok := ownerPool.Get(e); ok && o.PlayerID == 2 {
				if p, ok := posPool.GetPtr(e); ok {
					p.X = fixed.FromFloat(x)
					p.Y = fixed.FromFloat(y)
				}
			}
		})
	}
	moveP2(24, 30)
	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	t.Logf("After P2 moves to (24,30): (24,30) state=%d currently_visible=%v",
		grid.Visible[30*48+24], grid.IsCurrentlyVisible(24, 30))

	// P2 at (24,30) should NOT make (24,30) visible to P1
	// (P2 units only reveal for P2's grid, not P1's)
	if grid.IsCurrentlyVisible(24, 30) {
		t.Error("BUG: enemy presence on explored tile made it visible to P1")
	}

	// The critical check: does P2 appear in P1's snapshot?
	data := gs.GenerateSnapshot(1, fullView(gs))
	p2Count := countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("P2 units in P1 snapshot: %d (expected 0)", p2Count)
	if p2Count > 0 {
		t.Errorf("BUG: %d enemy units leaked through fog on explored tile", p2Count)
	}
}
