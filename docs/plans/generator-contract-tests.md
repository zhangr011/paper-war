# Plan — Generator Contract Regression Tests

The recent terrain-polish work (10 commits on `terrain-polish-loop`) and the
clash-elevation fix all depend on the map generator producing specific data:
hill elevation layers, water, forest cover, rock/brush scatter, valid
stronghold specs. Most of that is currently untested — `TestPropertyDeterminism`
and a few others exist, but there's no positive assertion that the features
the shaders render against are actually present. A silent generator change
(e.g. a refactor that drops shallow scatter or flattens elevation) would make
the polish disappear with no test failing.

Lock the contracts with a focused Go test suite. Pure test addition — no
production code changes, zero balance/render risk, fully verifiable.

## Scope — `server/pkg/tilemap/generate_test.go` (extend or add)

Property-style tests over `GenerateMap` across several seeds (deterministic
per seed — pin a fixed set, not `time.Now()`):

1. **Elevation layers present** — every generated map with hills contains BOTH
   layer-1 (slope) and layer-2 (peak) hills, and ZERO layer-0 hills (already
   known true for procedural; codify it). Reference: `generate.go:108-122`.
2. **Water present** — each map has non-zero `TerrainDeep` tiles (river + lake
   budget = `waterFraction * w * h`), and the count is within a sane band
   (e.g. > 0 and < 25% of the map). Guards both "no water" and "flooded".
3. **Forest cover in range** — `TerrainForest` count is > 0 and within a band
   around `forestFraction` (allow generous tolerance since it's
   percentile-derived). Guards the "80% forest" or "no forest" extremes.
4. **Scatter present** — `TerrainRock` and `TerrainBrush` both appear on
   generated maps (applyScatter, `generate.go:442`). Assert each > 0.
5. **No degenerate stronghold specs** — every `gm.Strongholds` spec has
   `Level >= 1` and sits on a passable, non-hill, non-deep tile (matches the
   placement guard at `generate.go:589-596`).
6. **Bridges span water** — every `TerrainBridge` tile is adjacent to at least
   one `TerrainDeep` tile (matches `placeBridges` intent). Skip if no bridges.
7. **Stronghold spacing** — pairwise Manhattan distance between stronghold
   specs ≥ the generator's min spacing (10, `generate.go:575`).

## Clash maps — extend `elevation_test.go`

`TestLoadClashMap_Elevation` already covers elevation. Add one assertion per
clash map that the map is internally consistent: no tile has `Elevation != 0`
unless `TerrainType == TerrainHill` (DeriveElevation must never write elevation
on non-hill tiles — protects the "visual-only, hill-only" invariant).

## Verification

- `cd server && go test ./pkg/tilemap/ -v` — all new tests pass; existing
  suite stays green.
- If any contract assertion reveals a real generator gap (e.g. a seed that
  produces zero rock), DO NOT weaken the test — report it as a finding so it
  can be triaged. Tests assert intended behavior, not current accidents.

## Out of scope

- Any production/generator code change (test-only plan). If a test reveals a
  bug, note it in the report — don't fix it in this pass.
- Client/render tests (WebGL — separate concern).
- Playtest/balance harness (movement outcomes — separate).

## Pointers

- `server/pkg/tilemap/generate.go` — generator under test (water/forest/scatter/
  elevation/stronghold/bridge placement).
- `server/pkg/tilemap/tilemap_test.go` — existing tests to extend/not regress
  (`TestPropertyDeterminism`, `TestGenerateMapHasHills`, `TestPropertyHillCoverage`).
- `server/pkg/tilemap/elevation_test.go` — clash-elevation tests to extend.
- `server/pkg/component/movement.go` — `TerrainType` constants for assertions.
