# 0010 — Campaign Map Visualization

Date: 2026-06-13

## Context

The procedural map generator (ADR-0006) produces heightmap-based terrain with hills,
rivers, lakes, forests, strongholds, and bridges. However, the client rendered every
tile as a flat colored square from a static palette with no visual depth. The minimap
showed only unit dots on a black background. The server had elevation data (`int8`)
in each tile but never sent it to the client.

## Decision

### 1. Extended map data protocol (2 bytes/tile)

Server `MapData()` now packs interleaved `[terrainType, elevation]` per tile instead
of terrain type only. Total payload: `2 * w * h` bytes (9216 bytes for 48x96 map).
Elevation is `int8` (0-100) from the heightmap, cast to `uint8` for transport.

### 2. Client terrain rendering enhancements

- **Hills**: Brighten base color by elevation ratio. Low hills = muted tan, peaks =
  bright sandy highlights. `lerp(base, highlight, elevation/100 * 0.5)`.
- **Plains**: Deterministic noise jitter (integer hash of x,y coordinates) adds ±3%
  brightness variation per tile, breaking up flat solid appearance.
- **Deep water**: Blue channel oscillates with `sin((x+y)*0.7 + frame*0.15) * 0.04`
  for animated shimmer.
- **Shallow water**: Gentler animation with slight green tint shift.
- **Shoreline**: Water tiles adjacent to land tiles are darkened by 4% for edge effect.
- **Bridge**: Subtle alternating brightness variation per tile coordinate.

### 3. Terrain object layer (Pass 2)

Forest tiles, strongholds, and bridges now draw small colored quads in the existing
`objectBatch` (Pass 2 of `gl.js` renderer), which was previously unused.

- **Forest trees**: 1-3 tree sprites per forest tile (hash-determined count and position).
  Each tree = brown trunk rect + dark green canopy rect.
- **Strongholds**: Stone keep icon (gray rect base + brown roof rect above). Size scales
  with stronghold level (L1-L5).
- **Bridges**: 3 horizontal dark-brown plank lines per bridge tile.

All objects include `sortY` for Y-sorting via painter's algorithm (already implemented
in `drawObjects`).

### 4. Cached minimap terrain

`buildMinimapTerrain()` renders terrain colors to an offscreen `<canvas>` (1 pixel per
tile) once on map data receipt. The minimap `drawMinimap()` draws this cached image
stretched to fit, then overlays unit dots and viewport rectangle each frame.

Performance: one `putImageData()` call at map load, one `drawImage()` per frame.

### 5. Backward compatibility

The client `setMapTerrain()` de-interleaves the 2-byte format into separate
`terrainData` and `elevationData` arrays. All terrain rendering gracefully falls back
to flat colors if `elevationData` is null (e.g. clash maps with no elevation).

## Files Modified

- `server/pkg/game/session.go` — `MapData()` packs 2 bytes/tile
- `client/src/main.js` — `setMapTerrain()`, `buildTerrainTiles()`, new
  `buildTerrainObjects()`, `buildMinimapTerrain()`, updated `drawMinimap()`
- `client/src/connection.js` — unchanged (passes raw bytes through)
- `client/src/gl.js` — unchanged (drawObjects already existed)

## Performance

- Terrain batch: still 1 draw call (same quad count)
- Object batch: 1 draw call (new, was unused)
- Minimap: 1 `drawImage()` + O(units) `fillRect` per frame
- No per-frame allocation for minimap terrain (cached)
