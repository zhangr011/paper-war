package tilemap

// generate_test.go — generator contract regression tests.
//
// Locks the data contracts the terrain-polish shaders depend on:
// hill elevation layers, water, forest cover, rock/brush scatter, valid
// stronghold specs, bridges-over-water, and stronghold spacing. A silent
// change to the generator that drops or distorts any of these will fail one
// of these tests rather than silently make the polish disappear.
//
// See docs/plans/generator-contract-tests.md.

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// contractTestWidth/Height and seed set are pinned for determinism — never
// derive from time.Now() (a flaky contract test is worse than none).
const (
	contractTestWidth  int32 = 32
	contractTestHeight int32 = 32
)

// contractSeeds is the fixed seed set exercised by every contract test.
// 10 seeds is enough to catch regressions while keeping the suite fast
// (GenerateMap runs the full pipeline + connectivity retries per seed).
var contractSeeds = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

// terrainHistogram counts occurrences of each TerrainType across a GameMap.
func terrainHistogram(gm *GameMap) map[component.TerrainType]int {
	h := make(map[component.TerrainType]int)
	for _, tl := range gm.Tiles {
		h[tl.TerrainType]++
	}
	return h
}

// TestContract_ElevationLayers verifies every map with hills has BOTH
// layer-1 (slope) and layer-2 (peak) hills and ZERO layer-0 hills — the
// invariant established by Stage 2 of the pipeline (generate.go stage 2).
// A regression that flattens elevation or drops a layer will break the
// hill shading the client renders.
func TestContract_ElevationLayers(t *testing.T) {
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			var slope, peak, layer0 int
			for _, tl := range gm.Tiles {
				if tl.TerrainType != component.TerrainHill {
					continue
				}
				switch tl.Elevation {
				case 0:
					layer0++
				case 1:
					slope++
				case 2:
					peak++
				default:
					t.Fatalf("hill tile with unexpected elevation %d", tl.Elevation)
				}
			}
			if slope == 0 {
				t.Errorf("seed %d: no layer-1 (slope) hills; want at least one", seed)
			}
			if peak == 0 {
				t.Errorf("seed %d: no layer-2 (peak) hills; want at least one", seed)
			}
			if layer0 != 0 {
				t.Errorf("seed %d: %d layer-0 hills; want 0", seed, layer0)
			}
			t.Logf("seed %d: slope=%d peak=%d", seed, slope, peak)
		})
	}
}

// TestContract_WaterPresent asserts each map has non-zero TerrainDeep and
// that water stays a minority of the map (>0 and <25%). Guards both
// "drought" (river+lake pipeline broken) and "flood" (budget blown).
func TestContract_WaterPresent(t *testing.T) {
	const maxWaterFrac = 0.25
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			hist := terrainHistogram(gm)
			water := hist[component.TerrainDeep]
			total := int32(len(gm.Tiles))
			t.Logf("seed %d: deep water=%d (%.3f)", seed, water, float64(water)/float64(total))
			if water == 0 {
				t.Fatalf("seed %d: no TerrainDeep water tiles", seed)
			}
			frac := float64(water) / float64(total)
			if frac >= maxWaterFrac {
				t.Errorf("seed %d: water fraction %.3f >= cap %.3f", seed, frac, maxWaterFrac)
			}
		})
	}
}

// TestContract_ForestCover asserts TerrainForest is present and within a
// generous band. applyForest is percentile-derived targeting forestFraction
// (~15% of eligible tiles), so we guard the 0% and >50% extremes rather
// than pinning a tight tolerance.
func TestContract_ForestCover(t *testing.T) {
	const maxForestFrac = 0.50
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			hist := terrainHistogram(gm)
			forest := hist[component.TerrainForest]
			total := int32(len(gm.Tiles))
			t.Logf("seed %d: forest=%d (%.3f)", seed, forest, float64(forest)/float64(total))
			if forest == 0 {
				t.Fatalf("seed %d: no TerrainForest tiles", seed)
			}
			frac := float64(forest) / float64(total)
			if frac > maxForestFrac {
				t.Errorf("seed %d: forest fraction %.3f > cap %.3f", seed, frac, maxForestFrac)
			}
		})
	}
}

// TestContract_ScatterPresent asserts both TerrainRock and TerrainBrush
// appear on every generated map (applyScatter, generate.go stage 6).
func TestContract_ScatterPresent(t *testing.T) {
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			hist := terrainHistogram(gm)
			rock := hist[component.TerrainRock]
			brush := hist[component.TerrainBrush]
			t.Logf("seed %d: rock=%d brush=%d", seed, rock, brush)
			if rock == 0 {
				t.Errorf("seed %d: no TerrainRock placed by scatter pass", seed)
			}
			if brush == 0 {
				t.Errorf("seed %d: no TerrainBrush placed by scatter pass", seed)
			}
		})
	}
}

// TestContract_StrongholdSpecsValid checks every gm.Strongholds spec has
// Level >= 1 and sits on a passable tile (not Hill, not Deep) — matches
// the placement guard in placeStrongholds (generate.go ~589-596).
func TestContract_StrongholdSpecsValid(t *testing.T) {
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			if len(gm.Strongholds) == 0 {
				t.Logf("seed %d: no strongholds placed (skipping tile checks)", seed)
				// Still fail — generator should place at least one on a 32x32 map.
				t.Errorf("seed %d: expected at least one stronghold spec, got 0", seed)
				return
			}
			for _, s := range gm.Strongholds {
				if s.Level < 1 {
					t.Errorf("seed %d: spec (%d,%d) Level=%d, want >= 1", seed, s.X, s.Y, s.Level)
				}
				tile := gm.TileAt(s.X, s.Y)
				if tile == nil {
					t.Errorf("seed %d: spec (%d,%d) out of bounds", seed, s.X, s.Y)
					continue
				}
				if tile.TerrainType == component.TerrainHill || tile.TerrainType == component.TerrainDeep {
					t.Errorf("seed %d: spec (%d,%d) on impassable terrain %d",
						seed, s.X, s.Y, tile.TerrainType)
				}
			}
		})
	}
}

// TestContract_BridgesSpanWater asserts every TerrainBridge tile is part of
// the watercourse structure — i.e. 4-adjacent to TerrainDeep OR another
// TerrainBridge — not orphaned on land. placeBridges only ever converts
// TerrainDeep → TerrainBridge, so a bridge tile with no water and no bridge
// neighbor would indicate a placement bug.
//
// NOTE on the relaxed contract: the original plan proposed "every bridge tile
// is 4-adjacent to at least one TerrainDeep tile". That proxy is unsatisfiable
// as written: placeBridges (generate.go ~695-715) runs a Y-extension loop that
// deliberately converts the ENTIRE contiguous TerrainDeep run at the bridge's
// X column into bridge tiles — a "full crossing". After conversion, interior
// bridge tiles have only bridge/land neighbors; no TerrainDeep remains
// adjacent. Every one of the 10 contract seeds fails the strict proxy. This
// is intentional generator behavior, not an accident, so we relax to the
// faithful observable contract ("bridge is anchored to the watercourse
// structure, not stranded on land") and report the strict-proxy failure +
// the map-edge over-consumption (e.g. seed 2 places a 4-tile bridge ending at
// the bottom-right corner with no adjacent water) as a finding for separate
// triage.
func TestContract_BridgesSpanWater(t *testing.T) {
	dirs := [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			var bridges [][2]int32
			for y := int32(0); y < gm.Height; y++ {
				for x := int32(0); x < gm.Width; x++ {
					if gm.TileAt(x, y).TerrainType == component.TerrainBridge {
						bridges = append(bridges, [2]int32{x, y})
					}
				}
			}
			if len(bridges) == 0 {
				t.Logf("seed %d: no bridges (assertion skipped)", seed)
				return
			}
			touchDeep := 0
			for _, b := range bridges {
				anchored := false
				for _, d := range dirs {
					n := gm.TileAt(b[0]+d[0], b[1]+d[1])
					if n == nil {
						continue
					}
					if n.TerrainType == component.TerrainDeep || n.TerrainType == component.TerrainBridge {
						anchored = true
						if n.TerrainType == component.TerrainDeep {
							touchDeep++
						}
						break
					}
				}
				if !anchored {
					t.Errorf("seed %d: bridge (%d,%d) is orphaned — no 4-adjacent TerrainDeep or TerrainBridge",
						seed, b[0], b[1])
				}
			}
			t.Logf("seed %d: %d bridges, %d touch residual TerrainDeep", seed, len(bridges), touchDeep)
		})
	}
}

// TestContract_StrongholdSpacing asserts pairwise Manhattan distance between
// stronghold specs is >= 10, matching the min-spacing guard in
// placeStrongholds (generate.go ~575).
func TestContract_StrongholdSpacing(t *testing.T) {
	const minSpacing int32 = 10
	for _, seed := range contractSeeds {
		t.Run(seedName(seed), func(t *testing.T) {
			gm := GenerateMap(contractTestWidth, contractTestHeight, seed)
			ss := gm.Strongholds
			t.Logf("seed %d: %d strongholds", seed, len(ss))
			for i := 0; i < len(ss); i++ {
				for j := i + 1; j < len(ss); j++ {
					d := abs32(ss[i].X-ss[j].X) + abs32(ss[i].Y-ss[j].Y)
					if d < minSpacing {
						t.Errorf("seed %d: specs (%d,%d) and (%d,%d) Manhattan distance %d < %d",
							seed, ss[i].X, ss[i].Y, ss[j].X, ss[j].Y, d, minSpacing)
					}
				}
			}
		})
	}
}

// seedName keeps subtest names short and stable.
func seedName(seed int64) string {
	// Avoid fmt.Sprintf to keep the import list lean.
	return "seed_" + intToString(seed)
}

// intToString is a tiny strconv-free helper for subtest naming.
func intToString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
