package tilemap

import "github.com/user/paper-war/server/pkg/component"

type Tile struct {
	TerrainType component.TerrainType
	// Elevation is a discrete hill-layer band, only meaningful when
	// TerrainType == TerrainHill. Values: 0 = low (implicit for non-hill
	// tiles), 1 = mid (hill slope), 2 = peak (rocky summit). Issue #49.
	Elevation uint8
	BlockLOS  bool
	Health    int32
	MaxHealth int32
}

type ObjectiveType uint8

const (
	ObjectiveElimination ObjectiveType = 0
	ObjectiveCapture    ObjectiveType = 1
	ObjectiveSurvival   ObjectiveType = 2
)

type Objective struct {
	Type        ObjectiveType
	TargetX     int32 // Capture: stronghold tile X
	TargetY     int32 // Capture: stronghold tile Y
	HoldTarget  int32 // Capture: ticks needed to hold (300)
	HoldCounter int32 // Capture: current progress
	Duration    int32 // Survival: total ticks
}

type GameMap struct {
	Width, Height int32
	Tiles         []Tile
	Objective     Objective
	Spawns        [][2]int32 // generator-placed spawn positions
	Seed          int64      // seed for debugging/reproducibility
}

func NewGameMap(w, h int32) *GameMap {
	tiles := make([]Tile, w*h)
	for i := range tiles {
		tiles[i] = Tile{TerrainType: component.TerrainPlain}
	}
	return &GameMap{Width: w, Height: h, Tiles: tiles}
}

func NewTestMap(w, h int32, fn func(x, y int32) component.TerrainType) *GameMap {
	gm := NewGameMap(w, h)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			gm.SetTerrain(x, y, fn(x, y))
		}
	}
	return gm
}

func (m *GameMap) index(x, y int32) int {
	return int(y*m.Width + x)
}

func (m *GameMap) inBounds(x, y int32) bool {
	return x >= 0 && x < m.Width && y >= 0 && y < m.Height
}

func (m *GameMap) TileAt(x, y int32) *Tile {
	if !m.inBounds(x, y) {
		return nil
	}
	return &m.Tiles[m.index(x, y)]
}

func (m *GameMap) SetTerrain(x, y int32, tt component.TerrainType) {
	if !m.inBounds(x, y) {
		return
	}
	t := &m.Tiles[m.index(x, y)]
	t.TerrainType = tt
	// Keep BlockLOS in sync with terrain so every placement (generator, clash
	// maps, editor-pasted maps) is correct without each site re-deriving it.
	// Issue #55 phase 2 — retires BlockLOS as a dead field.
	t.BlockLOS = component.BlocksLOS(tt)
}

func (m *GameMap) CostAt(x, y int32, profile *component.MovementProfile) uint8 {
	if !m.inBounds(x, y) {
		return 0
	}
	tt := m.Tiles[m.index(x, y)].TerrainType
	return profile.TerrainCosts[tt]
}