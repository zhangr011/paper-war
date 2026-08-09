package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
)

// TestFogEnemyAppearsAndDisappears: acceptance criterion from #22 —
// "Moving a team updates the visible fog area" and "previously explored
// areas remain dimmed but do not show live enemy unit positions".
//
// Scenario:
//  1. P1 at (24,10). P2 at (24,85) — far away, not visible. P2 count=0.
//  2. P2 moves to (24,20) — within P1 commander vision (radius 12). P2 count>0.
//  3. P2 moves back to (24,85) — out of vision. P2 count=0 again.
//  4. Tile (24,20) is now explored but not visible. Enemy must stay hidden.
func TestFogEnemyAppearsAndDisappears(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(float64(DefaultMapWidth)/2), fixed.FromFloat(10.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(float64(DefaultMapWidth)/2), fixed.FromFloat(float64(DefaultMapHeight)-10), 1, component.UnitLightInfantry)
	gs.Tick()

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])

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

	// Phase 1: P2 far away — should not appear
	data := gs.GenerateSnapshot(1, fullView(gs))
	p2 := countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("Phase 1 (P2 at y=85): P2 units visible=%d", p2)
	if p2 > 0 {
		t.Errorf("Phase 1: expected 0 enemy units, got %d", p2)
	}

	// Phase 2: P2 moves to (centerX,20) — within commander vision radius 12 from (centerX,10)
	moveP2(float64(DefaultMapWidth)/2, 20)
	gs.Tick()

	grid := gs.FogSys.GetGrid(1)
	t.Logf("Phase 2: (centerX,20) currently_visible=%v", grid.IsCurrentlyVisible(int32(DefaultMapWidth)/2, 20))
	if !grid.IsCurrentlyVisible(int32(DefaultMapWidth)/2, 20) {
		t.Error("Phase 2: (24,20) should be currently visible (within commander radius 12 from y=10)")
	}

	data = gs.GenerateSnapshot(1, fullView(gs))
	p2 = countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("Phase 2 (P2 at y=20): P2 units visible=%d", p2)
	if p2 == 0 {
		t.Error("Phase 2: expected enemy units to be visible when in vision range")
	}

	// Phase 3: P1 commander AND P2 both move away to (5,5) and (24,85).
	// This makes (24,20) transition from FogVisible → FogExplored.
	moveP2(float64(DefaultMapWidth)/2, float64(DefaultMapHeight)-10)
	moveP1(5, 5)
	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	tileIdx := 20*int(DefaultMapWidth) + int(DefaultMapWidth)/2
	t.Logf("Phase 3: (centerX,20) state=%d currently_visible=%v",
		grid.Visible[tileIdx], grid.IsCurrentlyVisible(int32(DefaultMapWidth)/2, 20))

	data = gs.GenerateSnapshot(1, fullView(gs))
	p2 = countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("Phase 3 (P2 moved away): P2 units visible=%d", p2)
	if p2 > 0 {
		t.Errorf("Phase 3: enemy should disappear when out of vision, got %d", p2)
	}

	// Phase 4: Verify tile (centerX,20) is now explored (dimmed) but enemy is hidden
	grid = gs.FogSys.GetGrid(1)
	if grid.Visible[tileIdx] != fog.FogExplored {
		t.Errorf("Phase 4: (centerX,20) should be FogExplored(1), got %d", grid.Visible[tileIdx])
	}
}

// TestFogSpectatorSeesAll: spectator (playerID=0) should see everything —
// no fog grid exists for playerID 0, so all units appear.
func TestFogSpectatorSeesAll(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(10.0), fixed.FromFloat(10.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(float64(DefaultMapWidth)-10), fixed.FromFloat(float64(DefaultMapHeight)-10), 1, component.UnitLightInfantry)
	gs.Tick()

	// Spectator has playerID=0 — no fog grid
	grid := gs.FogSys.GetGrid(0)
	if grid != nil {
		t.Log("Note: spectator has a fog grid (unusual but not necessarily wrong)")
	}

	// Spectator snapshot should include BOTH players' units
	data := gs.GenerateSnapshot(0, fullView(gs))
	p1 := countEnemyUnitsInSnapshot(t, data, 1)
	p2 := countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("Spectator snapshot: P1 units=%d, P2 units=%d", p1, p2)
	if p1 == 0 {
		t.Error("Spectator should see P1 units")
	}
	if p2 == 0 {
		t.Error("Spectator should see P2 units")
	}
}
