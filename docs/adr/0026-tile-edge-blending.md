# Tile-Edge Blending via Neighbor Texture

**Status:** Accepted (2026-07-31)

Extends ADR-0015 (textured-terrain-shader) with **per-pixel tile-edge
feathering**: the fragment shader samples neighboring tile types from a small
terrain-grid texture and blends a tile's color toward its neighbor by
edge-distance. First application is water **coastlines** — Deep tiles fade to a
teal shallows band where they meet land, killing the hard blue-to-green seam
that is the most visible terrain artifact.

## Decision

1. **Neighbor delivery: a tile-type texture, not per-tile attributes.** The
   terrain grid is uploaded once per match as a single-channel integer texture
   (one texel per tile; ~1440 bytes for a 30×48 map). The fragment shader reads
   neighbors with `texelFetch`. Alternative considered: passing 4–8 neighbor
   types as per-instance attributes on every tile quad. Rejected — it bloats
   the per-frame batch buffer and forces the CPU to recompute neighbors every
   frame in `buildTerrainTiles`, for no generality gain. The texture is
   uploaded once (terrain is static for a match) and decouples neighbor
   lookups from the per-frame batch entirely.

2. **General mechanism, curated blend table, water-first.** The shader samples
   arbitrary neighbors; which seams actually feather is governed by an explicit
   blendable-pairs table (data the shader reads). Ships with only **Deep↔land**
   pairs enabled (`Deep↔Plain`, `Deep↔Forest`, `Deep↔Hill`, `Deep↔Brush`).
   Everything else stays a hard edge — walls, bridges, roads, and rock outcrops
   keep crisp boundaries; uniform "blend every seam" feathering was rejected as
   muddy. The table is the extension point: forest-edge or hill-relief
   feathering later is one table entry, no hot-path re-touch.

3. **Water fades to a fixed teal shallows tint, not the neighbor's color.**
   Each blendable pair carries a target color source. For water pairs the
   target is teal — the existing `TerrainShallow` palette entry (id 2,
   "transition teal," ADR-0024), currently unused by the generator. Realistic:
   shallow water reads turquoise because of depth, not the adjacent terrain, so
   a uniform teal band inside the Deep tile reads as a true coastline. Future
   land-land pairs (forest edge, etc.) may specify "neighbor color" instead;
   the target is data, not hardcoded.

4. **Blend lives on the water tile; land tiles are unchanged.** The feather is
   computed only on Deep-tile fragments at edges facing a blendable land
   neighbor, fading from deep-blue at the tile center toward teal at the edge
   by per-pixel distance-to-edge. Land tiles keep their color and shader path.
   Mechanically Deep stays Deep — this is pure render. No change to
   `TerrainType`, movement costs, cover, or LOS.

## Why

ADR-0015's terrain shader shades each tile in isolation — one `tileType` +
`seed` per quad, no neighbor awareness — so every tile boundary is a hard seam.
The Deep↔land seam is the worst case because water and grass/forest/hill sit at
opposite ends of the palette; players see it every match along every river and
lake. CPU overlay quads (a sibling approach considered in the originating
grill) would soften it without touching the shader, but they don't generalize —
forest edges and hill transitions would each need their own overlay pass.
Neighbor-texture blending solves all of them with one mechanism, which is the
reason to spend the hot-path change here rather than on overlays.

## Consequences

- **ADR-0015 hot-path extension.** Each terrain tile quad now carries its
  integer tile-coordinate `(tx, ty)` (new attribute) so the fragment shader can
  compute neighbor texel positions, plus a one-time terrain-grid texture
  upload in `setMapTerrain`. The shader gains a small neighbor-sample + blend
  block. Cost is a handful of texture fetches per Deep-tile fragment only
  (early-out for non-Deep tiles).
- **`TerrainShallow` palette entry is now referenced.** Its color (id 2) is the
  coastline target tint. `Shallow` the *mechanic* (Light wades, Heavy blocked,
  `profiles.go`) is unchanged and still never emitted by the generator — this
  ADR borrows its color, not its gameplay.
- **Curated blend table** is the single source of "which seams feather." Adding
  a seam = one entry; no shader edit required for routine additions.
- **Pure client change.** No server, no wire format, no terrain data, no
  balance. No playtest-harness run required. Clash maps and the editor inherit
  the coastline automatically (they share the render path).

## Out of scope

- Emitting `TerrainShallow` terrain (the Light-wade / Heavy-block mechanic) —
  that is a balance change, not polish; belongs in its own issue + harness run.
- Feathering non-water seams (forest edge, hill relief) — mechanism supports
  it, table does not yet; deferred to a follow-up once coastline is validated.
- CPU overlay-quads coastline — rejected (see above).

See the plan at `docs/plans/terrain-polish-v2.md`. Originating design tree
resolved in a grill-with-docs session (2026-07-31).
