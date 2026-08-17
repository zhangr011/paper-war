package tilemap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// The migration gate (TestClashJSONMatchesLegacyGo, comparing the JSON files
// to the deleted Go-source map bodies) passed on 2026-08-15 and the Go bodies
// were removed (ADR-0033). What remains guards the JSON path itself.

// TestClashJSONLoadable: every saved map loads with sane dimensions, and the
// hills fixtures keep their authored terrain through the load (ramps at
// elevation 2, destructibles with HP).
func TestClashJSONLoadable(t *testing.T) {
	if clashMapDir == "" {
		t.Skip("no clash map data dir resolved")
	}
	names := ClashMapNames()
	if len(names) == 0 {
		t.Fatal("clash map data dir exists but lists no maps")
	}
	for _, name := range names {
		m, err := LoadClashMapJSON(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if m == nil {
			t.Errorf("%s: not found", name)
			continue
		}
		if m.Width < 16 || m.Height < 16 {
			t.Errorf("%s: %dx%d", name, m.Width, m.Height)
		}
	}
	// Spot-check the migrated hills fixture: ramps + destructibles survive.
	m, err := LoadClashMapJSON("hills_validation")
	if err != nil || m == nil {
		t.Fatalf("hills_validation: %v %v", m, err)
	}
	// Ramp authored at the lower band of its ridge (ADR-0034 convention).
	if tl := m.TileAt(13, 7); tl == nil || tl.TerrainType != component.TerrainRamp || tl.Elevation != 1 {
		t.Errorf("ramp (13,7): want Ramp/1")
	}
	if tl := m.TileAt(16, 16); tl == nil || tl.TerrainType != component.TerrainWall || tl.Health != wallHealth {
		t.Errorf("wall (16,16): want Wall HP %d", wallHealth)
	}
}

// TestClashJSONValidation: malformed snapshots are rejected with errors, not
// silently loaded.
func TestClashJSONValidation(t *testing.T) {
	base := func() *ClashMapFile {
		n := 32 * 32
		return &ClashMapFile{W: 32, H: 32, Terrain: make([]int, n), Elevation: make([]int, n)}
	}
	cases := []struct {
		name string
		f    func() *ClashMapFile
	}{
		{"dims too small", func() *ClashMapFile { f := base(); f.W, f.H = 8, 8; return f }},
		{"dims too large", func() *ClashMapFile { f := base(); f.Terrain = make([]int, 65*65); f.Elevation = make([]int, 65*65); f.W, f.H = 65, 65; return f }},
		{"length mismatch", func() *ClashMapFile { f := base(); f.Terrain = f.Terrain[:100]; return f }},
		{"terrain out of range", func() *ClashMapFile { f := base(); f.Terrain[5] = 19; return f }},
		{"reserved terrain id", func() *ClashMapFile { f := base(); f.Terrain[5] = 12; return f }},
		{"elevation out of range", func() *ClashMapFile { f := base(); f.Elevation[5] = 3; return f }},
	}
	for _, tc := range cases {
		if _, err := gameMapFromSnapshot(tc.f()); err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
	}
	// Sanity: the base snapshot itself is valid.
	if _, err := gameMapFromSnapshot(base()); err != nil {
		t.Errorf("base snapshot unexpectedly invalid: %v", err)
	}
}

// TestClashMapNameValidation: the slug regex is the path-traversal guard.
func TestClashMapNameValidation(t *testing.T) {
	for _, good := range []string{"plains", "hills_validation", "my_map_2", "a"} {
		if !ValidClashMapName(good) {
			t.Errorf("%q should be valid", good)
		}
	}
	for _, bad := range []string{"", "../x", "..\\x", "Map", "UPPER", "has space", "slash/x", "a-name"} {
		if ValidClashMapName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

// TestClashJSONSaveRoundTrip: Save → Load returns identical terrain/elevation,
// and destructible HP is restored per type on load.
func TestClashJSONSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prev := clashMapDir
	clashMapDir = dir
	defer func() { clashMapDir = prev }()

	f := &ClashMapFile{W: 32, H: 32, Terrain: make([]int, 1024), Elevation: make([]int, 1024)}
	f.Terrain[10] = int(component.TerrainWall)
	f.Terrain[20] = int(component.TerrainRock)
	f.Terrain[30] = int(component.TerrainHill)
	f.Elevation[30] = 2
	if _, err := SaveClashMapJSON("test_roundtrip", f); err != nil {
		t.Fatalf("save: %v", err)
	}
	m, err := LoadClashMapJSON("test_roundtrip")
	if err != nil || m == nil {
		t.Fatalf("load: %v %v", m, err)
	}
	if m.Tiles[10].Health != wallHealth || m.Tiles[20].Health != rockHealth {
		t.Errorf("destructible HP not restored: wall=%d rock=%d", m.Tiles[10].Health, m.Tiles[20].Health)
	}
	if m.Tiles[30].Elevation != 2 {
		t.Errorf("elevation not restored: %d", m.Tiles[30].Elevation)
	}
	// No stray tmp file.
	if _, err := os.Stat(filepath.Join(dir, "test_roundtrip.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("temp file left behind")
	}
	// Invalid name rejected by save.
	if _, err := SaveClashMapJSON("../evil", f); err == nil {
		t.Errorf("path traversal name accepted by save")
	}
}

// TestClashJSONAllConnected: every saved map must stay fully connected for
// both movement profiles — same guard the editor's live check enforces.
func TestClashJSONAllConnected(t *testing.T) {
	for _, name := range ClashMapNames() {
		m, err := LoadClashMapJSON(name)
		if err != nil || m == nil {
			t.Errorf("%s: load %v %v", name, m, err)
			continue
		}
		for i, p := range component.StandardMovementProfiles() {
			if !m.ConnectedFor(p) {
				t.Errorf("%s not connected for profile %d", name, i)
			}
		}
	}
}

// TestElevationCliffOnAnyTerrain: a Δ2 elevation step blocks movement between
// two PLAIN tiles (elevation authored on non-Hill terrain, ADR-0033), and a
// Ramp on either end restores the crossing. Guards the terrain-agnostic cliff
// rule now that the editor authors elevation on arbitrary tiles.
func TestElevationCliffOnAnyTerrain(t *testing.T) {
	m := NewGameMap(32, 32)
	light := component.StandardMovementProfiles()[0]
	// Baseline: flat plains walk.
	if w, _ := m.EdgeWalkableFor(10, 10, 11, 10, light, 0); !w {
		t.Fatal("flat plains step should be walkable")
	}
	// Plain tile raised to peak: Δ2 step is a cliff.
	m.TileAt(11, 10).Elevation = 2
	if w, _ := m.EdgeWalkableFor(10, 10, 11, 10, light, 0); w {
		t.Fatal("Δ2 step between plains should be a cliff")
	}
	// Δ1 remains walkable.
	m.TileAt(11, 10).Elevation = 1
	if w, _ := m.EdgeWalkableFor(10, 10, 11, 10, light, 0); !w {
		t.Fatal("Δ1 step should be walkable")
	}
	// Restore Δ2; a Ramp on the destination reopens the crossing.
	m.TileAt(11, 10).Elevation = 2
	m.SetTerrain(11, 10, component.TerrainRamp)
	if w, _ := m.EdgeWalkableFor(10, 10, 11, 10, light, 0); !w {
		t.Fatal("Δ2 step across a Ramp tile should be walkable")
	}
}
