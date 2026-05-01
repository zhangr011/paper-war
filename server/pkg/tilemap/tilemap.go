package tilemap

import "github.com/user/paper-war/server/pkg/component"

type Tile struct {
	TerrainType component.TerrainType
	Elevation   int8
	BlockLOS    bool
	Health      int32
	MaxHealth   int32
}

type GameMap struct {
	Width, Height int32
	Tiles         []Tile
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
	m.Tiles[m.index(x, y)].TerrainType = tt
}

func (m *GameMap) CostAt(x, y int32, profile *component.MovementProfile) uint8 {
	if !m.inBounds(x, y) {
		return 0
	}
	tt := m.Tiles[m.index(x, y)].TerrainType
	return profile.TerrainCosts[tt]
}