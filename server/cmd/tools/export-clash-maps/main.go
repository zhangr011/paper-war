// export-clash-maps — one-shot migration: writes each hand-authored clash map
// (clash_maps.go) to server/data/clash_maps/<name>.json AFTER DeriveElevation,
// i.e. exactly the state live play sees today. Run once from server/:
//
//	go run ./cmd/tools/export-clash-maps
//
// The JSON output then becomes the load path (ADR-0033) and the Go map bodies
// are deleted.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/paper-war/server/pkg/tilemap"
)

func main() {
	names := []string{"plains", "forest", "road", "river", "stronghold", "hills", "hills_validation"}
	outDir := filepath.Join("data", "clash_maps")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	for _, name := range names {
		m := tilemap.LoadClashMap(name) // runs DeriveElevation — capture the live state
		if m == nil {
			fmt.Fprintf(os.Stderr, "%s: LoadClashMap returned nil\n", name)
			os.Exit(1)
		}
		n := int(m.Width) * int(m.Height)
		type snap struct {
			W         int32 `json:"w"`
			H         int32 `json:"h"`
			Terrain   []int `json:"terrain"`
			Elevation []int `json:"elevation"`
		}
		s := snap{W: m.Width, H: m.Height, Terrain: make([]int, n), Elevation: make([]int, n)}
		for i, t := range m.Tiles {
			s.Terrain[i] = int(t.TerrainType)
			s.Elevation[i] = int(t.Elevation)
		}
		raw, err := json.MarshalIndent(s, "", " ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: marshal: %v\n", name, err)
			os.Exit(1)
		}
		path := filepath.Join(outDir, name+".json")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "%s: write: %v\n", name, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(raw))
	}
}
