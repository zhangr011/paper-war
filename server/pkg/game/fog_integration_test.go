package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/network"
)

func TestFogGridSentInSnapshot(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(20.0), fixed.FromFloat(20.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(float64(DefaultMapWidth) - 10), fixed.FromFloat(float64(DefaultMapHeight) - 10), 1, component.UnitLightInfantry)
	gs.Tick()

	grid := gs.FogSys.GetGrid(1)
	if grid == nil {
		t.Fatal("player 1 fog grid should exist after tick")
	}

	var unexplored, explored, visible int
	for _, v := range grid.Visible {
		switch v {
		case fog.FogUnexplored:
			unexplored++
		case fog.FogExplored:
			explored++
		case fog.FogVisible:
			visible++
		}
	}
	t.Logf("fog: unexplored=%d explored=%d visible=%d (total=%d)", unexplored, explored, visible, len(grid.Visible))

	if visible == 0 {
		t.Error("expected some visible tiles around commander")
	}
	if unexplored == 0 {
		t.Error("expected some unexplored tiles")
	}

	data := gs.GenerateSnapshot(1, fullView(gs))
	if data == nil {
		t.Fatal("snapshot should not be nil")
	}
	t.Logf("snapshot size: %d bytes", len(data))

	fogFound := false
	for i := len(data) - 8; i >= 0; i-- {
		if data[i] == 0xFF && i+3 < len(data) &&
			data[i+1] == 0xFE && data[i+2] == 0xFD && data[i+3] == 0xFC {
			fogFound = true
			break
		}
	}
	if !fogFound {
		t.Error("fog marker 0xFF 0xFE 0xFD 0xFC not found in snapshot binary")
	}
}

func TestFogClearPreservesExplored(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(24.0), fixed.FromFloat(48.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(float64(DefaultMapWidth) - 10), fixed.FromFloat(float64(DefaultMapHeight) - 10), 1, component.UnitLightInfantry)
	gs.Tick()

	grid := gs.FogSys.GetGrid(1)
	if grid == nil {
		t.Fatal("grid should exist")
	}
	visible1 := countState(grid, fog.FogVisible)
	if visible1 == 0 {
		t.Fatal("should have visible tiles after tick 1")
	}

	// Move ALL player 1 entities far away using GetPtr (pointer to actual data)
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		owner, ok := ownerPool.Get(e)
		if ok && owner.PlayerID == 1 {
			pos, ok := posPool.GetPtr(e)
			if ok {
				pos.X = fixed.FromFloat(5.0)
				pos.Y = fixed.FromFloat(5.0)
			}
		}
	})
	utPool.Each(func(e ecs.Entity, ut *component.UnitTypeComponent) {
		owner, ok := ownerPool.Get(e)
		if ok && owner.PlayerID == 1 {
			pos, ok := posPool.GetPtr(e)
			if ok {
				pos.X = fixed.FromFloat(5.0)
				pos.Y = fixed.FromFloat(5.0)
			}
		}
	})

	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	explored := countState(grid, fog.FogExplored)
	visible2 := countState(grid, fog.FogVisible)
	t.Logf("after move: explored=%d visible=%d", explored, visible2)

	if explored == 0 {
		t.Error("expected explored tiles from previous position")
	}
	if visible2 == 0 {
		t.Error("expected visible tiles at new position (5,5)")
	}
}

func TestCombatUnitsProvideVision(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(10.0), fixed.FromFloat(10.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(float64(DefaultMapWidth) - 10), fixed.FromFloat(float64(DefaultMapHeight) - 10), 1, component.UnitLightInfantry)
	gs.Tick()

	grid := gs.FogSys.GetGrid(1)
	t.Logf("after spawn: visible=%d", countState(grid, fog.FogVisible))

	// Move one combat unit far away using GetPtr
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	moved := false
	utPool.Each(func(e ecs.Entity, ut *component.UnitTypeComponent) {
		if moved {
			return
		}
		owner, ok := ownerPool.Get(e)
		if !ok || owner.PlayerID != 1 {
			return
		}
		pos, ok := posPool.GetPtr(e)
		if ok {
			pos.X = fixed.FromFloat(20.0)
			pos.Y = fixed.FromFloat(20.0)
			moved = true
		}
	})

	gs.Tick()

	grid = gs.FogSys.GetGrid(1)
	t.Logf("after unit move: visible=%d", countState(grid, fog.FogVisible))

	if !grid.IsCurrentlyVisible(20, 20) {
		t.Error("tile (20,20) should be visible from combat unit")
	}
}

func countState(grid *fog.FogGrid, state uint8) int {
	if grid == nil {
		return 0
	}
	count := 0
	for _, v := range grid.Visible {
		if v == state {
			count++
		}
	}
	return count
}

func fullView(gs *GameSession) network.Rect {
	mw, mh := gs.MapSize()
	return network.Rect{
		X: 0, Y: 0,
		W: fixed.FromFloat(float64(mw)),
		H: fixed.FromFloat(float64(mh)),
	}
}

var _ = ecs.Entity(0)
