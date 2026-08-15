package tilemap

import (
	"math/rand"
)

// Clash maps are JSON data files (ADR-0033): server/data/clash_maps/<name>.json.
// The seven original maps were migrated from the deleted Go-source bodies by
// server/cmd/tools/export-clash-maps (run through LoadClashMap, so post-
// DeriveElevation — exactly the state live play saw). This file now only
// routes names to the JSON loader.

// LoadClashMap returns a pre-designed clash map by name.
// Returns nil if the name is not recognized.
//
// The JSON path does NOT run DeriveElevation — the elevation grid is authored
// directly in the file (the legacy Go path needed derivation because those
// maps authored Hill tiles without elevation).
func LoadClashMap(name string) *GameMap {
	if name == "random" {
		names := []string{"plains", "forest", "road", "river", "stronghold", "hills"}
		name = names[rand.Intn(len(names))]
	}
	m, _ := LoadClashMapJSON(name)
	return m
}
