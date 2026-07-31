# Plan — Terrain Polish v2 (Shader-Side Coastline via Neighbor Texture)

Implements ADR-0026. Per-pixel tile-edge feathering in the ADR-0015 terrain
shader, applied first to water coastlines. Pure render — no terrain data,
movement, cover, LOS, or balance change.

Driven by a grill-with-docs session (2026-07-31). Every branch was resolved
against the existing domain model; see "Decision rationale" at the end.

## Scope

1. **Tile-type texture** — upload the terrain grid once per match as a
   single-channel integer texture; fragment shader reads neighbors.
2. **Tile-coordinate attribute** — give each terrain quad its integer `(tx, ty)`
   so the shader can compute neighbor texel positions.
3. **Curated blend table** — data-driven blendable pairs; ship Deep↔land only.
4. **Coastline blend** — Deep tiles fade to teal (Shallow color) at edges
   facing land.
5. **Docs** — ADR-0026 (already written); CONTEXT.md untouched.

## Out of scope (deliberately deferred)

- Emitting `TerrainShallow` terrain (Light-wade / Heavy-block mechanic) — a
  balance change, not polish. Coastline borrows Shallow's *color* only.
- Feathering non-water seams (forest edge, hill relief) — mechanism supports
  it, table not yet populated. One-entry extension later.
- CPU overlay-quads coastline — rejected (ADR-0026).
- Any server / wire / terrain-data change.

## Edits

### 1. Terrain-grid texture — `client/src/gl.js` + `client/src/main.js`

- In the `Renderer`, add a terrain-type texture (single-channel; `R8UI` or a
  packed `RGBA8` if integer textures are awkward on the target hardware —
  verify WebGL2 support; WebGL2 guarantees integer textures, so prefer
  `R8UI`). One texel per tile, dimensions = map width × height.
- Upload in `setMapTerrain` (`main.js:374`) — terrain is static for the match,
  so upload once after de-interleaving `terrainData`. Rebuild only if a new map
  arrives.
- Bind to a new sampler uniform in the terrain sprite program (`SPRITE_FS`).
- Keep the existing `whiteTexture` placeholder path intact for the
  non-terrain batches.

### 2. Tile-coordinate attribute — `client/src/gl.js` (`SpriteBatch` / `pushTexturedQuad`)

- Extend the terrain textured-quad path so each tile carries its integer
  tile-coordinate `(tx, ty)`. The fragment shader needs this to compute
  `texelFetch(terrainTex, ivec2(tx + dx, ty + dy), 0)` for neighbor lookups.
  Implement as either a new per-quad attribute or by deriving from the quad's
  world position + a uniform tile size — pick whichever fits `SpriteBatch`
  with the least churn; the call site is `drawTerrain` (`gl.js:729`) and
  `buildTerrainTiles` (`main.js:1385`).
- Non-terrain sprite batches (objects, fog, effects) are unaffected.

### 3. Curated blend table — `client/src/gl.js` (or data uploaded as a small uniform/texture)

- A blendable-pairs definition. Ship with water↔land pairs enabled:
  - `Deep(3) ↔ Plain(0)`, `Deep ↔ Forest(4)`, `Deep ↔ Hill(5)`,
    `Deep ↔ Brush(17)`.
  - Each entry's target = teal (Shallow color, id 2).
- Encode however is cheapest for the shader to test: e.g. a function
  `blendTarget(neighborType)` that returns the teal tint when `neighborType`
  is a blendable land type and `tileType == Deep`, else a sentinel for "no
  blend." Keep it data-shaped so adding pairs doesn't require GLSL edits.
- Every other seam stays a hard edge (no blend).

### 4. Coastline blend — `client/src/gl.js` (`SPRITE_FS`)

In the fragment shader, **for Deep tiles only** (early-out otherwise — avoids
neighbor fetches on the common case), for each of the 4 edges:

- Sample the neighbor tile type via `texelFetch`.
- If the (Deep, neighbor) pair is blendable, compute per-pixel
  distance-to-that-edge and fade the fragment color from deep-blue (tile
  center) toward the teal target (edge) by that distance.
- Land tiles' fragments are unchanged — the feather lives entirely on the
  water tile.

Tuning knob: feather width as a fraction of the tile (start ~0.4 of the
half-tile so the teal band is visible but the tile still reads as water).
Revisit at play zoom.

### 5. Docs

- ADR-0026 (`docs/adr/0026-tile-edge-blending.md`) — written.
- CONTEXT.md — no change. "Tile-type texture," "blend table," and "coastline"
  are rendering/implementation detail, not domain language; `Shallow`/`Deep`/
  `Terrain Type` already cover the domain side.

## Guards (required)

- **Zero gameplay change.** `terrainData`, `TerrainType`, `MovementProfile.
  TerrainCosts`, cover, and `BlockLOS` must be untouched. Verify by confirming
  all edits live in `client/src/gl.js` + `client/src/main.js` render paths —
  no `server/` file is modified.
- **Non-Deep tiles must early-out** before any neighbor texture fetch, so the
  common case pays nothing.
- **Coastline must not appear at Deep↔Bridge** seams (bridges span water and
  should read as a structure on top, not a shore). Either exclude Bridge from
  the land-side set, or confirm the bridge object batch draws over the
  feathered edge cleanly.
- **Degrade gracefully** if the terrain-type texture is absent (e.g. fallback
  / editor path) — shader should render flat tiles as today, no coastline, no
  errors.

## Verification

- **Visual (primary):** run a match with a river + lake. Confirm water meets
  land through a teal band instead of a hard seam; confirm forest-shore,
  plain-shore, and hill-shore all show the same teal band (per Q6 decision);
  confirm bridges still read as structures, not shores. Use the crash-restart
  e2e spec (`tests/e2e/zz-crash-restart.spec.js`, 20v20 clash) as the
  observation instrument, or `/browse` against a local match.
- **Non-water seams unchanged:** walls, roads, rock outcrops, forest↔plain
  edges stay hard (only Deep↔land feathers today).
- **Performance:** the terrain pass is the hot path every frame. Eyeball frame
  rate in a zoomed-out full-map view; the early-out must keep non-Deep cost
  flat. If a benchmark exists (`/benchmark`), run before/after.
- **Editor / clash maps:** confirm they inherit the coastline (shared render
  path) and that the editor's own `TERRAIN_COLORS` path
  (`client/editor/map_editor.js`) is unaffected or updated consistently.
- No playtest-harness run required (no balance/cover/LOS change).

## Decision rationale

Resolved in the grill session against the existing domain model:

1. **Coastline is render-only, not the Shallow mechanic.** `TerrainShallow`
   (id 2) is gameplay-touching: Light wades at cost 2, Heavy is blocked
   (`profiles.go:13,36`, `TestHeavyProfileCannotCrossShallow`). Emitting it
   would be a movement-balance change, not polish. Coastline therefore borrows
   Shallow's *color* only, via the shader — no terrain data changes. Rejected:
   a brand-new visual-only "Beach" terrain type (widens `TerrainCosts`,
   reinvents a mechanic to dodge a problem the render layer solves).

2. **Shader-side over CPU overlay.** Overlay quads would soften the water seam
   without touching the shader, but don't generalize — forest/hill edges would
   each need their own pass. Neighbor-texture blending solves all seams with
   one mechanism (ADR-0026).

3. **Tile-type texture over per-tile neighbor attributes.** One upload per
   match, zero per-frame cost, decoupled from the batch. Per-tile attributes
   bloat the per-frame buffer and recompute neighbors every frame for no gain.

4. **General mechanism, curated table, water-first.** Uniform "blend every
   seam" was rejected as muddy (walls/roads/rock want crisp edges). A curated
   blend table ships water-only now and is the one-entry extension point for
   future seams — the whole reason shader-side was worth it.

5. **Fixed teal target, not neighbor color.** Shallow water is teal because of
   depth, not the shore's material; a uniform teal band reads as a true
   coastline. The target is table-data, so future land-land blends can specify
   "neighbor color" without shader edits.

## Pointers

- `client/src/gl.js:729` — `drawTerrain` (textured-quad terrain path).
- `client/src/gl.js:427` — `InstancedBatch` (reference for attribute plumbing;
  terrain uses `SpriteBatch`, not this).
- `client/src/main.js:374` — `setMapTerrain` (texture upload site; terrain
  static per match).
- `client/src/main.js:1385` — `buildTerrainTiles` (per-tile descriptor build).
- `client/src/main.js:190,193-194` — `TERRAIN_COLORS`, Deep (id 3) + Shallow
  (id 2, the teal target).
- `client/editor/map_editor.js:43` — editor `TERRAIN_COLORS` (consistency
  check).
- `server/pkg/component/movement.go:8` — `TerrainShallow = 2` (mechanic,
  untouched).
- `server/pkg/component/profiles.go:13,36` — Shallow movement costs
  (untouched; confirms coastline must not emit the type).
- ADR-0015 (textured-terrain-shader — the base extended), ADR-0024
  (environment = terrain; Shallow color source).
