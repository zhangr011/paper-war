package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// TestDeriveElevation_3x3Block verifies the topology classification on a
// hand-built map: a 3×3 hill block surrounded by plain. Center (8 hill
// neighbors) → peak (2); the 8 surrounding hill tiles (5 hill neighbors
// each) → slope (1); plain tiles → 0.
func TestDeriveElevation_3x3Block(t *testing.T) {
	const w, h int32 = 7, 7
	m := NewGameMap(w, h)
	for dy := int32(0); dy < 3; dy++ {
		for dx := int32(0); dx < 3; dx++ {
			m.SetTerrain(2+dx, 2+dy, component.TerrainHill)
		}
	}

	DeriveElevation(m)

	// Center is peak.
	center := m.TileAt(3, 3)
	if center.Elevation != 2 {
		t.Fatalf("center tile elevation = %d, want 2 (peak)", center.Elevation)
	}

	// The 8 surrounding hill tiles are slope.
	for dy := int32(0); dy < 3; dy++ {
		for dx := int32(0); dx < 3; dx++ {
			x, y := 2+dx, 2+dy
			if x == 3 && y == 3 {
				continue
			}
			tl := m.TileAt(x, y)
			if tl.Elevation != 1 {
				t.Fatalf("hill tile (%d,%d) elevation = %d, want 1 (slope)", x, y, tl.Elevation)
			}
		}
	}

	// Every plain tile has implicit elevation 0.
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			tl := m.TileAt(x, y)
			if tl.TerrainType != component.TerrainHill && tl.Elevation != 0 {
				t.Fatalf("non-hill tile (%d,%d) elevation = %d, want 0", x, y, tl.Elevation)
			}
		}
	}
}

// TestDeriveElevation_Idempotent verifies that running DeriveElevation twice
// produces the same result (no drift, no corruption of non-hill tiles).
func TestDeriveElevation_Idempotent(t *testing.T) {
	m := NewGameMap(5, 5)
	for dy := int32(0); dy < 3; dy++ {
		for dx := int32(0); dx < 3; dx++ {
			m.SetTerrain(1+dx, 1+dy, component.TerrainHill)
		}
	}
	DeriveElevation(m)
	first := make([]uint8, len(m.Tiles))
	for i, tl := range m.Tiles {
		first[i] = tl.Elevation
	}
	DeriveElevation(m)
	for i, tl := range m.Tiles {
		if tl.Elevation != first[i] {
			t.Fatalf("tile %d elevation drift: first=%d second=%d", i, first[i], tl.Elevation)
		}
	}
}

// TestLoadClashMap_Elevation asserts that each named clash map, if it has
// any Hill tiles, has BOTH layer-1 (slope) and layer-2 (peak) hills and ZERO
// layer-0 hills (the bug being fixed).
func TestLoadClashMap_Elevation(t *testing.T) {
	names := []string{"plains", "forest", "road", "river", "stronghold", "hills"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			m := LoadClashMap(name)
			if m == nil {
				t.Fatalf("LoadClashMap(%q) returned nil", name)
			}
			var hillCount, slope, peak, layer0 int
			for _, tl := range m.Tiles {
				if tl.TerrainType != component.TerrainHill {
					continue
				}
				hillCount++
				switch tl.Elevation {
				case 0:
					layer0++
				case 1:
					slope++
				case 2:
					peak++
				default:
					t.Fatalf("%s: hill tile with unexpected elevation %d", name, tl.Elevation)
				}
			}
			if hillCount == 0 {
				t.Logf("%s: no hill tiles (assertion trivially satisfied)", name)
				return
			}
			if layer0 != 0 {
				t.Errorf("%s: %d/%d hill tiles are layer-0 (valley); want 0", name, layer0, hillCount)
			}
			if slope == 0 {
				t.Errorf("%s: no layer-1 (slope) hills; want at least one", name)
			}
			if peak == 0 {
				t.Errorf("%s: no layer-2 (peak) hills; want at least one", name)
			}
			t.Logf("%s: %d hill tiles — %d slope, %d peak", name, hillCount, slope, peak)
		})
	}
}

// TestLoadClashMap_ElevationHillOnly asserts the "elevation is hill-only"
// invariant of DeriveElevation: no tile has Elevation != 0 unless its
// TerrainType is TerrainHill or TerrainRamp. Ramp is the one exception — it
// carries an explicit elevation (set by the map author) so the cliff-crossing
// edge rule in tilemap.EdgeWalkable can fire (Phase 1). Protects the
// visual-only, hill-only contract against stray elevation elsewhere.
func TestLoadClashMap_ElevationHillOnly(t *testing.T) {
	names := []string{"plains", "forest", "road", "river", "stronghold", "hills"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			m := LoadClashMap(name)
			if m == nil {
				t.Fatalf("LoadClashMap(%q) returned nil", name)
			}
			for y := int32(0); y < m.Height; y++ {
				for x := int32(0); x < m.Width; x++ {
					tl := m.TileAt(x, y)
					if tl.TerrainType != component.TerrainHill &&
						tl.TerrainType != component.TerrainRamp &&
						tl.Elevation != 0 {
						t.Errorf("%s: tile (%d,%d) terrain=%d has Elevation=%d; want 0 on non-hill/non-ramp",
							name, x, y, tl.TerrainType, tl.Elevation)
					}
				}
			}
		})
	}
}
