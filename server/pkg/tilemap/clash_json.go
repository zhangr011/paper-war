package tilemap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/user/paper-war/server/pkg/component"
)

// Clash maps are data files (ADR-0033): server/data/clash_maps/<name>.json in
// the wire shape the editor already speaks ({w,h,terrain,elevation}, row-major,
// []int arrays). The map editor saves via POST /editor/clash-maps/save;
// LoadClashMap reads this directory first, so an edited map takes effect on
// the next match start with no rebuild or restart.
//
// Authored elevation is authoritative on this path — DeriveElevation is NOT
// called (the legacy Go-source path called it because those maps author Hill
// tiles without elevation; JSON authors the elevation grid directly).

// clashMapDir is resolved once; empty means no data dir (legacy path only).
var clashMapDir = resolveClashMapDir()

func resolveClashMapDir() string {
	candidates := []string{
		"data/clash_maps",                   // running from server/
		"../data/clash_maps",                // running from server/cmd/server/
		filepath.Join("..", "..", "data", "clash_maps"), // from server/cmd/tools/*
	}
	if v := os.Getenv("PAPER_WAR_DATA_DIR"); v != "" {
		candidates = append([]string{v}, candidates...)
	}
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}

// clashMapNameRe bounds saved map names: lowercase slug, 1-32 chars. This is
// the path-traversal guard for the save endpoint AND the load lookup.
var clashMapNameRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// ValidClashMapName reports whether name is a loadable/savable clash map slug.
func ValidClashMapName(name string) bool {
	return clashMapNameRe.MatchString(name)
}

// ClashMapNames lists the saved clash maps (basenames without .json).
// Returns nil when no data directory exists.
func ClashMapNames() []string {
	if clashMapDir == "" {
		return nil
	}
	entries, err := os.ReadDir(clashMapDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			if slug := name[:len(name)-5]; ValidClashMapName(slug) {
				names = append(names, slug)
			}
		}
	}
	return names
}

// ClashMapFile is the on-disk JSON shape — identical to the editor's wire
// format (clashMapSnapshot in cmd/server/main.go) so a save round-trips.
type ClashMapFile struct {
	W         int32 `json:"w"`
	H         int32 `json:"h"`
	Terrain   []int `json:"terrain"`
	Elevation []int `json:"elevation"`
}

// LoadClashMapJSON loads one saved clash map by slug. Returns (nil, nil) when
// the name is valid but no such file exists (mirrors LoadClashMap's legacy
// not-found behaviour), and a real error for malformed/invalid data.
func LoadClashMapJSON(name string) (*GameMap, error) {
	if !ValidClashMapName(name) {
		return nil, fmt.Errorf("invalid clash map name %q", name)
	}
	if clashMapDir == "" {
		return nil, nil
	}
	path := filepath.Join(clashMapDir, name+".json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f ClashMapFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return gameMapFromSnapshot(&f)
}

// SaveClashMapJSON validates and atomically writes one clash map.
// Returns the canonical name on success.
func SaveClashMapJSON(name string, f *ClashMapFile) (string, error) {
	if !ValidClashMapName(name) {
		return "", fmt.Errorf("invalid clash map name %q (want [a-z0-9_]{1,32})", name)
	}
	if clashMapDir == "" {
		return "", fmt.Errorf("no clash map data directory resolved")
	}
	if _, err := gameMapFromSnapshot(f); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(clashMapDir, name+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return name, nil
}

// gameMapFromSnapshot builds a GameMap from validated snapshot arrays.
// Destructible terrain gets its HP restored per type (the snapshot wire format
// carries only terrain+elevation — Wall/Rock HP is a constant per type, set
// here rather than growing the wire format).
func gameMapFromSnapshot(f *ClashMapFile) (*GameMap, error) {
	if f.W < 16 || f.W > 64 || f.H < 16 || f.H > 64 {
		return nil, fmt.Errorf("dimensions %dx%d outside 16..64", f.W, f.H)
	}
	n := int(f.W) * int(f.H)
	if len(f.Terrain) != n || len(f.Elevation) != n {
		return nil, fmt.Errorf("array length %d/%d, want %d (w×h)", len(f.Terrain), len(f.Elevation), n)
	}
	m := NewGameMap(f.W, f.H)
	for i := 0; i < n; i++ {
		tt := f.Terrain[i]
		if tt < 0 || tt > int(component.TerrainRamp) {
			return nil, fmt.Errorf("terrain[%d]=%d out of range 0..18", i, tt)
		}
		if tt >= 11 && tt <= 15 {
			return nil, fmt.Errorf("terrain[%d]=%d is a reserved stronghold id", i, tt)
		}
		elev := f.Elevation[i]
		if elev < 0 || elev > 2 {
			return nil, fmt.Errorf("elevation[%d]=%d out of range 0..2", i, elev)
		}
		m.Tiles[i].TerrainType = component.TerrainType(tt)
		m.Tiles[i].Elevation = uint8(elev)
		switch component.TerrainType(tt) {
		case component.TerrainWall:
			m.Tiles[i].Health = wallHealth
			m.Tiles[i].MaxHealth = wallHealth
		case component.TerrainRock:
			m.Tiles[i].Health = rockHealth
			m.Tiles[i].MaxHealth = rockHealth
		}
	}
	return m, nil
}
