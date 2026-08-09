# Unit sprite atlas override

Drop a hand-authored or AI-generated atlas here as **`unit_atlas.png`** to
replace the procedurally-generated unit sprites.

## Contract

- **Dimensions:** exactly **1024 × 576** px. Any other size is rejected at
  load and the procedural atlas is kept.
- **Layout:** the 32×32-cell grid defined by `atlasCell()` in
  `client/src/unit_atlas.js`. Each `(unitType, state, dir, frame)` sprite
  occupies one 32×32 cell. There are 140 sprites × 4 reserved cells.
- **Transparency:** empty cells must be transparent (alpha 0). The renderer
  alpha-blends, and team/HP tints multiply the sampled texel — so paint
  sprites in neutral white/grey and let the shader tint them, or paint in
  full color if you want fixed shading.

## How it loads

At startup `main.js` generates the procedural atlas, uploads it, then calls
`loadUnitAtlasImage()` (in `unit_atlas.js`), which fetches
`assets/sprites/unit_atlas.png`. If the file exists and matches 1024×576 it
swaps in; otherwise it silently no-ops and the procedural sprites stay. No
renderer or shader changes are involved.

## Generating a template

The procedural atlas is itself a valid grid-correct template. From the
browser console while the game is running:

```js
import('./src/unit_atlas.js').then(m => m.downloadUnitAtlasTemplate());
```

This downloads `unit_atlas.png` — paint over it in any tool (Penpot,
OpenPencil, Photoshop, Aseprite) and drop the result back in this folder.

See `docs/research/art-asset-pipeline.md` for the full pipeline discussion.
