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

func TestGenerateMapCreatesScatteredStrongholds(t *testing.T) {
	gm := GenerateMap(48, 96, 42)

	countByLevel := map[component.TerrainType]int{}
	total := 0
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			tt := gm.TileAt(x, y).TerrainType
			if !isStrongholdTerrain(tt) {
				continue
			}
			countByLevel[tt]++
			total++
		}
	}

	if total < 80 {
		t.Fatalf("stronghold tile count = %d, want substantial scattered strongholds", total)
	}
	for level := component.TerrainStronghold1; level <= component.TerrainStronghold5; level++ {
		if countByLevel[level] == 0 {
			t.Fatalf("stronghold level %d has no tiles", level-component.TerrainStronghold1+1)
		}
	}
	if countByLevel[component.TerrainStronghold5] <= countByLevel[component.TerrainStronghold1] {
		t.Fatalf("level 5 stronghold area = %d, want larger than level 1 area %d",
			countByLevel[component.TerrainStronghold5], countByLevel[component.TerrainStronghold1])
	}
}

func TestGenerateMapLinksStrongholdsWithRoads(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	strongholdsWithRoad := 0
	strongholdGroups := findStrongholdGroups(gm)

	for _, group := range strongholdGroups {
		if groupTouchesRoad(gm, group) {
			strongholdsWithRoad++
		}
	}

	if len(strongholdGroups) < int((gm.Width*gm.Height)/400)-2 {
		t.Fatalf("stronghold groups = %d, want about one per 20x20 grid", len(strongholdGroups))
	}
	if strongholdsWithRoad == 0 {
		t.Fatalf("strongholds linked to roads = 0/%d, want a sparse road network", len(strongholdGroups))
	}
	if strongholdsWithRoad >= len(strongholdGroups) {
		t.Fatalf("strongholds linked to roads = %d/%d, want some strongholds left off-road", strongholdsWithRoad, len(strongholdGroups))
	}
}

func TestStrongholdRoadsAreNotOnlyOrthogonalCorridors(t *testing.T) {
	gm := GenerateMap(48, 96, 42)

	if !hasDiagonalRoadStep(gm) {
		t.Fatalf("stronghold road network has no diagonal or staggered steps")
	}
}

func isStrongholdTerrain(tt component.TerrainType) bool {
	return tt >= component.TerrainStronghold1 && tt <= component.TerrainStronghold5
}

func findStrongholdGroups(gm *GameMap) [][][2]int32 {
	visited := make(map[[2]int32]bool)
	var groups [][][2]int32
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			start := [2]int32{x, y}
			if visited[start] || !isStrongholdTerrain(gm.TileAt(x, y).TerrainType) {
				continue
			}
			var group [][2]int32
			queue := [][2]int32{start}
			visited[start] = true
			for len(queue) > 0 {
				cell := queue[0]
				queue = queue[1:]
				group = append(group, cell)
				for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					next := [2]int32{cell[0] + d[0], cell[1] + d[1]}
					if visited[next] {
						continue
					}
					tile := gm.TileAt(next[0], next[1])
					if tile == nil || !isStrongholdTerrain(tile.TerrainType) {
						continue
					}
					visited[next] = true
					queue = append(queue, next)
				}
			}
			groups = append(groups, group)
		}
	}
	return groups
}

func groupTouchesRoad(gm *GameMap, group [][2]int32) bool {
	for _, cell := range group {
		for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			tile := gm.TileAt(cell[0]+d[0], cell[1]+d[1])
			if tile == nil {
				continue
			}
			if tile.TerrainType == component.TerrainRoad || tile.TerrainType == component.TerrainBridge {
				return true
			}
		}
	}
	return false
}

func hasDiagonalRoadStep(gm *GameMap) bool {
	for y := int32(1); y < gm.Height-1; y++ {
		for x := int32(1); x < gm.Width-1; x++ {
			if !isRoadLike(gm.TileAt(x, y).TerrainType) {
				continue
			}
			if (isRoadLike(gm.TileAt(x+1, y+1).TerrainType) ||
				isRoadLike(gm.TileAt(x-1, y+1).TerrainType)) &&
				!isRoadLike(gm.TileAt(x, y+1).TerrainType) {
				return true
			}
		}
	}
	return false
}

func isRoadLike(tt component.TerrainType) bool {
	return tt == component.TerrainRoad || tt == component.TerrainBridge
}
