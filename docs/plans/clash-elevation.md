# Plan — Populate Elevation on Clash Maps

Clash maps set `TerrainHill` tiles but never assign `Elevation`, so every
clash-map hill is layer 0 (valley). The elevation-aware hill shader (commit
`850a5a8`, ADR-0024 elevation model) therefore renders no peaks or slopes on
clash/crash maps — the very maps used by the spectator harness and the
crash-restart e2e spec. Procedural (`GenerateMap`) maps are unaffected; they
assign elevation correctly (`generate.go:116,118`).

Confirmed via a Go diagnostic: 5 seeds of `GenerateMap(32,32)` all produce a
mix of layer-1 (slope) and layer-2 (peak) hills, zero layer-0 hills. Clash
maps produce all layer-0 hills because `clash_maps.go` has no `Elevation`
references at all.

## Approach

Derive elevation from hill topology in a shared post-process, so every clash
map gets peaks in the interior of hill clusters and slopes at the fringes —
matching the procedural model's semantics (peak = highest, slope = mid,
valley = low/implicit).

### 1. New helper — `server/pkg/tilemap/elevation.go`

```go
// DeriveElevation classifies each Hill tile's elevation layer from its
// neighborhood: interior hills (≥6 of 8 neighbors also Hill) → peak (2);
// fringe hills → slope (1); non-hill tiles keep the implicit 0. Mirrors the
// procedural generator's "top 25% of hills are peaks" intent using only
// topology (clash maps have no heightmap). Idempotent; safe to call on maps
// that already carry elevation (it overwrites only Hill tiles).
func DeriveElevation(m *GameMap) { ... }
```

8-neighborhood count (Chebyshev), threshold 6 of 8 for peak. Bounds-safe.

### 2. Wire into `LoadClashMap` — `server/pkg/tilemap/clash_maps.go`

Call `DeriveElevation(m)` once at the end of `LoadClashMap` (after the
`switch` constructs the map, before returning it) so all six clash maps
benefit and any future clash map is covered automatically. Do NOT edit each
`ClashXxx()` authoring function — one call site, can't be forgotten.

Procedural `GenerateMap` keeps its heightmap-based assignment; do not route
it through `DeriveElevation` (the heightmap is more accurate than topology).

## Verification

- New test `server/pkg/tilemap/elevation_test.go`:
  - For each of the six clash maps returned by `LoadClashMap`, assert: if the
    map contains any Hill tiles, it contains **both** layer-1 and layer-2
    hills, and **zero** layer-0 hills. (Maps with no hills skip.)
  - A direct unit test of `DeriveElevation` on a hand-built map: a 3×3 hill
    block → center is peak (2), edges are slope (1), surrounded plain is 0.
- Re-run the existing `tilemap` test suite — must stay green (derivation only
  writes `Elevation` on Hill tiles; `TerrainType`, `BlockLOS`, `Health`
  untouched, so movement/LOS/cost tests are unaffected).
- Manual eyeball (deferred — needs a real display): a clash `hills` map
  should now show rocky summits and shaded slopes instead of uniform flat
  hills.

## Out of scope

- Changing procedural-map elevation (already correct).
- Changing elevation semantics or the wire format (Elevation already flows
  to the client via `session.go:1989`).
- Any client/render change (the shader already samples elevation correctly).
- Touching `BlocksLOS`, cover, or `TerrainCosts` (elevation is visual-only,
  per CONTEXT.md).

## Pointers

- `server/pkg/tilemap/clash_maps.go:378` — `LoadClashMap` (wire point).
- `server/pkg/tilemap/clash_maps.go:316` — `ClashHills` (216 hill tiles, the
  map most affected).
- `server/pkg/tilemap/generate.go:108-122` — procedural elevation assignment
  (reference for intent; not modified).
- `server/pkg/tilemap/tilemap.go:7,83` — `Tile.Elevation` field; `SetTerrain`
  does not reset it (safe to set post-authoring).
- `server/pkg/game/session.go:1989` — wire encoding (`data[i*2+1] = Elevation`).
- commit `850a5a8` — the elevation-aware hill shader that consumes this data.
