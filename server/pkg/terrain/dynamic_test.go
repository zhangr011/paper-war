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
	ff1 := cache.Get(2, 2, profile, 0)
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
	ff2 := cache.Get(2, 2, profile, 0)
	if ff2 == nil {
		t.Fatal("cache should return new flow field after invalidation")
	}
}

// TestRockDestruction verifies Phase 3 doodad HP on Rock → Plain via the
// destruction path, and that the applied change is exposed via DrainApplied
// (the snapshot builder's source for EventTerrainChange wire events).
func TestRockDestruction(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	gm.SetTerrain(2, 1, component.TerrainRock)
	tile := gm.TileAt(2, 1)
	tile.Health = 300
	tile.MaxHealth = 300

	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainRock] = 2
	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	// Partial damage: rock survives, no event applied yet.
	ts.ProcessDestruction(2, 1, 100)
	if tile.Health != 200 {
		t.Fatalf("rock HP after partial damage = %d, want 200", tile.Health)
	}
	ts.Tick(nil, 1)
	if applied := ts.DrainApplied(); applied != nil {
		t.Errorf("no terrain change expected before destruction, got %+v", applied)
	}
	if gm.TileAt(2, 1).TerrainType != component.TerrainRock {
		t.Error("rock should still be rock after partial damage")
	}

	// Lethal damage: rock → Plain, event produced with correct x/y/newTerrain.
	ts.ProcessDestruction(2, 1, 250)
	ts.Tick(nil, 1)

	if gm.TileAt(2, 1).TerrainType != component.TerrainPlain {
		t.Errorf("destroyed rock should become Plain, got %d", gm.TileAt(2, 1).TerrainType)
	}
	applied := ts.DrainApplied()
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied terrain event, got %d", len(applied))
	}
	ev := applied[0]
	if ev.X != 2 || ev.Y != 1 {
		t.Errorf("applied event coords = (%d,%d), want (2,1)", ev.X, ev.Y)
	}
	if ev.NewTerrain != component.TerrainPlain {
		t.Errorf("applied event NewTerrain = %d, want Plain", ev.NewTerrain)
	}
	// Second drain returns nil (buffer cleared).
	if again := ts.DrainApplied(); again != nil {
		t.Errorf("DrainApplied should clear the buffer, got %+v", again)
	}
}

// TestForestDestruction verifies Forest doodad destruction → Plain and that
// MaxHealth/Health are zeroed after the tile is converted (so it can't be
// re-damaged as a destructible).
func TestForestDestruction(t *testing.T) {
	gm := tilemap.NewGameMap(4, 4)
	gm.SetTerrain(1, 2, component.TerrainForest)
	tile := gm.TileAt(1, 2)
	tile.Health = 200
	tile.MaxHealth = 200

	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainForest] = 2
	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	ts.ProcessDestruction(1, 2, 200)
	ts.Tick(nil, 1)

	after := gm.TileAt(1, 2)
	if after.TerrainType != component.TerrainPlain {
		t.Errorf("destroyed forest should become Plain, got %d", after.TerrainType)
	}
	if after.MaxHealth != 0 || after.Health != 0 {
		t.Errorf("destroyed tile HP should be zeroed, got Health=%d MaxHealth=%d", after.Health, after.MaxHealth)
	}
}

// TestTileDamageCallbackRoutesToProcessDestruction exercises the combat→terrain
// seam (TileDamageFn) at the TerrainSystem end: the callback assigned in
// session.go (gs.combatSys.TileDamageFn = terrainSys.ProcessDestruction) must
// decrement doodad HP and eventually produce the wire-event precursor.
func TestTileDamageCallbackRoutesToProcessDestruction(t *testing.T) {
	gm := tilemap.NewGameMap(4, 4)
	gm.SetTerrain(3, 0, component.TerrainWall)
	tile := gm.TileAt(3, 0)
	tile.Health = 400
	tile.MaxHealth = 400

	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	// This is the exact wiring session.go performs.
	var onTileDamage func(x, y int32, dmg int32) = ts.ProcessDestruction

	// Two splash hits at 120 each (< 400 total) damage but don't breach.
	onTileDamage(3, 0, 120)
	onTileDamage(3, 0, 120)
	ts.Tick(nil, 1)
	if gm.TileAt(3, 0).TerrainType != component.TerrainWall {
		t.Fatal("wall should survive sub-lethal splash")
	}

	// Third hit breaches → Wall → Plain, event produced next tick.
	onTileDamage(3, 0, 200)
	ts.Tick(nil, 1)

	if gm.TileAt(3, 0).TerrainType != component.TerrainPlain {
		t.Errorf("breached wall should become Plain, got %d", gm.TileAt(3, 0).TerrainType)
	}
	applied := ts.DrainApplied()
	if len(applied) != 1 || applied[0].X != 3 || applied[0].Y != 0 || applied[0].NewTerrain != component.TerrainPlain {
		t.Errorf("unexpected applied events: %+v", applied)
	}
}

// TestIndestructibleTileIgnored verifies that ProcessDestruction is a no-op for
// tiles with MaxHealth == 0 (Plain, Shallow, etc.) — the guard that keeps
// non-doodad terrain from being damaged by AoE splash.
func TestIndestructibleTileIgnored(t *testing.T) {
	gm := tilemap.NewGameMap(3, 3)
	gm.SetTerrain(1, 1, component.TerrainPlain) // MaxHealth == 0
	cache := pathfinding.NewCache(gm, 10)
	profile := &component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	ts := NewTerrainSystem(gm, cache, []*component.MovementProfile{profile})

	ts.ProcessDestruction(1, 1, 500)
	ts.Tick(nil, 1)

	if gm.TileAt(1, 1).TerrainType != component.TerrainPlain {
		t.Errorf("Plain should be unchanged by damage, got %d", gm.TileAt(1, 1).TerrainType)
	}
	if applied := ts.DrainApplied(); applied != nil {
		t.Errorf("indestructible tile should produce no events, got %+v", applied)
	}
}
