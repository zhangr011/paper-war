package pathfinding

import (
	"testing"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestFlowFieldOpenPlane(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	ff := Compute(gm, 2, 2, profile, 0)
	dir := ff.GetDirection(0, 0)
	if dir.DX <= 0 || dir.DY <= 0 {
		t.Errorf("direction from (0,0) to (2,2) = (%d,%d), want positive", dir.DX, dir.DY)
	}
}

func TestFlowFieldWallBypass(t *testing.T) {
	gm := tilemap.NewTestMap(5, 5, func(x, y int32) component.TerrainType {
		if x == 2 { return component.TerrainWall }
		return component.TerrainPlain
	})
	profile := testInfantryProfile()
	ff := Compute(gm, 4, 2, profile, 0)
	dir := ff.GetDirection(1, 2)
	if dir.DX > 0 && dir.DY == 0 {
		t.Error("(1,2) direction points straight right into wall, should bypass")
	}
}

func TestFlowFieldTargetCell(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	ff := Compute(gm, 2, 2, profile, 0)
	dir := ff.GetDirection(2, 2)
	if dir.DX > fixed.FromFloat(0.1) || dir.DY > fixed.FromFloat(0.1) {
		t.Errorf("target cell should be near zero, got (%d,%d)", dir.DX, dir.DY)
	}
}

func TestFlowFieldImpassable(t *testing.T) {
	gm := tilemap.NewGameMap(3, 3)
	gm.SetTerrain(1, 1, component.TerrainDeep)
	profile := testInfantryProfile()
	ff := Compute(gm, 1, 1, profile, 0)
	_ = ff.GetDirection(0, 0) // should not panic
}

func testInfantryProfile() *component.MovementProfile {
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	p.TerrainCosts[component.TerrainRoad] = 1
	p.TerrainCosts[component.TerrainShallow] = 2
	p.TerrainCosts[component.TerrainForest] = 2
	p.TerrainCosts[component.TerrainBridge] = 1
	return p
}

func TestFlowFieldCache(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 10)

	ff1 := cache.Get(2, 2, profile, 0)
	if ff1 == nil {
		t.Fatal("Get should return a flow field")
	}
	ff2 := cache.Get(2, 2, profile, 0)
	if ff2 != ff1 {
		t.Error("second Get should return same cached flow field")
	}
	ff3 := cache.Get(0, 0, profile, 0)
	if ff3 == ff1 {
		t.Error("different target should return different flow field")
	}
}

func TestFlowFieldCacheEviction(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 2)

	cache.Get(0, 0, profile, 0)
	cache.Get(1, 1, profile, 0)
	cache.Get(2, 2, profile, 0)

	if cache.Size() > 2 {
		t.Errorf("cache size = %d, want <= 2", cache.Size())
	}
}

func TestFlowFieldCacheInvalidate(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 10)

	ff1 := cache.Get(2, 2, profile, 0)
	cache.Invalidate(2, 2, profile)
	ff2 := cache.Get(2, 2, profile, 0)
	if ff2 == ff1 {
		t.Error("after Invalidate, should recompute")
	}
}