# Art Asset Pipeline (OpenPencil / Penpot) — Research Note

**Date:** 2026-08-09
**Trigger:** User ask "how to import openpencil to our project, use openpencil to generate art assets. or the other way, such as Penpot". This note disambiguates the tools, grounds them against the project's actual art pipeline, and gives an honest recommendation.

---

## TL;DR

"OpenPencil" is a **real, distinct project** — it is not Pencil2D. It is an open-source, AI-native, Figma-compatible **vector design editor** (`open-pencil/open-pencil`, MIT, Tauri+Vue+Skia). The user is therefore comparing **two vector design tools** (OpenPencil vs Penpot), not an animation tool vs a design tool. Both are external asset *authoring* environments — neither is a runtime dependency you "import" into the game.

paper-war's art pipeline is **100% procedural** today: unit sprites are drawn at startup onto an HTMLCanvasElement by `client/src/unit_atlas.js:733` (`generateUnitAtlas()`), then uploaded as a WebGL2 texture via `client/src/gl.js:1012` (`setUnitTexture()`). There is **no art-import path** and **no on-disk sprite files** for units. The only image assets in the repo are 6 HUD/chrome PNGs under `client/assets/textures/` consumed by CSS, not by the WebGL renderer.

**Recommendation:** neither tool is strictly *needed* — the procedural pipeline is intentional and ships. If the goal is to replace procedural unit sprites with hand-crafted art, **Penpot is the lower-risk choice** (production-mature, SVG/PNG export, plugin API). **OpenPencil is the more interesting architectural fit** (Design-as-Code, MCP, scriptable, AI-native, `.fig` native) — and it pairs unusually well with this codebase's code-defined philosophy and the existing Claude Code tooling — but its own docs say *"Active development. Not ready for production use."* The wire-in is identical for both: export PNG frames → match the `atlasCell()` grid layout → load as an image into `setUnitTexture()`.

---

## 1. "OpenPencil" disambiguation

"OpenPencil" is ambiguous in the wild. Verified against primary sources:

| Candidate | Is it real? | Source |
|---|---|---|
| **OpenPencil** (`open-pencil/open-pencil`) | **Yes — and almost certainly what the user means**, given the user paired it with "Penpot". Both are open-source Figma-alternative *vector design* tools. | [official docs](https://open-pencil-open-pencil.mintlify.app/introduction), [npm `@open-pencil/mcp`](https://www.jsdelivr.com/package/npm/@open-pencil/mcp) |
| Pencil2D (`pencil2d/pencil2d`) | Real, but **not** what "OpenPencil" refers to. It is a 2D *hand-drawn animation* tool (raster/vector frames), GUI-centric, no spritesheet export. A poor fit for this codebase. | [pencil2d.org](https://www.pencil2d.org/), [forum: no spritesheet export](https://discuss.pencil2d.org/t/exporting-as-a-png-sprite-sheet-rather-than-a-video/4056) |
| OpenToonz, Synfig | Real animation tools; not named "OpenPencil" and not design/vector tools. Out of scope. | — |

**A project literally named "OpenPencil" does exist** and is a vector design editor / Figma alternative. That is what the rest of this note studies. (For completeness: there is also `ZSeven-W/openpencil`, a separate "AI-native vector design tool in pure Rust" claim — the `open-pencil` org + Mintlify docs + npm package are the more substantive, citable project and are treated as canonical here.)

---

## 2. How paper-war consumes art today

Grounded in the code (CodeGraph + on-disk files).

### 2.1 Units are 100% procedurally drawn — no art files

- **`client/src/unit_atlas.js:733` — `generateUnitAtlas()`** builds an `HTMLCanvasElement` (1024×576) at startup and draws every unit × state × direction × frame cell with 2D-canvas ops. No external image is loaded.
- **Atlas grid constants** (`client/src/unit_atlas.js:45-86`): `ATLAS_CELL = 32`, `ATLAS_COLS = 32`, `MAX_FRAMES_PER_SPRITE = 4`, 7 unit types × 5 states × 4 directions, `ATLAS_W = 1024`, `ATLAS_H = 576`. `FRAMES_PER_STATE = [2,2,4,3,4]`.
- **`atlasCell(unitType, state, dir, frame)`** (`client/src/unit_atlas.js:105-118`) is the single source of truth for the grid layout — any externally-authored art must match this cell mapping exactly, or the atlas sampler will read the wrong pixels.
- The renderer explicitly notes this is procedural, with a white-pixel fallback before the canvas lands: `client/src/gl.js:981-985`.

### 2.2 The natural seam for externally-generated art

- **`Renderer.setUnitTexture(canvas, atlasW, atlasH)`** (`client/src/gl.js:1012-1037`) uploads any image source as the unit atlas via `gl.texImage2D(...)`. Its JSDoc param type is `HTMLCanvasElement | ImageBitmap | HTMLImageElement` — so an `HTMLImageElement` loaded from a PNG file drops in with **no signature change**.
- Filtering is `NEAREST` (`gl.js:1030-1031`) to preserve pixel-art edges — any imported art should be authored at 32px cell resolution (or a clean integer multiple) to survive downscaling crisply.

### 2.3 The editors are parameter tuners, not art importers

- **`client/editor/animation_editor.js`** (`client/editor/animation_editor.js:1-30`) imports the live `unit_atlas.js` symbols (`drawCell`, `atlasCell`, `ATLAS_*`, `FRAMES_PER_STATE`, `ANIM_FPS`) and is explicitly *"Read-only against sprite code — tunes parameters only."* It previews what `generateUnitAtlas()` produces; it does not consume art files.
- **`client/editor/units_editor.js`** (648 lines) authors gameplay constants (weapons/armor/unit tables) — not visual assets.
- Conclusion: there is **no art-import path today**. Building one means (a) authoring art in an external tool, (b) exporting PNG, (c) adding an image-load branch in/around `generateUnitAtlas()`.

### 2.4 Image assets that DO exist (HUD chrome, not game sprites)

```
client/assets/textures/
  gold-cta-tile.png      ink-border.png        navy-header-tile.png
  parchment-tile.png     parchment-warm.png    wood-frame-9slice.png
```
These are referenced as CSS `url(...)` backgrounds in `client/style.css:36-41` (the Paper UIKit / ADR-0021 design system). They are **never loaded by the WebGL renderer**. The `design/` directory holds `map.png`, `main.png`, `component.png` — reference mockups, also not runtime assets.

### 2.5 Aesthetic

Shader comments in `client/src/gl.js` explicitly target a *"pixel-art look"* (noise quantized with `floor()` onto a chunky pixel grid) and the design references are flat paper/ink textures. This is a **flat paper-cutout / pixel-art** aesthetic — relevant: vector tools that export clean SVG/PNG fit; photorealistic or smooth-gradient art does not.

---

## 3. OpenPencil findings

Primary source: [official docs](https://open-pencil-open-pencil.mintlify.app/introduction) + [docs index (`/llms.txt`)](https://open-pencil-open-pencil.mintlify.app/llms.txt) + [npm `@open-pencil/mcp`](https://www.jsdelivr.com/package/npm/@open-pencil/mcp).

- **What it is.** Open-source (MIT), AI-native, Figma-compatible vector design editor. Local-first. Built with Vue 3 + Tauri v2 desktop shell; Skia (CanvasKit WASM) renderer; Yoga layout; file format is Figma's Kiwi binary + Zstd + ZIP.
- **Status (load-bearing caveat).** The docs state verbatim: *"Active development. Not ready for production use."* Planned features include 100% `.fig` parity, shader effects, code signing, CI visual-regression tooling. Treat as alpha.
- **Figma compatibility.** Native `.fig` import/export, copy-paste with Figma (same Kiwi binary), full rendering parity target.
- **Export formats (CLI-confirmed).** The CLI command [`export`](https://mintlify.wiki/open-pencil/open-pencil/cli/export.md) is summarized in `/llms.txt` as: *"Render .fig files to PNG, JPG, or WEBP."* I could not confirm a first-class **SVG** export from the CLI page itself (the renderer is Skia/raster). Vector-native editing is real, but if you need SVG out, verify against the live `export` doc before committing.
- **Generation / automation (the strong suit).**
  - **Headless CLI** `@open-pencil/cli`: `inspect`, `find`, `analyze`, `eval` ("Execute JavaScript with Figma Plugin API"), `export`. Scriptable end-to-end — you can generate a design from code without opening the GUI.
  - **MCP server** `@open-pencil/mcp` over stdio or HTTP: connect Claude Code, Cursor, or any MCP client to *read/write `.fig` files headlessly*. 75 tools wired to chat/CLI/MCP, including create-shape, set fill/stroke/layout, variables, vectors, boolean ops, find nodes, "render JSX to design nodes."
  - Two modes: **app mode** (drives the running editor) and **file mode** (operates on a `.fig` directly).
- **Fit for paper-war.** The "Design-as-Code" + MCP + `eval` model maps cleanly onto this repo's code-defined-unit philosophy and its existing Claude Code setup. You could in principle script a node-tree that emits one 32×32 cell per `(unitType, state, dir, frame)`, then `op export` a PNG atlas. The blocker is maturity: alpha status means the rendering/output may still shift under you.

### Integration path (OpenPencil → paper-war)

There is **nothing to literally `import`** as a runtime dep. OpenPencil is an offline authoring/rendering tool. The wire-in is:

1. `npm i -g @open-pencil/cli @open-pencil/mcp` (or run the desktop app).
2. Author unit cells (or script them via `eval`/MCP) in a `.fig` file, one frame per `atlasCell()` slot — grid is 32 cols × 18 rows of 32px cells, `ATLAS_W=1024 × ATLAS_H=576` (see §2.1).
3. `op export <file.fig> --format png` to render the atlas to a 1024×576 PNG.
4. In `client/src/unit_atlas.js`, add an image-load branch: replace the `generateUnitAtlas()` canvas with an `HTMLImageElement` (or `ImageBitmap`) sourced from `client/assets/textures/unit-atlas.png`, then call the existing `Renderer.setUnitTexture(img, ATLAS_W, ATLAS_H)` (`client/src/gl.js:1012`). No renderer changes required.

---

## 4. Penpot findings

Primary source: [Penpot export guide](https://help.penpot.app/user-guide/export-import/exporting-layers/) + [plugin hub](https://penpot.app/penpothub/plugins/svg-exporter) + Penpot GitHub.

- **What it is.** Open-source (MPL) design & prototyping platform, hosted or self-hosted, multi-user real-time collaboration. The mature, production-used Figma alternative.
- **Export formats (officially confirmed).** Per the [Exporting layers guide](https://help.penpot.app/user-guide/export-import/exporting-layers/): **PNG, JPEG, WEBP, SVG, PDF**. SVG is the *native rendering format* — "100% of SVG features will be converted perfectly" applies to SVG export; PDF export has a known rasterization caveat, so prefer **SVG or PNG**.
- **Export UX.** Per-layer export presets with scale + suffix; multiple presets per layer (one click → PNG@1x, PNG@2x, SVG). Also exposed in View-mode "code" tab.
- **Generation / automation.**
  - **Plugin API** — e.g. [SVG Exporter](https://penpot.app/penpothub/plugins/svg-exporter) (one-click SVG to clipboard), "Export SVGs to URI" plugin (data URI / code snippet), and `node.export()` for reading image data from plugins.
  - **REST / backend API** — programmatic export to third-party apps (see [GitHub discussion #462](https://github.com/penpot/penpot/discussions/462), [issue #545](https://github.com/penpot/penpot/issues/545)). Less polished than the plugin path.
  - No native **spritesheet** exporter — same gap as everywhere else; combine exported PNG cells externally (or lay them out in a Penpot frame sized to 1024×576 and export the whole frame as one PNG).
- **Fit for paper-war.** SVG-native export matches the paper-cutout vector aesthetic better than raster. PNG export drops straight into the WebGL atlas path. Lower risk than OpenPencil because Penpot is mature. The cost is that Penpot is *less* programmable / AI-native than OpenPencil — you're driving a GUI, not scripting a node tree.

### Integration path (Penpot → paper-war)

1. Create a Penpot file (hosted at `design.penpot.app` or self-hosted).
2. Build a 1024×576 frame; place each unit sprite in its `atlasCell()` slot (32×32 cells, 32 cols × 18 rows — §2.1).
3. Add an export preset on the frame: **PNG @1x** (or SVG if you add an SVG→raster step). One-click export → `unit-atlas.png`.
4. Drop into `client/assets/textures/unit-atlas.png` and wire `generateUnitAtlas()` to load it as an `HTMLImageElement` → `Renderer.setUnitTexture(img, ATLAS_W, ATLAS_H)` (`client/src/gl.js:1012`). Identical seam as OpenPencil.

---

## 5. Recommendation

**For this specific project** — WebGL2, procedural canvas atlas, code-defined units, flat paper-cutout/pixel-art aesthetic, existing Claude Code tooling:

1. **Default: stay procedural.** The current pipeline (`unit_atlas.js` draws the atlas at runtime) is intentional, dependency-free, and keeps the repo tiny. "Use a design tool to generate art" only wins if you actually want hand-authored art that code can't easily express (rich silhouettes, detailed unit identity beyond the 7 team-tinted shapes).

2. **If you want hand-authored unit art → Penpot is the lower-risk pick.** Production-mature, SVG/PNG export confirmed by official docs, plugin API for batch export. Accept the GUI-centric workflow. Wire-in is a single image-load branch in `unit_atlas.js` + the existing `setUnitTexture()`.

3. **If you want programmable / AI-generated art and can stomach alpha → OpenPencil is the better *architectural* fit.** Its Design-as-Code + MCP + `eval` model lets you generate the atlas from a script (or from Claude Code over MCP) without a GUI — which mirrors how this very repo already treats units as code. Just know the tool self-describes as *"not ready for production use,"* so verify SVG/PNG output stability before depending on it.

4. **Pencil2D is not the tool.** It's a hand-drawn *animation* tool, "OpenPencil" refers to a distinct design tool, Pencil2D has no native spritesheet export, and it has no automation surface. Dismiss it unless the user clarifies they meant frame-by-frame animation authoring.

**Concrete first step (whichever tool):** add an `HTMLImageElement` load path in `client/src/unit_atlas.js` that, if `client/assets/textures/unit-atlas.png` exists, returns that image for `setUnitTexture()` instead of calling `generateUnitAtlas()`. That keeps the procedural fallback and makes the asset pipeline tool-agnostic — you can swap Penpot/OpenPencil output freely without touching the renderer.

---

## 6. Sources

| Source | URL | Used for |
|---|---|---|
| OpenPencil official docs | https://open-pencil-open-pencil.mintlify.app/introduction | What OpenPencil is, status, features, tech stack, license |
| OpenPencil docs index (`/llms.txt`) | https://open-pencil-open-pencil.mintlify.app/llms.txt | CLI `export` command → "Render .fig files to PNG, JPG, or WEBP"; full CLI/API tool list |
| `@open-pencil/mcp` on npm (jsDelivr) | https://www.jsdelivr.com/package/npm/@open-pencil/mcp | MCP server package, headless Vue SDK |
| open-pencil skill reference | https://explainx.ai/skills/open-pencil/skills/open-pencil | Two modes: app mode vs file mode; CLI/MCP usage |
| Pencil2D home | https://www.pencil2d.org/ | Disambiguation — Pencil2D is an animation tool, not "OpenPencil" |
| Pencil2D forum: no spritesheet export | https://discuss.pencil2d.org/t/exporting-as-a-png-sprite-sheet-rather-than-a-video/4056 | Pencil2D cannot export spritesheets natively |
| Penpot exporting-layers guide | https://help.penpot.app/user-guide/export-import/exporting-layers/ | Export formats: PNG, JPEG, WEBP, SVG, PDF; export presets |
| Penpot SVG Exporter plugin | https://penpot.app/penpothub/plugins/svg-exporter | Plugin-based SVG export |
| Penpot GitHub discussion #462 | https://github.com/penpot/penpot/discussions/462 | REST/backend API for programmatic SVG export |
| Penpot GitHub issue #545 | https://github.com/penpot/penpot/issues/545 | SVG as native rendering format; import/export support |
| `client/src/unit_atlas.js` | `client/src/unit_atlas.js:45-86, 105-118, 733-761` | Procedural atlas generation, grid layout, constants |
| `client/src/gl.js` | `client/src/gl.js:981-985, 1012-1037` | `setUnitTexture()` seam — accepts canvas/image/bitmap |
| `client/editor/animation_editor.js` | `client/editor/animation_editor.js:1-30` | Editor is read-only parameter tuner over `unit_atlas.js` |
| `client/assets/textures/` | 6 PNG files (see §2.4) | Only on-disk art assets; HUD chrome consumed by CSS, not WebGL |
| `client/style.css:36-41` | CSS `url(...)` refs to texture PNGs | Confirms UI textures are CSS-only, not renderer-loaded |
