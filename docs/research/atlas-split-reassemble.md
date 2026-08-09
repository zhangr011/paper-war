# Splitting + Reassembling AI-Generated Art into the Unit Atlas — Research Note

**Date:** 2026-08-09
**Trigger:** User ask "I have an AI generate image, how to split it and reassemble in current project". This note covers why an AI image can't drop into the atlas directly, the concrete split/repack techniques per tool (Canvas, ImageMagick, Pillow, sprite-sheet packers), and an opinionated recommendation tuned to this codebase.

---

## TL;DR

An AI-generated image (Midjourney / SD / Gemini / etc.) **cannot** be dropped straight into `client/assets/sprites/unit_atlas.png` because the atlas is a fixed **1024×576** grid of **32×32** cells laid out in a non-obvious order — `unitType`-major, then `state`, then `dir`, then `frame` (see `client/src/unit_atlas.js:110-123`). An AI naturally produces a contact sheet, a set of portraits, or a 4-direction turnaround — none of which match that interleaved slot order. You have to **split** the source into cells and **repack** them into the exact `atlasCell()` layout.

**Recommendation for this project (ranked):**

1. **Best fit — a small `client/editor/atlas_packer.html` tool.** Vanilla JS, no build step, no new deps. Matches the existing `client/editor/*.html` editor pattern, reuses `atlasCell()`, `ATLAS_CELL`, `ATLAS_COLS`, `generateUnitAtlas()`, and the `canvas.toBlob()` download pattern already in `downloadUnitAtlasTemplate()` (`unit_atlas.js:814-829`). Loads the AI source, lets you snap source regions onto the grid-correct target canvas, exports `unit_atlas.png`.
2. **Offline scripted reassembly (Python + Pillow).** Best when the AI source is a clean, regular grid and you want a reproducible, checked-in pipeline. `Image.crop()` the source cells, `Image.paste()` into a 1024×576 canvas at `atlasCell()` coordinates.
3. **ImageMagick one-liner** — only viable when the source is *already* a regular grid AND you're willing to write the cell-permutation as a script. Good for a one-off; brittle as a repeatable pipeline.
4. **Sprite-sheet packers (TexturePacker, ShoeBox, Aseprite)** — **wrong direction.** They *pack* (remove dead space, irregular layout). The atlas needs a *fixed grid placement*. Skip them unless you export with a fixed-grid template.

**Cheapest path if it fits your source:** if the AI image is already one sprite per unit on a regular grid, a single ImageMagick/Pillow rearrange is a 20-line script. If the source is loose files or an irregular contact sheet, build the packer tool.

---

## 1. The constraint — the exact atlas contract

From `client/assets/sprites/README.md` and `client/src/unit_atlas.js`:

- **Dimensions:** exactly **1024×576** px. Any other size is rejected at load and the procedural atlas is kept (`unit_atlas.js:793-798`).
- **Cell size:** **32×32** px (`ATLAS_CELL = 32`, `unit_atlas.js:45`).
- **Grid:** **32 columns** (`ATLAS_COLS = 32`, `unit_atlas.js:46`), **18 rows** (`ATLAS_ROWS`, `unit_atlas.js:80`) → 576 cells total, 560 used.
- **Slot order** — the single source of truth is `atlasCell()` (`unit_atlas.js:110-123`):

  ```
  spriteSlot = unitType * (STATES * DIRECTIONS) + state * DIRECTIONS + dir
  linearCell = spriteSlot * MAX_FRAMES_PER_SPRITE + frame
  x          = (linearCell % ATLAS_COLS) * ATLAS_CELL
  y          = floor(linearCell / ATLAS_COLS) * ATLAS_CELL
  ```

  with constants `STATES=5`, `DIRECTIONS=4`, `MAX_FRAMES_PER_SPRITE=4` (`unit_atlas.js:54, 62, 47`). So the order is **unitType-major → state → dir → frame**, and each `(unitType,state,dir)` sprite *reserves 4 contiguous cells* even if the state uses fewer frames.
- **Frame counts per state** (`FRAMES_PER_STATE`, `unit_atlas.js:91`): idle=2, idle2=2, move=4, attack=3, die=4. Unused reserved cells (e.g. attack's 4th) stay transparent.
- **7 unit types** (0=LI, 1=HI, 2=Sniper, 3=AAI, 4=MG, 5=MA, 6=MM) — `unit_atlas.js:41`.
- **Transparency:** empty cells must be alpha 0; the shader multiplies texel × per-instance tint, so paint in neutral white/grey or full color (README §Transparency).
- **Filtering:** `NEAREST` (`gl.js:1030-1031`) — art authored at 32px cells (or a clean integer multiple) survives crisp.

**Concrete consequence of the slot math:** unitType 0 occupies `linearCell` 0..79 (rows 0–2), unitType 1 occupies 80..159 (rows 2–4), and so on. The 4 frames of one sprite are always 4 adjacent cells in the same row (because `MAX_FRAMES_PER_SPRITE=4 < ATLAS_COLS=32`). An artist/AI's natural layout ("all 4 directions of unit 0 together", or "7 units in a row") does **not** match this.

---

## 2. Why an AI image doesn't fit directly

An AI generator typically outputs one of:

| Source shape | What you get | Why it mismatches |
|---|---|---|
| **Contact sheet** | One image tiled into a grid of sprites | The grid's column count, cell size, and especially the *slot order* won't match `atlasCell()`. Even a "clean" 8×8 grid is in the wrong order. |
| **7 unit portraits** | 7 loose files, one per unitType | Missing 5 states × 4 dirs × N frames per unit. Needs duplication (idle fills idle+idle2, mirroring for W, back-variant for N) — see §4.3. |
| **Turnaround sheet** | One unit, 4 directions, maybe a few states | Right idea but still needs per-frame placement into `atlasCell()` slots; usually only covers one unit. |
| **Single hero image** | One illustration | Useless as a direct drop-in; must be segmented into cells first. |

In every case you must (a) **split** the source into 32×32 (or integer-multiple) cells and (b) **reassemble** them into the 1024×576 grid at the exact `atlasCell()` coordinates. There is no shortcut unless you can prompt the AI to emit the grid in the exact slot order — which it cannot reliably do for 560 labelled cells.

---

## 3. Splitting + repacking techniques (per tool, with real APIs)

### 3.1 Canvas / JS in-browser (most aligned with this codebase)

Primary source: [MDN — `CanvasRenderingContext2D.drawImage()`](https://developer.mozilla.org/en-US/docs/Web/API/CanvasRenderingContext2D/drawImage).

The load-bearing API is the **9-argument** `drawImage`, which copies a sub-rectangle of a source image into a rectangle of the destination canvas:

```js
ctx.drawImage(image, sx, sy, sWidth, sHeight, dx, dy, dWidth, dHeight)
```

- `image`: an `HTMLImageElement` (your loaded AI source), `HTMLCanvasElement`, `ImageBitmap`, etc.
- `sx, sy, sWidth, sHeight`: the **source rectangle** (the cell you want to lift out of the AI image).
- `dx, dy, dWidth, dHeight`: the **destination rectangle** on the atlas canvas (always 32×32 here, at `atlasCell()` coordinates).

The reassembly loop is literally:

```js
import { atlasCell, ATLAS_W, ATLAS_H, ATLAS_CELL,
         FRAMES_PER_STATE, STATES, DIRECTIONS } from '../src/unit_atlas.js';

const atlas = document.createElement('canvas');
atlas.width = ATLAS_W; atlas.height = ATLAS_H;
const ctx = atlas.getContext('2d');
ctx.imageSmoothingEnabled = false;                 // keep pixel-art crisp (unit_atlas.js:746)
ctx.clearRect(0, 0, ATLAS_W, ATLAS_H);

for (let t = 0; t < 7; t++)
  for (let s = 0; s < STATES; s++)
    for (let d = 0; d < DIRECTIONS; d++) {
      const fc = FRAMES_PER_STATE[s];
      for (let f = 0; f < fc; f++) {
        const dst = atlasCell(t, s, d, f);          // {x, y, 32, 32}
        const src = userPickedSourceRect(t, s, d, f); // from the UI
        ctx.drawImage(aiImage, src.x, src.y, src.w, src.h,
                      dst.x, dst.y, ATLAS_CELL, ATLAS_CELL);
      }
    }
```

Export uses the exact pattern already in `downloadUnitAtlasTemplate()` (`unit_atlas.js:814-829`): `canvas.toBlob(cb, 'image/png')` → object URL → anchor click. See [MDN — `HTMLCanvasElement.toBlob()`](https://developer.mozilla.org/en-US/docs/Web/API/HTMLCanvasElement/toBlob).

This is the right tool because the project is already vanilla-JS-no-build, already has an `editor/` directory of HTML+JS tools (`animation.html`, `units.html`, `map.html`), and the atlas math is exported and reusable.

### 3.2 ImageMagick (CLI batch)

Primary sources: [ImageMagick — Command-line Options (`-crop`)](https://imagemagick.org/command-line-options/#crop), [Cutting and Bordering examples](https://usage.imagemagick.org/crop/), [Montage — Arrays of Images](https://usage.imagemagick.org/montage/).

**Split** a source into a grid of 32×32 tiles (writes `tile-0.png` … `tile-N.png`):

```sh
magick source.png -crop 32x32 +repage  +adjoin  tile-%d.png
```

- `-crop 32x32` — geometry `WxH+X+Y`; without offsets it tiles the whole image into 32×32 cells.
- `+repage` — reset the virtual canvas offsets so each tile is a standalone image.
- `+adjoin` — write each tile to its own file (`%d` sequence). Without it, ImageMagick writes one multi-frame file.

**Reassemble** named tiles into the 1024×576 grid. There is no built-in "place at cell N" flag — `montage` *packs*, it does not place at absolute coordinates. The honest path is a tiny shell loop that composes each tile onto the canvas at the computed `atlasCell()` (x,y):

```sh
magick -size 1024x576 xc:none \
  \( tile-<n0>.png -geometry +<x0>+<y0> \) -composite \
  \( tile-<n1>.png -geometry +<x1>+<y1> \) -composite \
  ... unit_atlas.png
```

where each `+x+y` is `atlasCell(t,s,d,f).x/y`. You must generate that `+x+y` list from the `atlasCell()` math — ImageMagick can't infer the slot order. So this is really "a script that emits an ImageMagick command", not a one-liner.

**Verdict:** best when the AI source is a clean regular grid and you're doing a one-off. For a repeatable pipeline you'll want the loop in Python (§3.4) or the in-browser tool (§3.1).

### 3.3 Sprite-sheet packers (TexturePacker, ShoeBox, Aseprite / LibreSprite)

These tools' core job is **packing**: taking irregularly-sized sprites and finding a space-efficient layout (usually with a texture-size budget). That is the *opposite* of what the atlas contract needs — a **fixed** 1024×576 grid with a **fixed** slot order. TexturePacker does support a "grid" / "legacy" mode that keeps regular cells, but you still can't specify the `atlasCell()` permutation declaratively; you'd lay sprites out by hand and export. Aseprite's sprite-sheet export similarly assumes a simple row-major order.

**Verdict:** skip these for the atlas *reassembly*. They remain useful for **authoring** the individual unit cells (Aseprite for pixel-art frames), then feed those cells into one of the other techniques.

### 3.4 Python / Pillow (scripted reassembly)

Primary source: [Pillow — Image module](https://pillow.readthedocs.io/en/stable/reference/Image.html).

- `Image.crop(box)` — `box` is a 4-tuple `(left, upper, right, lower)`; returns a new `Image`.
- `Image.paste(im, box=None, mask=None)` — pastes `im` into the current image at `box` (a 4-tuple or a 2-tuple `(x, y)`).
- `Image.new('RGBA', (w, h), (0,0,0,0))` — transparent canvas.

A self-contained repacker (~25 lines):

```python
from PIL import Image

ATLAS_CELL, ATLAS_COLS, STATES, DIRECTIONS, MAX_FRAMES = 32, 32, 5, 4, 4
FRAMES_PER_STATE = [2, 2, 4, 3, 4]
ATLAS_W, ATLAS_H = 1024, 576

def atlas_cell(t, s, d, f):
    slot = t * (STATES * DIRECTIONS) + s * DIRECTIONS + d
    linear = slot * MAX_FRAMES + f
    return (linear % ATLAS_COLS) * ATLAS_CELL, (linear // ATLAS_COLS) * ATLAS_CELL

# source_map[(t,s,d,f)] = (sx, sy) top-left of the 32x32 region in the AI source
src = Image.open('ai_source.png').convert('RGBA')
atlas = Image.new('RGBA', (ATLAS_W, ATLAS_H), (0, 0, 0, 0))
for t in range(7):
    for s in range(STATES):
        for d in range(DIRECTIONS):
            for f in range(FRAMES_PER_STATE[s]):
                sx, sy = source_map[(t, s, d, f)]
                cell = src.crop((sx, sy, sx + ATLAS_CELL, sy + ATLAS_CELL))
                dx, dy = atlas_cell(t, s, d, f)
                atlas.paste(cell, (dx, dy))
atlas.save('unit_atlas.png')
```

The only project-specific work is filling `source_map` — the `(t,s,d,f) → source-rect` table. If the AI source is itself a regular grid, that table is arithmetic; if it's irregular, you build it by hand once.

---

## 4. Recommended approach for paper-war

### 4.1 Primary: a `client/editor/atlas_packer.html` tool

Matches the existing editor pattern (`animation.html`, `units.html`, `map.html` — all vanilla JS, no build, served as static files). Concrete shape:

**Files**
- `client/editor/atlas_packer.html` — layout: left = AI source `<img>` on a `<canvas>`, right = 1024×576 target `<canvas>`; bottom = unit/state/dir/frame selector + "Export PNG" button.
- `client/editor/atlas_packer.js` — imports from `../src/unit_atlas.js`: `atlasCell`, `ATLAS_CELL`, `ATLAS_COLS`, `ATLAS_W`, `ATLAS_H`, `FRAMES_PER_STATE`, `STATES`, `DIRECTIONS`, `generateUnitAtlas`, `drawCell`.

**Function flow**
1. On load, call `generateUnitAtlas()` and draw it as a **faint background guide layer** on the target canvas so the user can see which slot is which (the procedural sprite labels each slot visually). This is the "template overlay" trick — it turns the abstract `atlasCell` grid into a visual map.
2. User loads their AI source via `<input type="file">` → `URL.createObjectURL()` → `new Image()`.
3. User picks the current `(unitType, state, dir, frame)` from a `<select>` set, then **drag-selects a rectangle on the source canvas** (mouse down/move/up → `(sx, sy, sw, sh)`). Snap to 32-multiples if the source is already grid-aligned.
4. On release, `targetCtx.drawImage(srcImg, sx, sy, sw, sh, dst.x, dst.y, 32, 32)` where `dst = atlasCell(unitType, state, dir, frame)`. The change is immediately visible against the guide layer.
5. Provide a **"fill derived facings"** shortcut: once the user maps `(t, state, DIR_S, frame)`, auto-populate `DIR_W` by `drawImage` with a horizontal flip (mirror the source sub-rect), and leave `DIR_N` for the user to supply a back variant (or copy S as a placeholder). This mirrors what the procedural painters already do (`unit_atlas.js:210-214`).
6. **Export** reuses `downloadUnitAtlasTemplate()`'s pattern (`unit_atlas.js:816-828`): `targetCanvas.toBlob(cb, 'image/png')` → object URL → `<a download="unit_atlas.png">` → click → revoke.

**Why this wins**
- Zero new deps; zero build step; runs by opening the HTML (like the other editors).
- Reuses the exact `atlasCell()` math — the grid placement is provably correct by construction, no duplicated constants.
- The guide layer removes the "which slot is which?" problem entirely.
- Exported PNG drops straight into `client/assets/sprites/unit_atlas.png` and loads via the existing `loadUnitAtlasImage()` (`unit_atlas.js:783-805`) — no renderer or shader changes.

### 4.2 Source→target coordinate mapping (the load-bearing detail)

For every target slot the mapping is:

```
target_origin = atlasCell(unitType, state, dir, frame)   // {x, y, 32, 32}  (unit_atlas.js:110)
source_rect   = user selection on AI image               // (sx, sy, sw, sh)
ctx.drawImage(aiImage, sx, sy, sw, sh,
             target_origin.x, target_origin.y, 32, 32)
```

If the AI source cells are already 32×32, `sw == sh == 32`; if they're larger (e.g. 64×64 for detail), `drawImage` downscales with `imageSmoothingEnabled=false` for crisp pixel-art. The destination is always 32×32.

### 4.3 When the AI source has fewer cells than the atlas needs

Realistically the AI produces far fewer than 140 sprites. Reasonable reductions, in order of visual cost:
- **One sprite per (unit, dir)**, reused across all states — cheapest, loses animation. Acceptable for a first pass; the renderer will still advance frames but they'll look identical.
- **One sprite per (unit, state, dir)**, frame 0 reused for all frames — keeps state identity (idle vs attack vs die silhouettes differ), loses frame animation.
- **Full per-frame** — the ideal; only worth it once art direction is locked.

The packer tool should make "copy this slot's source to all frames of this state" a one-click action, since that's the common case during iteration.

### 4.4 Simpler alternatives, ranked by effort vs robustness

1. **Prompt the AI to emit a per-unit turnaround sheet (1 unit × 4 dirs × a few states), repeat for all 7 units, then assemble with the packer tool.** Lowest effort for a real result. The packer handles the permutation; you just feed it 7 small sheets.
2. **Python/Pillow script (§3.4) checked into `docs/research/` or a `tools/` dir.** Best if you'll regenerate often (e.g. the AI prompt is part of the pipeline). The `source_map` table is the only hand-maintenance.
3. **ImageMagick one-off (§3.2).** Fine for a single import; don't build a pipeline on it.
4. **Hand-place in Aseprite/Photoshop using `downloadUnitAtlasTemplate()` as a guide.** Works, but non-reproducible and doesn't scale to regen.

---

## 5. Sources

| Source | URL | Used for |
|---|---|---|
| MDN `CanvasRenderingContext2D.drawImage` | https://developer.mozilla.org/en-US/docs/Web/API/CanvasRenderingContext2D/drawImage | 9-arg `drawImage` signature: source-rect → dest-rect copy |
| MDN `HTMLCanvasElement.toBlob` | https://developer.mozilla.org/en-US/docs/Web/API/HTMLCanvasElement/toBlob | PNG export pattern (matches `downloadUnitAtlasTemplate`) |
| ImageMagick Command-line Options (`-crop`) | https://imagemagick.org/command-line-options/#crop | `WxH+X+Y` geometry, grid tiling |
| ImageMagick Cutting and Bordering examples | https://usage.imagemagick.org/crop/ | `+repage`, tile-by-tile crop usage |
| ImageMagick Montage — Arrays of Images | https://usage.imagemagick.org/montage/ | `montage` packs rather than places — why it's the wrong tool |
| Pillow Image module | https://pillow.readthedocs.io/en/stable/reference/Image.html | `Image.crop((l,u,r,l))`, `Image.paste(im, box, mask)`, `Image.new` |
| `client/assets/sprites/README.md` | `client/assets/sprites/README.md` | Atlas contract (dims, cell size, transparency, load path) |
| `client/src/unit_atlas.js` | `client/src/unit_atlas.js:45-96, 110-123, 738-766, 783-829` | `atlasCell()` math, constants, `generateUnitAtlas`, `loadUnitAtlasImage`, `downloadUnitAtlasTemplate` |
| `client/src/gl.js` | `client/src/gl.js:1012-1037` | `setUnitTexture()` accepts `HTMLImageElement` (NEAREST filtering) |
| `docs/research/art-asset-pipeline.md` | prior research note | Established the atlas format + override seam |
