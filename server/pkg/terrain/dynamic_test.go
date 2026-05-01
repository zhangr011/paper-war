package terrain

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestBridgeDestruction(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	gm.SetTerrain(2, 2, component.TerrainBridge)
	tile := gm.TileAt(2, 2)
	tile.Health = 50
	tile.MaxHealth = 50

	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainBridge] = 1

	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	// Verify bridge is passable before
	if gm.CostAt(2, 2, profile) != 1 {
		t.Error("bridge should be passable before destruction")
	}

	// Destroy bridge
	ts.ProcessDestruction(2, 2, 50)
	ts.Tick(nil, 1)

	// After destruction, bridge → deep water (impassable for infantry)
	if gm.CostAt(2, 2, profile) != 0 {
		t.Errorf("destroyed bridge should be impassable, cost = %d", gm.CostAt(2, 2, profile))
	}
}

func TestWallDestruction(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	gm.SetTerrain(3, 3, component.TerrainWall)
	tile := gm.TileAt(3, 3)
	tile.Health = 100
	tile.MaxHealth = 100

	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1

	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	if gm.CostAt(3, 3, profile) != 0 {
		t.Error("wall should be impassable")
	}

	ts.ProcessDestruction(3, 3, 100)
	ts.Tick(nil, 1)

	if gm.CostAt(3, 3, profile) != 1 {
		t.Errorf("destroyed wall should become plain (cost 1), got %d", gm.CostAt(3, 3, profile))
	}
}

func TestCacheInvalidatedOnTerrainChange(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1

	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	// Populate cache
	ff1 := cache.Get(2, 2, profile)
	if ff1 == nil {
		t.Fatal("cache should return flow field")
	}

	// Destroy something
	gm.SetTerrain(1, 1, component.TerrainWall)
	tile := gm.TileAt(1, 1)
	tile.Health = 10
	tile.MaxHealth = 10
	ts.ProcessDestruction(1, 1, 10)
	ts.Tick(nil, 1)

	// Cache should be invalidated — new Get should recompute
	ff2 := cache.Get(2, 2, profile)
	if ff2 == nil {
		t.Fatal("cache should return new flow field after invalidation")
	}
}
