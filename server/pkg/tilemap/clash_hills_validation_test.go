package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// TestClashHillsValidation_Features is the regression guard for the
// hills_validation fixture (docs/terrain-readability-plan.md Phase 0). It
// asserts the authored terrain survives LoadClashMap — which runs
// DeriveElevation and could otherwise silently re-grade the hills — exactly
// where the integration tests and playtest harness rely on it: Ramp crossings
// at the pass edges with explicit Elevation 2, destructible Walls/Rocks with
// HP on the valley mid-line, the carved central pass, and a peak in the ridge
// interior. It also guards that the fixture stays fully connected for both
// movement profiles — a wall blocking the only N-S route would silently break
// every clash played on this map.
func TestClashHillsValidation_Features(t *testing.T) {
	m := LoadClashMap("hills_validation")
	if m == nil {
		t.Fatalf("LoadClashMap(\"hills_validation\") returned nil")
	}
	if m.Width != 32 || m.Height != 32 {
		t.Fatalf("fixture is %dx%d, want 32x32", m.Width, m.Height)
	}

	// Ramps flank each pass at Elevation 2 — the authored Δ2 cliff crossing.
	// Ramp is not Hill, so DeriveElevation must leave these at 2.
	rampCells := [][2]int32{
		{13, 7}, {19, 7}, {13, 8}, {19, 8},
		{13, 23}, {19, 23}, {13, 24}, {19, 24},
	}
	for _, c := range rampCells {
		tl := m.TileAt(c[0], c[1])
		if tl == nil {
			t.Fatalf("nil tile at ramp %v", c)
		}
		if tl.TerrainType != component.TerrainRamp {
			t.Errorf("ramp %v: terrain=%d, want Ramp(18)", c, tl.TerrainType)
		}
		if tl.Elevation != 2 {
			t.Errorf("ramp %v: elevation=%d, want 2 (DeriveElevation must not touch Ramp)", c, tl.Elevation)
		}
	}

	// Central pass (col cmidX) carved back to Plain at Elevation 0 through
	// each ridge — the no-cost route under the cliffs.
	for _, pc := range [][2]int32{{16, 7}, {16, 8}, {16, 23}, {16, 24}} {
		tl := m.TileAt(pc[0], pc[1])
		if tl == nil || tl.TerrainType != component.TerrainPlain || tl.Elevation != 0 {
			t.Errorf("pass %v: terrain=%d elev=%d, want Plain/0", pc, terrainOf(tl), elevOf(tl))
		}
	}

	// Ridge interior peak: far from the pass, ≥6 Hill neighbours → peak (2).
	interior := m.TileAt(5, 7)
	if interior == nil || interior.Elevation != 2 {
		t.Errorf("ridge interior (5,7): elev=%d, want peak 2", elevOf(interior))
	}

	// Destructible Walls across the mid-line with full HP.
	for _, wc := range [][2]int32{{15, 16}, {16, 16}, {17, 16}} {
		tl := m.TileAt(wc[0], wc[1])
		if tl == nil || tl.TerrainType != component.TerrainWall {
			t.Errorf("wall %v: terrain=%d, want Wall", wc, terrainOf(tl))
			continue
		}
		if tl.Health != wallHealth || tl.MaxHealth != wallHealth {
			t.Errorf("wall %v: HP=%d/%d, want %d", wc, tl.Health, tl.MaxHealth, wallHealth)
		}
	}

	// Destructible Rocks flanking the wall with full HP.
	for _, rc := range [][2]int32{{11, 16}, {21, 16}} {
		tl := m.TileAt(rc[0], rc[1])
		if tl == nil || tl.TerrainType != component.TerrainRock {
			t.Errorf("rock %v: terrain=%d, want Rock", rc, terrainOf(tl))
			continue
		}
		if tl.Health != rockHealth || tl.MaxHealth != rockHealth {
			t.Errorf("rock %v: HP=%d/%d, want %d", rc, tl.Health, tl.MaxHealth, rockHealth)
		}
	}

	// Connectivity guard for both movement profiles. The wall + rocks sit on
	// the central axis; this asserts they don't seal the only N-S route.
	for i, p := range component.StandardMovementProfiles() {
		if !m.ConnectedFor(p) {
			t.Errorf("fixture not fully connected for movement profile %d", i)
		}
	}
}

func terrainOf(t *Tile) component.TerrainType {
	if t == nil {
		return 255
	}
	return t.TerrainType
}

func elevOf(t *Tile) uint8 {
	if t == nil {
		return 255
	}
	return t.Elevation
}
