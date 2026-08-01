package tilemap

import "github.com/user/paper-war/server/pkg/component"

// DeriveElevation classifies each Hill tile's elevation layer from its
// 8-neighborhood topology: interior hills (≥6 of 8 neighbors also Hill) →
// peak (layer 2); fringe hills → slope (layer 1); non-hill tiles keep the
// implicit 0. Mirrors the procedural generator's "top of the hill cluster
// is the peak" intent using only topology (clash maps carry no heightmap).
//
// Idempotent: overwrites Elevation only on Hill tiles. Safe to call on maps
// that already carry accurate elevation (e.g. GenerateMap output), though
// callers that have a real heightmap should prefer their own assignment.
//
// Issue #49 — discrete 3-layer model. See ADR-0024.
func DeriveElevation(m *GameMap) {
	for y := int32(0); y < m.Height; y++ {
		for x := int32(0); x < m.Width; x++ {
			t := m.TileAt(x, y)
			if t == nil || t.TerrainType != component.TerrainHill {
				continue
			}
			hillNeighbors := 0
			for dy := int32(-1); dy <= 1; dy++ {
				for dx := int32(-1); dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					n := m.TileAt(x+dx, y+dy)
					if n != nil && n.TerrainType == component.TerrainHill {
						hillNeighbors++
					}
				}
			}
			if hillNeighbors >= 6 {
				t.Elevation = 2
			} else {
				t.Elevation = 1
			}
		}
	}
}
