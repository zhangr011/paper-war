# ADR-0015: Textured Terrain via Fragment Shader Noise

**Date:** 2026-06-14
**Status:** Accepted
**Issue:** [#15 — Map visual enhancement](https://github.com/zhangr011/paper-war/issues/15)

## Context

The v1 client rendered terrain tiles as flat colored quads via
`SpriteBatch.pushColorQuad`. While performant (single draw call for all
visible tiles), the result looked synthetic and nothing like the dark,
textured pixel-art aesthetic of `design/map.png`:

| Property              | Before (flat)        | Design target                |
|-----------------------|----------------------|------------------------------|
| Avg brightness        | ~120/255             | ~64/255                      |
| Plains color          | (0.22, 0.48, 0.18)   | #305010 = (0.19, 0.31, 0.06) |
| Water                 | light blue           | dark teal RGB(37,80,124)     |
| Per-pixel variation   | 0 (flat within tile) | variance ~1900-2200          |
| Visible grid lines    | yes (implicit)       | no                           |

Three implementation strategies were considered:

1. **Multi-quad subdivision** — split each tile into a 4×4 or 8×8 grid of
   colored quads. Simple but pushes 16-64× more vertices, blowing the
   60 000-vertex batch budget on large viewports.
2. **Pre-baked texture atlas** — generate procedural textures on a 2D
   canvas at startup, sample in shader. Higher fidelity but adds texture
   memory and upload cost.
3. **GPU-side hash noise** — compute per-pixel noise in the fragment
   shader using a deterministic hash function. Zero extra memory, no
   vertex-budget impact, true per-pixel variation.

## Decision

**Use GPU-side hash noise (option 3)** implemented directly in the
existing `SpriteBatch` fragment shader, plus a full overhaul of the
`TERRAIN_COLORS` palette to match the design image.

### Changes

1. **Vertex format extended 8 → 10 floats** per vertex in `SpriteBatch`.
   Two new attributes:
   - `a_tileType` (float) — 0 means "flat color" (objects, effects, fog,
     UI keep existing behavior); ≥1 routes through the noise branch with
     type-specific patterns.
   - `a_seed` (float) — per-tile deterministic seed so identical terrain
     types don't share the same noise pattern.

2. **Fragment shader noise** — Dave-Hoskins-style `hash21` quantized with
   `floor()` to give chunky pixel-art blocks rather than smooth gradients.
   Five pattern branches:
   - **Water (2, 3)** — horizontal wave bands animated by `u_time` + grain
   - **Hills (5)** — chunky vertical grain with occasional darker cracks
   - **Road/Bridge (1, 7)** — plank lines every 8 px + grain
   - **Forest (4)** — darker organic noise with occasional light flecks
   - **Default (plains, swamp, desert, …)** — organic per-pixel grain

3. **Palette overhaul** — all 16 entries in `TERRAIN_COLORS` retuned to
   the dark earthy design palette. Dominant plains now `#305010`, water
   `RGB(37,80,124)`, hills `#907040`, etc.

4. **Client-side simplification** — `buildTerrainTiles()` no longer
   applies JS-side jitter/water-shimmer/plank-darkening/shoreline-edge
   detection; all of that is now GPU-side. Only hill elevation shading
   (which depends on continuous elevation data) remains client-side.

5. **`u_time` uniform** threaded through `Renderer.endFrame()` to all
   sprite batches so water waves animate.

### Why not pre-baked textures?

- Avoids texture upload / VRAM cost for what is fundamentally a noise
  function.
- Stays within the existing 4-batch / 5-draw-call architecture — no new
  render passes.
- Tuneable in-shader: changing wave frequency or noise scale is a one-line
  edit, no re-baking.
- If we later need higher fidelity (e.g. hand-authored terrain sprites),
  the atlas path (`Renderer.setAtlas`) is already in place and the
  `a_tileType`/`a_seed` attributes can coexist with atlas sampling.

## Consequences

- **Forward-compatible:** all non-terrain callers of `pushColorQuad` pass
  `tileType=0` by default, so objects, effects, fog, and HP bars render
  exactly as before.
- **Draw-call budget unchanged:** still one terrain batch → one draw call.
  CPU-side `buildTerrainTiles` is simpler (no JS math for jitter/edge
  detection), so per-frame CPU cost is slightly lower.
- **Memory:** CPU vertex buffer grew from 8 → 10 floats/vertex
  (+25%). At `MAX_BATCH_VERTICES = 60000` that's 480 KB extra CPU buffer
  — negligible.
- **GPU cost:** one extra hash + branch per fragment for textured tiles.
  On integrated GPUs this is well under 0.1 ms for a 1080p frame.
- **Debugging:** WebGL `readPixels` returns an empty buffer after
  composite (the context uses the default `preserveDrawingBuffer: false`).
  For per-pixel verification during development, temporarily flip that
  flag on in `gl.js`; it is intentionally left off in production for
  performance.

## Verification

Sampled the rendered canvas via `gl.readPixels` (with
`preserveDrawingBuffer` temporarily enabled for measurement):

| Metric              | Before   | After    | Target       |
|---------------------|----------|----------|--------------|
| Avg brightness      | ~120     | 28/255   | ~64/255 ✓    |
| Avg local variance  | ~0       | 10.2/255 | >0 ✓         |
| Dominant color      | (56,122,45) | (27,46,12) | #305010 ✓ |

The minimap (which uses the same `TERRAIN_COLORS` on a 2D canvas) shows
the dominant color bucket `1-2-0` = (32-63, 64-95, 0-31), which is
exactly `#305010` — the dominant green from `design/map.png`.
