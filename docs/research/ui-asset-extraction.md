# UI Asset Extraction from a Labeled Design Sheet — Research Note

**Date:** 2026-08-09
**Trigger:** User wants to extract usable individual assets out of `design/component.png` (1672×941) — an AI-generated "Paper UI Kit" *spec sheet* (Battle HUD mockup + color swatches with hex labels + typography samples + spacing grid + button states + decorative tape/pin/clip/ink-stain), all specimens sitting on a shared cream/paper background. This is NOT a clean sprite sheet; elements are labeled specimens on a textured background, not transparent tiles. The goal is to feed individual assets into the project's CSS-driven UI texture pipeline.

This note is scoped to **UI chrome extraction** and is distinct from the prior unit-sprite work in `docs/research/atlas-split-reassemble.md` (which targets the 1024×576 WebGL unit atlas).

---

## TL;DR

The design sheet has a textured cream background that is **not a single flat color**: I measured `design/component.png` and the dominant background is `#FBF9F6` (≈24% of pixels in a 16-color quantized histogram) but it bleeds through at least three warm variants (`#F0ECE8`, `#E7CFAE`, `#DEC5A2`) and the image carries **155,640 unique colors** — i.e. heavy anti-aliasing and paper grain. Threshold-based transparency keying (ImageMagick `-fuzz ... -transparent`) therefore either leaves a halo (low fuzz) or eats anti-aliased edges and cream-colored elements like tape (high fuzz). There is no threshold that works across the whole sheet.

There is also **no reliable automated element slicer** for a *flattened* labeled PNG. Connected-component labeling, OpenCV `findContours`/`MSER`, and YOLO-style detectors all need either a clean binary mask first or a trained model — and the "UI design → assets" services (Anima, Locofy, TeleportHQ, Figma dev-mode export) operate on *layered source files* (`.fig`/`.sketch`), not on flattened raster. They cannot recover layers from a PNG.

**Recommendation for this project (vanilla JS, macOS, ImageMagick available, no build step):**

1. **Primary — hand-crop + per-element transparency key.** You know the layout; crop each specimen with `magick -crop WxH+X+Y`, then key out cream with `-fuzz 15% -transparent "#FBF9F6"` and `-trim +repage`. ~2 minutes per asset, full control, no deps. Hand-mask the cream-on-cream cases (tape, pin) with a lasso.
2. **ML fallback — Rembg (`isnet-general-use`) per cropped element** for anti-aliased specimens that key poorly. `pip install rembg[cpu]` once, then `rembg i -m isnet-general-use -a el.png out.png`. Crop before running (salient-object models get confused by a whole spec sheet).
3. **Zero-install ML — remove.bg API** (1 credit/image, `size=auto`, `type=graphic`). Same crop-first caveat.
4. **Do not automate slicing or palette extraction.** The sheet is one-of-a-kind; the 6-10 crops aren't worth a CC pipeline, and the color tokens are already locked in `client/style.css:8` ("Design #28 palette — tuned via OpenCV diff").

For **9-slice frames** specifically, match the existing `wood-frame-9slice.png` convention: 64×64, frame ring width 14 px, transparent center, consumed as `border-image: url(...) 14 14 14 14 stretch` (`client/style.css:44, 184`). New frames just need ring width `W` and a matching `border-image-slice` value of `W`.

---

## 1. The problem — labeled design sheet → clean assets

`design/component.png` is a **design-spec sheet**, not a sprite sheet:

- **Shared cream background** — every specimen sits on the same paper. Confirmed by sampling: all four corners average `srgb(94–98%)` ≈ `#FBF9F6` (ImageMagick `-resize 1x1` on 50×50 corner crops).
- **Textured, not flat** — a 16-color quantized histogram shows the background split across `#FBF9F6` (dominant), `#F0ECE8`, `#E7CFAE`, `#DEC5A2`. Paper grain + multiply-blended panels.
- **Heavy anti-aliasing** — `magick ... -format "%k"` reports **155,640 unique colors** in a 1.5 MP image. Edges blend smoothly into the cream.
- **Labels live next to each element** — hex codes beside swatches, captions beside specimens. These are not part of the asset and must be excluded from crops.
- **Cream-on-cream specimens** — tape, pin, paper-clip, ink stain are deliberately close in value to the paper. They will not separate from the background by any color threshold.

The extraction job is therefore: **(a)** locate each element (manual — the layout is known), **(b)** crop it, **(c)** remove the shared background while preserving anti-aliased edges, **(d)** trim to bbox, **(e)** for frames, author as a 9-slice matching the project convention.

---

## 2. Project context — how textures are consumed today

Grounded in `client/style.css` and `client/assets/textures/`.

### 2.1 Two consumption patterns

| Pattern | CSS mechanism | Example |
|---|---|---|
| **Tiled fill** | `background: var(--tex-X) repeat center / Npx Npx, <base-color>; background-blend-mode: multiply;` | parchment, navy header, gold CTA, ink border |
| **9-slice frame** | `border: 8px solid transparent; border-image: var(--tex-wood-frame) 14 14 14 14 stretch;` | wood frame on top bar / panels |

Key references in `client/style.css`:
- Texture `url(...)` vars: lines **36–41** (`--tex-parchment`, `--tex-parchment-warm`, `--tex-wood-frame`, `--tex-navy-header`, `--tex-gold-cta`, `--tex-ink-border`).
- Tiled background usage: lines **177–178**, **225–226**, **289–290**, **473–474**, **614–615**, **702–703**, **832–833** (pattern: `var(--tex-X) repeat center / <size>, var(--paper-bg); background-blend-mode: multiply`).
- 9-slice frame usage: lines **184**, **429**, **1018**, **1200** — all `border-image: var(--tex-wood-frame) 14 14 14 14 stretch`.
- `--frame-slice: 14` token at line **44** with comment: *"9-slice border widths must match the wood-frame-9slice.png frame ring."*

### 2.2 Existing textures (all CSS-only, never loaded by WebGL)

Measured with `sips -g pixelWidth -g pixelHeight`:

| File | Size | Role |
|---|---|---|
| `parchment-tile.png` | 256×256 | base paper grain (multiplied over `--paper-bg`) |
| `parchment-warm.png` | 256×256 | warmer tan variant |
| `wood-frame-9slice.png` | 64×64 | panel frame; 14 px ring, transparent center |
| `navy-header-tile.png` | 128×32 | horizontal header strip |
| `gold-cta-tile.png` | 128×32 | CTA strip |
| `ink-border.png` | 64×64 | 1 px ink stroke |

The 9-slice convention is confirmed by sampling `wood-frame-9slice.png`: top-left 16×16 averages `srgba(151,118,70,0.99)` (opaque golden-brown frame), center 16×16 is `srgba(0,0,0,0)` (fully transparent). Frame ring ≈ 14 px, matching `--frame-slice: 14`.

### 2.3 These textures are *procedurally generated*

`scripts/gen-textures.py` (Pillow, deterministic `SEED`) regenerates all six PNGs byte-identically. So the project already has a "no hand-art" pipeline for chrome. This matters: any asset extracted from `design/component.png` should either (a) replace one of these six roles with a clearly better hand-authored version, or (b) fill a *new* role (decorative `<img>`, a one-off panel frame). It's not obvious that the design sheet's specimens are better than the procedural textures already in place — most of the value is in **new** decorative elements (tape, pin, ink stain) and any frame variant the design introduces.

---

## 3. Findings per research question

### 3.1 Background removal / transparency keying on a textured paper bg

**Threshold keying — ImageMagick `-fuzz` + `-transparent`.**
Per the [ImageMagick `-fuzz` docs](https://imagemagick.org/command-line-options/#fuzz): *"Colors within this distance are considered equal… match colors that are close to the target color in RGB space."* The distance is an RGB-cube radius, in absolute units or `%` of max intensity. [`-transparent`](https://imagemagick.org/command-line-options/#transparent) then sets matching pixels to alpha 0.

```
magick design/component.png -crop 320x120+X+Y +repage \
  -fuzz 15% -transparent "#FBF9F6" -trim +repage out.png
```

- **Failure mode (load-bearing for this sheet).** `-fuzz` is a single RGB radius. The background spreads across ≥4 cream variants whose combined RGB distance from `#FBF9F6` exceeds the per-pixel distance of many anti-aliased element edges. A fuzz small enough to spare edges (≈5%) leaves cream residue around every edge; a fuzz big enough to clear the texture (≈25%) eats the anti-alias ramp and punches holes in cream-adjacent specimens (the gold CTA, the warm parchment panels, tape). **No single fuzz value works across the whole sheet.** Per-element tuning is required, and cream-on-cream elements (tape, pin, paper-clip) key out their own subject along with the bg.
- **Halos.** Even with a good fuzz, anti-aliased edges leave a 1–2 px cream halo because the intermediate alpha pixels are partially cream. `-channel A -morphology Erode Disk:1` or `-blur 0x1` before keying mitigates but does not eliminate this.

**ML matting — the robust choice for anti-aliased specimens.**

| Tool | Input requirement | Fit for this sheet |
|---|---|---|
| **[Rembg](https://github.com/danielgatis/rembg)** (`pip install rembg[cpu]`) | Single RGB image; runs salient-object segmentation. Models: `u2net` (general), `u2netp` (lightweight), `isnet-general-use` (DIS, newer, stronger), `silueta`, `birefnet-general`. Alpha-matting mode `-a` refines edges. | **Best fit** when run on a *cropped single element*, not the whole sheet. `isnet-general-use` is the recommended model for general specimens. |
| **[remove.bg API](https://www.remove.bg/api)** | `POST https://api.remove.bg/v1.0/removebg` with `image_file`, `size=auto`, optional `type=graphic` (added 2024-06-03 for non-subject foregrounds). 1 credit/image for ≤10 MP PNG. | Same crop-first caveat. Zero local install; costs credits; needs network. Official curl sample in the [API docs](https://www.remove.bg/api). |
| **[U²-Net](https://github.com/xuebinqin/U-2-Net)** (the model under Rembg) | Salient-object detection (Pattern Recognition 2020). | Already wrapped by Rembg — no reason to run raw. |
| **[MODNet](https://github.com/ZHKKKe/MODNet)** | **Portrait** matting (AAAI 2022). Trimap-free, real-time, **humans only**. | **Wrong tool** — trained on people, not paper UI specimens. |
| **[BackgroundMattingV2](https://github.com/PeterL1n/BackgroundMattingV2)** | Requires a **captured background frame** (photo of the empty bg behind the subject) plus the subject frame. | **Not applicable** — there is no "empty background" capture of a static design sheet. |

- **Failure mode (all salient-object models).** Run on the *whole sheet*, the model sees a confusing multi-subject field and may segment only the most salient blob (the HUD mockup) or produce a fragmented mask. **Always crop to one element first.**
- **Honest expectation.** On a cleanly cropped single specimen with good fg/bg contrast, Rembg `isnet-general-use -a` produces a usable alpha matte with no halo. On cream-on-cream elements (tape, pin) no automated method is reliable — hand-mask.

### 3.2 Automated element detection / slicing on a labeled sheet

**Connected-component labeling — [ImageMagick `connected-components`](https://imagemagick.org/connected-components/).**

```
magick sheet.png -fuzz 20% -transparent "#FBF9F6" \
  -define connected-components:verbose=true \
  -define connected-components:area-threshold=1000 \
  -connected-components 4 out.png
```

Output (per the docs): `Objects (id: bounding-box centroid area mean-color)`. `-define connected-components:area-threshold=N` merges small blobs into neighbors; `-define connected-components:mean-color=true` repaints by mean; default sort is decreasing area.

- **Failure mode.** CC needs a *binary mask* first (the `-transparent` step), so it inherits every fuzz-keying failure above. Worse: **touching elements merge into one component** — a row of swatches, a HUD mockup with nested panels, or a button group becomes a single bbox. And nested elements (panel inside a frame) produce parent/child blobs that need filtering by area. CC is a *helper after you have a good mask*, not a slicer.
- **Where it is useful.** Once an element is cropped and keyed, CC can find its inner bbox for `-trim` or detect whether it has transparent interior regions.

**OpenCV `findContours` / `MSER`.** Same fundamental shape: `findContours` (see [OpenCV shape docs](https://docs.opencv.org/4.x/d3/dc0/group__imgproc__shape.html)) traces iso-level boundaries on a binary image; `MSER` (Maximally Stable Extremal Regions) finds stable connected regions in a gradient — useful for *text* detection, less so for soft paper-cutout panels. Both need a threshold step first; both nest (parent/child contours); both merge touching elements. No improvement over IM CC for this use case.

**YOLO / object detection for UI elements.** Requires a labeled training set ("button", "swatch", "tape"). There is no off-the-shelf pre-trained model for "paper UI kit elements." Training one for a single design sheet is absurd. Skip.

**"UI design → assets" services — Anima, Locofy, TeleportHQ, Figma dev-mode.** These tools ([Anima](https://www.animaapp.com/), [Locofy](https://www.locofy.ai/), [TeleportHQ](https://teleporthq.io/), Figma dev-mode export) all operate on **layered source files** (`.fig`, `.sketch`, `.xd`) where each element is already a named vector/raster node. They export layers to code or PNGs. **None of them recover layers from a flattened PNG** — once layers are rasterized together, the boundary information is gone (a PNG is a pixel grid, not a scene graph). The design sheet here is a flattened PNG with no accompanying source file, so this entire tool category does not apply.

- **The only honest answer for slicing.** Manual crop-by-coordinate is the robust path. The layout is human-legible and one-of-a-kind; automation would cost more than the 6–10 crops are worth.

### 3.3 9-slice / 9-patch generation from a cropped frame

**CSS `border-image` — the project's mechanism.** Per [MDN `border-image`](https://developer.mozilla.org/en-US/docs/Web/CSS/border-image), the shorthand is `border-image: <source> <slice> / <width> / <outset> <repeat>`. [`border-image-slice`](https://developer.mozilla.org/en-US/docs/Web/CSS/border-image-slice) divides the source into **9 regions** via up to 4 inset values (top/right/bottom/left): 4 fixed corners, 4 edge regions (stretched or repeated per `border-image-repeat`), and a center region (included only with the `fill` keyword). The project uses `border-image: var(--tex-wood-frame) 14 14 14 14 stretch` — uniform 14 px slice, edges stretched, center dropped (no `fill`), so the element's own `background:` shows through the transparent frame interior.

**Android 9-patch — the authoring convention.** A `.9.png` adds a **1 px transparent border** with black guide pixels: top/left edges mark the stretchable region(s), bottom/right edges mark content padding. Authored with Android Studio's `draw9patch` tool. This is a *different* mechanism than CSS `border-image` (guide pixels vs. CSS slice values), but the mental model is the same: corners fixed, one axis of the edges stretched, center scaled.

**Project convention (load-bearing for new frames).** Measured against `wood-frame-9slice.png`:
- Canvas: 64×64.
- Frame ring width: **14 px** (matches `--frame-slice: 14`, `client/style.css:44`).
- Frame pixels: opaque golden-brown (`srgba(151,118,70,0.99)` from a 16×16 top-left sample).
- Center: fully transparent (`srgba(0,0,0,0)`), so the element's `background` shows through (no `fill` keyword in any of the 4 `border-image` declarations).

**To add a new frame from the design sheet**, produce a square PNG with: ring width `W` px of opaque art, fully transparent center, then wire it as `border-image: url('...') W W W W stretch;` and add a `--frame-slice-<name>: W` token. Keep `W` consistent so one CSS variable can govern all frames. The existing frame is 64×64 @ 14 px → ratio ≈ 0.22; matching that ratio keeps visual weight consistent.

**Tooling.**
- **[TexturePacker](https://www.codeandweb.com/texturepacker)** has a built-in 9-patch/9-scale editor (per their features page) and a pivot-point editor. Useful if you're already authoring sprite sheets; overkill for a single frame.
- **draw9patch** (bundled with Android SDK / Android Studio) — free, purpose-built for painting the 1 px guides onto a PNG. Works even if the target is CSS (just ignore the padding guides and read the stretch guides by eye to set your CSS `border-image-slice` value).
- **ImageMagick** can *crop* a frame out of the sheet but cannot author the 9-patch metadata — CSS doesn't need metadata anyway, just the slice number.

### 3.4 Color / tokens extraction from a swatch row

**Programmatic options.**
- **OpenCV k-means** (`cv2.kmeans`) on a downsampled crop → returns `k` centroid RGBs as the palette. Standard, deterministic, fast.
- **ImageMagick histogram** — `magick crop.png -colors N -format "%c" histogram:info:` prints `count: (r,g,b) #hex` per bucket, sorted by count. I used this on the full sheet and recovered the dominant cream `#FBF9F6` plus the warm variants and the navy `#121B23` in one command. For a swatch row specifically: crop the row, run `-colors <swatch-count>`, read the centroids.

**What the project already does.** `client/style.css:8` comment: *"Design #28 palette — tuned via OpenCV diff against design/main.png."* The palette tokens (`--paper-bg`, `--panel-header`, `--text-heading`, `--gold-accent`, etc.) are already locked and multiply-blended against `parchment-tile`. The typography tokens cite `design/component.png` directly (`client/style.css:31–33`: Teko/Inter/JetBrains Mono). So **the design's tokens have already been migrated by hand** — the swatch row in `component.png` is evidence of an already-applied decision, not new information.

**Verdict — not worth automating.** A swatch-reader script is ~10 lines and fun to write, but the tokens are in place, multiply-blend tuning is a one-time manual calibration, and new sheets arrive rarely. Run `magick ... histogram:info:` ad-hoc when a new swatch shows up; don't ship a pipeline.

---

## 4. Ranked recommendation with concrete commands

Assumptions: macOS dev box, ImageMagick 7 (`magick`; the `convert` warning is real — prefer `magick`), Python 3 available for `pip install rembg`, no project build step to preserve. Single user, one-of-a-kind sheet → optimize for control, not throughput.

### Tier 1 — Hand-crop + per-element key (primary, do this first)

```
# 1. Inspect the sheet with a viewer; note each element's bbox WxH+X+Y.
# 2. Crop + key + trim per element:
magick design/component.png -crop 320x120+X+Y +repage \
  -fuzz 15% -transparent "#FBF9F6" \
  -trim +repage \
  client/assets/textures/<name>.png
```

Tune `-fuzz` per element (start 10%, raise until cream clears, lower if edges erode). For cream-on-cream elements (tape, pin, ink stain), do **not** key — open in an editor (Preview, GIMP, Aseprite) and lasso-mask by hand, or use Tier 2.

**Effort:** ~2 min/asset. **Robustness:** high for high-contrast specimens; low for cream-on-cream. **Deps:** none new.

### Tier 2 — Rembg ML matting for anti-aliased / low-contrast specimens

```
# One-time:
pip install 'rembg[cpu]'
# Per element (crop FIRST — salient models confuse on full sheets):
magick design/component.png -crop WxH+X+Y +repage /tmp/el.png
rembg i -m isnet-general-use -a /tmp/el.png client/assets/textures/<name>.png
```

`-a` enables alpha matting (refines edges). `-om` writes only the mask if you want to inspect. Models: `isnet-general-use` (best for general specimens), `u2net` (default, fast), `silueta` (smaller). See [Rembg README · Models](https://github.com/danielgatis/rembg).

**Effort:** ~5 min setup + 30 sec/asset. **Robustness:** high for anti-aliased specimens on textured bg; still fails on truly cream-on-cream (no contrast = no signal). **Deps:** `rembg[cpu]` (pip).

### Tier 3 — remove.bg API (zero-install ML)

```
curl -H "X-Api-Key: $REMOVEBG_KEY" \
  -F "image_file=@/tmp/el.png" \
  -F "size=auto" -F "type=graphic" \
  -f https://api.remove.bg/v1.0/removebg -o client/assets/textures/<name>.png
```

Per the [API docs](https://www.remove.bg/api): `type=graphic` (added 2024-06-03) is tuned for non-subject foregrounds — the right choice for UI specimens. 1 credit/image (≤10 MP PNG). Crop first.

**Effort:** 30 sec/asset. **Robustness:** comparable to Rembg `isnet`. **Deps:** network + API key + credits.

### Tier 4 — Do NOT automate

- **Slicing** (CC / contours / YOLO / "design-to-assets" services) — not viable on a flattened labeled sheet; the layout is one-of-a-kind and manual crops win.
- **Palette extraction** — tokens already locked in `client/style.css:8`. Use `magick ... histogram:info:` ad-hoc if a new swatch appears.
- **Building a `client/editor/asset_extractor.html` tool** — only worth it if design sheets become a recurring input (multiple sheets/month). For a single sheet it's over-engineering. If that changes, model it on the existing `client/editor/atlas_packer.html` pattern (per `docs/research/atlas-split-reassemble.md` §4.1).

### New frames must match the 9-slice convention

```
# Produce a square PNG: opaque ring width W, transparent center.
# Wire it in style.css:
--tex-<new>-frame: url('assets/textures/<new>-frame-9slice.png');
--frame-slice-<new>: W;   /* match the ring width you authored */
...
border-image: var(--tex-<new>-frame) var(--frame-slice-<new>) ... stretch;
```

Keep the ring-width-to-canvas ratio near the existing 14/64 ≈ 0.22 unless the design clearly calls for a heavier frame.

---

## 5. Adversarial failure-mode summary

| Technique | Expected failure on THIS sheet |
|---|---|
| IM `-fuzz -transparent` (whole sheet) | No single fuzz clears texture without eroding edges; cream variants span >25% RGB distance. |
| IM `-fuzz -transparent` (per element) | Cream halos on anti-aliased edges; keys out cream-colored specimens (tape, pin). |
| Rembg / remove.bg on whole sheet | Salient-object confusion; segments only the HUD mockup or fragments. |
| Rembg / remove.bg on cropped element | Still fails on cream-on-cream (no contrast). |
| Connected-components slicing | Needs a clean mask first; merges touching/nested elements; inherits fuzz failures. |
| OpenCV findContours / MSER | Same — binary threshold first; merges touching elements. |
| YOLO / UI detectors | No pre-trained model for paper-kit elements; training is absurd for one sheet. |
| Anima / Locofy / Figma dev-mode | Require layered source (`.fig`); cannot recover layers from a flattened PNG. |
| BackgroundMattingV2 | Needs a captured empty-bg frame; not applicable to a static sheet. |
| MODNet | Portrait-only; wrong domain. |
| Palette automation | Tokens already locked (`style.css:8`); marginal value. |

---

## 6. Sources

| Source | URL | Used for |
|---|---|---|
| ImageMagick `-transparent` | https://imagemagick.org/command-line-options/#transparent | Threshold transparency keying |
| ImageMagick `-fuzz` | https://imagemagick.org/command-line-options/#fuzz | RGB-distance color matching semantics |
| ImageMagick connected-components | https://imagemagick.org/connected-components/ | CC labeling, `area-threshold`, `mean-color`, `verbose` defines |
| MDN `border-image` | https://developer.mozilla.org/en-US/docs/Web/CSS/border-image | CSS 9-slice model: source/slice/width/outset/repeat |
| MDN `border-image-slice` | https://developer.mozilla.org/en-US/docs/Web/CSS/border-image-slice | 9-region division, `fill` keyword |
| Rembg (README) | https://github.com/danielgatis/rembg | CLI usage (`rembg i`, `-m`, `-a`, `-om`), model list (`u2net`, `isnet-general-use`, `birefnet-general`, `silueta`) |
| U²-Net | https://github.com/xuebinqin/U-2-Net | The salient-object model under Rembg (Pattern Recognition 2020) |
| MODNet | https://github.com/ZHKKKe/MODNet | Portrait-only matting (AAAI 2022) — dismissed as wrong domain |
| BackgroundMattingV2 | https://github.com/PeterL1n/BackgroundMattingV2 | Requires captured bg frame — dismissed as not applicable |
| remove.bg API | https://www.remove.bg/api | `POST /v1.0/removebg`, `size=auto`, `type=graphic`, credit model |
| TexturePacker | https://www.codeandweb.com/texturepacker | Built-in 9-patch editor (cited as tooling option) |
| Android 9-patch (draw9patch) | https://developer.android.com/studio/write/draw9patch | 1px-guide 9-patch authoring convention (attempted fetch; standard reference) |
| OpenCV `imgproc` shape module | https://docs.opencv.org/4.x/d3/dc0/group__imgproc__shape.html | `findContours` / hierarchy reference |
| `client/style.css` | `client/style.css:8, 31-33, 36-44, 177-184, 429, 1018, 1200` | Palette tokens, typography tokens, texture url vars, `--frame-slice: 14`, tiled-bg pattern, `border-image` usage |
| `client/assets/textures/` | 6 PNGs (see §2.2) | Existing texture inventory + dimensions (sips) |
| `scripts/gen-textures.py` | `scripts/gen-textures.py:1-80` | Procedural texture generator (Pillow, deterministic SEED) |
| `design/component.png` | 1672×941, 155,640 unique colors, bg `#FBF9F6` | The design sheet under analysis (measured via ImageMagick) |
| `docs/research/atlas-split-reassemble.md` | prior note | Distinct (unit-sprite atlas), cited to keep scope boundary clear |
| `docs/research/art-asset-pipeline.md` | prior note | Established that UI textures are CSS-only, not WebGL-loaded |
