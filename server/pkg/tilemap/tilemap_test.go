package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

func TestNewGameMap(t *testing.T) {
	gm := NewGameMap(4, 4)
	if gm.Width != 4 || gm.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", gm.Width, gm.Height)
	}
	tile := gm.TileAt(0, 0)
	if tile.TerrainType != component.TerrainPlain {
		t.Errorf("default terrain = %d, want plain", tile.TerrainType)
	}
}

func TestSetTerrain(t *testing.T) {
	gm := NewGameMap(4, 4)
	gm.SetTerrain(1, 1, component.TerrainForest)
	tile := gm.TileAt(1, 1)
	if tile.TerrainType != component.TerrainForest {
		t.Errorf("terrain = %d, want forest", tile.TerrainType)
	}
}

func TestCostAt(t *testing.T) {
	profile := component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainForest] = 2
	profile.TerrainCosts[component.TerrainDeep] = 0

	gm := NewGameMap(4, 4)
	gm.SetTerrain(2, 2, component.TerrainForest)
	gm.SetTerrain(3, 3, component.TerrainDeep)

	if gm.CostAt(0, 0, &profile) != 1 {
		t.Errorf("plain cost = %d, want 1", gm.CostAt(0, 0, &profile))
	}
	if gm.CostAt(2, 2, &profile) != 2 {
		t.Errorf("forest cost = %d, want 2", gm.CostAt(2, 2, &profile))
	}
	if gm.CostAt(3, 3, &profile) != 0 {
		t.Errorf("deep cost = %d, want 0", gm.CostAt(3, 3, &profile))
	}
}

func TestOutOfBounds(t *testing.T) {
	gm := NewGameMap(4, 4)
	tile := gm.TileAt(-1, 0)
	if tile != nil {
		t.Error("out of bounds should return nil")
	}
}

func TestNewTestMap(t *testing.T) {
	gm := NewTestMap(5, 5, func(x, y int32) component.TerrainType {
		if x == 2 {
			return component.TerrainWall
		}
		return component.TerrainPlain
	})
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	if gm.CostAt(2, 2, p) != 0 {
		t.Error("wall should be impassable")
	}
	if gm.CostAt(0, 0, p) != 1 {
		t.Error("plain should be passable")
	}
}

func TestGenerateMapCreatesHorizontalRiver(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	midY := gm.Height / 2

	riverCells := 0
	for y := midY - 5; y <= midY+6; y++ {
		for x := int32(0); x < gm.Width; x++ {
			tile := gm.TileAt(x, y)
			if tile == nil {
				continue
			}
			if tile.TerrainType == component.TerrainDeep || tile.TerrainType == component.TerrainBridge {
				riverCells++
			}
		}
	}

	if riverCells < int(gm.Width) {
		t.Fatalf("horizontal river band has %d river cells, want at least %d", riverCells, gm.Width)
	}
}

func TestGenerateMapCreatesVerticalRoads(t *testing.T) {
	gm := GenerateMap(48, 96, 42)

	verticalRoadColumns := 0
	for x := int32(0); x < gm.Width; x++ {
		roadCells := 0
		for y := int32(0); y < gm.Height; y++ {
			tile := gm.TileAt(x, y)
			if tile == nil {
				continue
			}
			if tile.TerrainType == component.TerrainRoad || tile.TerrainType == component.TerrainBridge {
				roadCells++
			}
		}
		if roadCells >= int(gm.Height/2) {
			verticalRoadColumns++
		}
	}

	if verticalRoadColumns < 2 {
		t.Fatalf("vertical road columns = %d, want at least 2", verticalRoadColumns)
	}
}
