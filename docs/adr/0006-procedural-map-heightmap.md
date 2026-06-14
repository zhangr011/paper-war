# 0006 — Procedural Map Generation: Heightmap Pipeline

Date: 2026-06-13

Supersedes: 0005 (road-connected spawns, minimum bridges, walls on roads)

## Context

The original map generator (`generate.go`) placed terrain via random cluster blobs, a single horizontal river band, straight roads, and scattered walls/strongholds. It was designed around a horizontal river with bridges as the central strategic feature. The resulting maps lacked natural terrain structure and visual coherence.

Issue #6 introduced a target reference image (`design/map.png`) showing a naturalistic temperate wilderness with elongated hill ridges, dense forest (~75%), a river, and a lake — no roads, no walls, no artificial structure.

A 75-question grilling session resolved every design branch before implementation.

## Decision

Replace `GenerateMap()` with a heightmap-driven procedural pipeline. The pipeline produces all terrain from two Perlin noise layers plus a river trace algorithm. No hand-placed structures except bridges.

### Pipeline stages

1. **Heightmap** — Perlin noise (1 octave, freq 0.05, 2.5x X-stretch) produces elongated ridges directly. No erosion (unnecessary at 48x96 tile resolution).
2. **Hills** — height threshold → `TerrainHill` (~12%). Ridges emerge from noise shape.
3. **River** — downhill trace from a random high point (top 10% elevation) to the lowest point. Variable width (1 tile upstream → 3 downstream). All `TerrainDeep`.
4. **Lake** — sea level sweep fills the lowest depression. All `TerrainDeep`. Combined water target ~2%.
5. **Forest** — second Perlin layer (offset coordinates, freq 0.12, 1 octave). Adaptive threshold at 25th percentile of eligible tiles → exactly ~75% `TerrainForest`.
6. **Composition** — Hill > Deep > Forest > Plain. Forest never on hill or water.
7. **Passes & strongholds** — detect non-hill tiles flanked by hills (relaxed thresholds). Place 1-3 `TerrainStronghold1` at passes. Count: `min(w*h/2000, 3)`.
8. **Bridges** — 1-2 at narrowest river sections. `TerrainBridge`, Health=200, destructible.
9. **Spawns** — top-center, bottom-center (portrait). Fallback: search edge then 12 rows inward. Clear 6x6 to `TerrainPlain`.
10. **Elevation** — store normalized heightmap (0-100) on `Tile.Elevation` for future mechanics.
11. **Objective** — terrain-driven: Capture (stronghold at pass) or Elimination. 15% Survival override (Duration=3000 ticks = 10 min). AI behavior for Survival is separate (see issue TBD).
12. **Validation** — BFS connectivity for Light and Heavy profiles between spawns. Panic on failure.

### Key differences from ADR-0005

| Aspect | ADR-0005 | ADR-0006 |
|--------|----------|----------|
| Strategic structure | Roads + bridges + walls | Natural ridges + river + lake |
| Bridge guarantee | Minimum 3 | 1-2 at narrowest river points |
| Roads | Guaranteed road network | None |
| Walls | Random chokepoints | None (natural chokepoints) |
| Connectivity | Road-connected spawns | BFS path exists for both profiles |
| Water | Horizontal river + shallow fords | Downhill river + depression lake, all Deep |
| Terrain diversity | All 16 types used | 6 types only (Forest, Hill, Deep, Plain, Stronghold1, Bridge) |
| Symmetry | Horizontal mirror | None (PvE campaign) |
| Objective | Random 1/3 | Terrain-driven + 15% Survival |
| Destructible terrain | Bridges + walls | Bridges only (Health=200) |

### Scope

Campaign maps only. Clash maps remain hand-authored in `clash_maps.go`. A scaled-down version for clash maps is deferred to future work.

### New dependency

`github.com/aquilax/go-perlin` (MIT, pure Go, 87 importers)

### Struct changes

`GameMap` gains `Spawns [][2]int32` and `Seed int64`. Function signature unchanged: `GenerateMap(w, h int32, seed int64) *GameMap`.

### Testing

Property-based: 100 random seeds, 13 invariants (dimensions, spawns, coverage %, connectivity, objective validity, determinism). Benchmark test (not CI-gated).

## Considered and rejected

- **Erosion simulation** — overkill at 48x96. Noise stretching produces ridges directly.
- **Biome variety (Desert, Snow, Swamp)** — target is temperate forest only. No gameplay differentiation for other biomes in v1.
- **Road network** — the heightmap's natural structure (ridges, river, passes) provides strategic corridors without artificial roads.
- **Map symmetry** — PvE campaign. AI adapts to terrain. Symmetry is a competitive PvP tool.
- **Wall placement** — ridge passes and lake shorelines create natural chokepoints.
- **Multiple stronghold levels** — no gameplay difference in v1.
- **Shallow water** — all water is Deep. Bridges are the only crossing. Simplifies rules.
