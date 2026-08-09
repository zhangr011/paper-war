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

	// Spawn positions scaled to the default map dimensions.
	spawnX := float64(DefaultMapWidth) / 2
	spawnY1 := 10.0
	spawnY2 := float64(DefaultMapHeight) - 10.0

	t.Logf("Phase after Start: %d (PhasePlaying=%d)", gs.Lifecycle.Phase, 1)

	// Spawn BOTH players so objective system doesn't end the match
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(spawnX), fixed.FromFloat(spawnY1), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(spawnX), fixed.FromFloat(spawnY2), 1, component.UnitLightInfantry)

	gs.Tick()
	t.Logf("Phase after tick 1: %d", gs.Lifecycle.Phase)

	grid := gs.FogSys.GetGrid(1)
	if grid == nil {
		t.Fatal("P1 fog grid missing")
	}
	t.Logf("After tick 1: P1 visible=%d, (%g,%g)=%d",
		countState(grid, fog.FogVisible), spawnX, spawnY1,
		grid.Visible[int(spawnY1)*int(DefaultMapWidth)+int(spawnX)])

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

	// Coordinates scaled to default map dimensions. P1 at center, P2 near
	// the opposite short edge so the match stays in PhasePlaying.
	centerX := float64(DefaultMapWidth) / 2  // 15 on a 30-wide map
	centerY := float64(DefaultMapHeight) / 2 // 24 on a 48-tall map
	p2Y := float64(DefaultMapHeight) - 4.0   // near bottom edge

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(centerX), fixed.FromFloat(centerY), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(centerX), fixed.FromFloat(p2Y), 1, component.UnitLightInfantry)
	gs.Tick()

	// Use integer tile coords for fog-grid queries.
	tileX := int32(centerX)
	tileY := int32(centerY)
	stride := int(DefaultMapWidth)
	idx := int(tileY)*stride + int(tileX)

	grid := gs.FogSys.GetGrid(1)
	if !grid.IsCurrentlyVisible(tileX, tileY) {
		t.Fatalf("setup: (%d,%d) should be currently visible", tileX, tileY)
	}

	// Move P1 units to (5,5) — away from center
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
	t.Logf("After P1 moves to (5,5): (%d,%d) state=%d, currently_visible=%v",
		tileX, tileY, grid.Visible[idx], grid.IsCurrentlyVisible(tileX, tileY))

	if grid.IsCurrentlyVisible(tileX, tileY) {
		t.Errorf("(%d,%d) should be explored, not currently visible after P1 moved away", tileX, tileY)
	}

	// Now move P2 to the explored tile — the explored tile
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
	moveP2(float64(tileX), float64(tileY))
	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	t.Logf("After P2 moves to (%d,%d): state=%d currently_visible=%v",
		tileX, tileY, grid.Visible[idx], grid.IsCurrentlyVisible(tileX, tileY))

	// P2 at the tile should NOT make (24,30) visible to P1
	// (P2 units only reveal for P2's grid, not P1's)
	if grid.IsCurrentlyVisible(tileX, tileY) {
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
