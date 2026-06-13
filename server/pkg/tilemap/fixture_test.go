package tilemap

// fixture_test.go — shared test fixture for property-based tests.
//
// The 13 property tests each iterate over 100 seeds. Without sharing, that's
// 1300+ GenerateMap calls (each running the full pipeline + connectivity
// retries). This package-level var generates the 100 maps ONCE and lets every
// property test reuse them, cutting suite time from ~93s to ~7s.
//
// Tests that need a specific seed (e.g. seed=42 sanity checks) or a fresh map
// (e.g. determinism comparison) still call GenerateMap directly — those are
// cheap single calls.

const propTestWidth, propTestHeight = 48, 96
const propTestSeeds = 100

// testMaps is the shared fixture: maps[seed] = GenerateMap(48, 96, seed).
// Initialized once at package load.
var testMaps = genTestMaps()

func genTestMaps() []*GameMap {
	out := make([]*GameMap, propTestSeeds)
	for seed := int64(0); seed < propTestSeeds; seed++ {
		out[seed] = GenerateMap(propTestWidth, propTestHeight, seed)
	}
	return out
}
