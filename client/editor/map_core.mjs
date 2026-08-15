// map_core.mjs — pure (DOM-free) logic for the clash map editor.
//
// Everything here is node-testable (map_core_test.mjs) and mirrors server
// constants where noted. map_editor.js keeps the DOM wiring and imports
// from this module. Convention matches client/src/*_test.mjs.

// --- Constants (mirror server/pkg/component/movement.go) --------------------
export const GRID = 32;

export const TERRAIN_NAMES = [
  'Plain', 'Road', 'Shallow', 'Deep', 'Forest', 'Hill', 'Swamp', 'Bridge',
  'Wall', 'Snow', 'Desert',
  '(reserved)', '(reserved)', '(reserved)', '(reserved)', '(reserved)', // 11-15 retired stronghold terrain (#54)
  'Rock', 'Brush',
  'Ramp', // 18 — graded ramp; permits crossing a 2-tier cliff (Phase 1)
];

// Terrain ids retired from the enum (stronghold moved to entity, #54). Skipped
// in the palette and in export.
export const RESERVED_TERRAIN = new Set([11, 12, 13, 14, 15]);

// Terrain type colors — copied verbatim from client/src/main.js (TERRAIN_COLORS).
// Histogram-tuned to the dark earthy pixel-art palette of design/map.png.
export const TERRAIN_COLORS = [
  [0.28, 0.41, 0.15],  // 0 Plain
  [0.66, 0.52, 0.32],  // 1 Road
  [0.22, 0.40, 0.55],  // 2 Shallow
  [0.22, 0.48, 0.61],  // 3 Deep
  [0.11, 0.22, 0.055], // 4 Forest
  [0.62, 0.50, 0.30],  // 5 Hill
  [0.20, 0.28, 0.11],  // 6 Swamp
  [0.50, 0.33, 0.16],  // 7 Bridge
  [0.48, 0.45, 0.40],  // 8 Wall
  [0.82, 0.86, 0.90],  // 9 Snow
  [0.66, 0.55, 0.33],  // 10 Desert
  [0.54, 0.32, 0.18],  // 11 Stronghold1
  [0.59, 0.35, 0.18],  // 12 Stronghold2
  [0.64, 0.38, 0.18],  // 13 Stronghold3
  [0.69, 0.41, 0.18],  // 14 Stronghold4
  [0.74, 0.44, 0.18],  // 15 Stronghold5
  [0.40, 0.38, 0.36],  // 16 Rock — stone gray (heavier than Wall)
  [0.34, 0.42, 0.20],  // 17 Brush — scrubby olive-green
  [0.76, 0.64, 0.40],  // 18 Ramp — graded earth tone (distinct from Hill/Road)
];

// hillShadeRGB — copied verbatim from client/src/main.js. Layer 0 = valley
// shadow, 1 = mid (unchanged), 2 = stone-gray peak. Applied to Hill tiles in
// the preview so elevation reads. Returns [r,g,b] in 0..1.
export function hillShadeRGB(r, g, b, layer) {
  if (layer === 2) {
    return [
      r + (0.74 - r) * 0.55,
      g + (0.74 - g) * 0.55,
      b + (0.72 - b) * 0.55,
    ];
  }
  if (layer === 0) {
    return [r * 0.88, g * 0.88, b * 0.88];
  }
  return [r, g, b];
}

// Runtime clash spawns (main.go:377-389): mw/2 ± 0 on x, mh/2 ± 4 on y.
// On the 32×32 canvas that's (16,12) and (16,20). Shown as on-canvas markers
// so the author knows where squads actually appear.
export const RUNTIME_SPAWNS = [
  { x: 16, y: 12, color: '#6fa3d6' },
  { x: 16, y: 20, color: '#d6a36f' },
];

// Movement-profile terrain costs — copied verbatim from
// server/pkg/component/profiles.go (StandardMovementProfiles). cost 0 =
// impassable. Indices 0..18 line up with terrain ids (18 = Ramp). Used by the
// live connectivity check (mirrors isConnected in map_validate.go).
export const PROFILE_COSTS = {
  Light: [1,1,2,0,2,3,3,1,0,2,2,1,1,1,1,1,4,1,1,0],
  Heavy: [1,1,0,0,3,4,4,1,0,3,2,1,1,1,1,1,5,2,1,0],
};

// --- Brush geometry -----------------------------------------------------------

// brushOffsets returns the [{dx,dy}] tile offsets of a brush footprint
// centered on the paint anchor. size 1/2/3/5 (SC-style); shape 'square' or
// 'circle'. For even sizes the anchor is the top-left of the footprint block;
// for odd sizes it is the exact center tile. The circle mask keeps every tile
// whose center lies within radius size/2 of the footprint center.
//
// Memoized — the editor calls this per mousemove.
const _offsetCache = new Map();
export function brushOffsets(size, shape = 'square') {
  const key = `${size}:${shape}`;
  if (_offsetCache.has(key)) return _offsetCache.get(key);
  const c = (size - 1) / 2; // footprint center (x.5 for even sizes)
  const r2 = (size / 2) * (size / 2) + 1e-9;
  const out = [];
  for (let dy = 0; dy < size; dy++) {
    for (let dx = 0; dx < size; dx++) {
      if (shape === 'circle') {
        const ex = dx - c, ey = dy - c;
        if (ex * ex + ey * ey > r2) continue;
      }
      const ox = dx - Math.floor(c), oy = dy - Math.floor(c);
      out.push({ dx: ox, dy: oy });
    }
  }
  _offsetCache.set(key, out);
  return out;
}

// rectIndices returns the grid indices of the rectangle between two corners
// (either drag direction), clamped to the map. x0/y0 = anchor, x1/y1 = current.
export function rectIndices(x0, y0, x1, y1) {
  const xa = Math.max(0, Math.min(x0, x1));
  const xb = Math.min(GRID - 1, Math.max(x0, x1));
  const ya = Math.max(0, Math.min(y0, y1));
  const yb = Math.min(GRID - 1, Math.max(y0, y1));
  const out = [];
  for (let y = ya; y <= yb; y++) {
    for (let x = xa; x <= xb; x++) {
      out.push(y * GRID + x);
    }
  }
  return out;
}

// --- Flood fill ----------------------------------------------------------------

// floodFill fills the 4-connected region with the same (terrain, elevation)
// as the origin tile. payload is {terrain: id} to repaint terrain (elevation
// of filled tiles untouched) or {elev: layer} to repaint elevation (gated to
// Hill tiles — same rule as the brush). Mutates the arrays in place; returns
// the number of tiles changed (0 when the payload is already in effect, so a
// click is a no-op edit rather than an empty undo entry).
export function floodFill(terrain, elevation, sx, sy, payload) {
  const origin = sy * GRID + sx;
  const t0 = terrain[origin], e0 = elevation[origin];
  if (payload.terrain !== undefined && payload.terrain === t0) return 0;
  if (payload.elev !== undefined) {
    const target = payload.elev;
    if (terrain[origin] !== 5) return 0; // elevation only lands on Hill (brush rule)
    if (e0 === target) return 0;
  }
  const visited = new Uint8Array(GRID * GRID);
  let changed = 0;
  const stack = [origin];
  visited[origin] = 1;
  while (stack.length) {
    const i = stack.pop();
    const x = i % GRID, y = (i - (i % GRID)) / GRID;
    if (terrain[i] !== t0 || elevation[i] !== e0) continue; // region predicate
    if (payload.terrain !== undefined) {
      terrain[i] = payload.terrain;
    } else {
      if (terrain[i] !== 5) continue; // per-tile Hill gate
      elevation[i] = payload.elev;
    }
    changed++;
    // 4-neighbours
    if (x > 0 && !visited[i - 1]) { visited[i - 1] = 1; stack.push(i - 1); }
    if (x < GRID - 1 && !visited[i + 1]) { visited[i + 1] = 1; stack.push(i + 1); }
    if (y > 0 && !visited[i - GRID]) { visited[i - GRID] = 1; stack.push(i - GRID); }
    if (y < GRID - 1 && !visited[i + GRID]) { visited[i + GRID] = 1; stack.push(i + GRID); }
  }
  return changed;
}

// --- View transform --------------------------------------------------------------

// Zoom levels in px-per-tile (SC-editor style discrete steps).
export const ZOOM_LEVELS = [8, 16, 32, 64];

export const MIN_ZOOM = ZOOM_LEVELS[0];
export const MAX_ZOOM = ZOOM_LEVELS[ZOOM_LEVELS.length - 1];

// View is {px: px-per-tile, ox, oy: screen offset of map origin (0,0) in
// canvas pixels}. All conversions between screen pixels and tiles go through
// these two functions — the single choke point so every tool stays correct
// under zoom/pan.
export function screenToTile(sx, sy, view) {
  return { x: Math.floor((sx - view.ox) / view.px), y: Math.floor((sy - view.oy) / view.px) };
}
export function tileToScreen(tx, ty, view) {
  return { x: tx * view.px + view.ox, y: ty * view.px + view.oy };
}

// zoomAround keeps the tile under the given screen point fixed while
// changing the px-per-tile (the anchor tile may be fractional — callers pass
// the exact cursor position). Returns a NEW view; clamps px to the level set.
export function zoomAround(sx, sy, nextPx, view) {
  const px = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, nextPx));
  // Keep the exact sub-tile point under (sx, sy) fixed to avoid anchor drift.
  const exactTx = (sx - view.ox) / view.px;
  const exactTy = (sy - view.oy) / view.px;
  return { px, ox: sx - exactTx * px, oy: sy - exactTy * px };
}

// clampPan keeps the map covering the viewport when the map is larger than
// it, and centers the map when it is smaller. w/h are the canvas dimensions.
export function clampPan(view, w, h) {
  const mapPx = GRID * view.px;
  let ox = view.ox, oy = view.oy;
  if (mapPx <= w) {
    ox = (w - mapPx) / 2;
  } else {
    ox = Math.min(0, Math.max(w - mapPx, ox));
  }
  if (mapPx <= h) {
    oy = (h - mapPx) / 2;
  } else {
    oy = Math.min(0, Math.max(h - mapPx, oy));
  }
  return { px: view.px, ox, oy };
}


// --- Ramp grading -----------------------------------------------------------------

// RAMP_TERRAIN is the terrain id of the graded ramp (component.TerrainRamp).
export const RAMP_TERRAIN = 18;

// rampElevationAt returns the auto-graded elevation for a ramp tile painted
// at (x,y): one tier below the highest 4-neighbour tier, clamped to 0..2.
// This makes the ramp the intermediate step of the cliff it crosses — a Δ2
// cliff becomes two Δ1 edges (walkable), and the server's ramp rule
// (EdgeWalkableFor, tilemap.go:165-170) still catches residual Δ2 pairs.
// Elevation of non-adjacent tiles is ignored; neighbours are read from the
// CURRENT grid (pre-mutation), which is what a drag paints along its path.
export function rampElevationAt(elevation, x, y) {
  let maxN = 0;
  if (x > 0) maxN = Math.max(maxN, elevation[y * GRID + x - 1]);
  if (x < GRID - 1) maxN = Math.max(maxN, elevation[y * GRID + x + 1]);
  if (y > 0) maxN = Math.max(maxN, elevation[(y - 1) * GRID + x]);
  if (y < GRID - 1) maxN = Math.max(maxN, elevation[(y + 1) * GRID + x]);
  return Math.max(0, Math.min(2, maxN - 1));
}

// --- Connectivity ---------------------------------------------------------------

// edgeWalkable — faithful port of the server's cliff rule
// (tilemap.EdgeWalkableFor): a step between two tiles is walkable when the
// elevation delta is ≤ 1, OR either endpoint is a Ramp tile (a Ramp permits
// crossing a full 2-tier cliff).
export function edgeWalkable(terrain, elevation, i, j) {
  const d = Math.abs(elevation[i] - elevation[j]);
  if (d <= 1) return true;
  return terrain[i] === RAMP_TERRAIN || terrain[j] === RAMP_TERRAIN;
}

// isConnected — faithful port of server/pkg/tilemap/map_validate.go. 4-dir
// BFS from start to end; a tile is traversable when its profile cost > 0 AND
// the cliff edge rule (edgeWalkable) allows the step. Note (matches server):
// the start tile's own cost is NOT checked — only neighbors — so the editor
// flags exactly what GenerateMap's validation would.
export function isConnected(terrain, elevation, start, end, costs) {
  if (start.x === end.x && start.y === end.y) return true;
  const visited = new Uint8Array(GRID * GRID);
  const queue = [start.y * GRID + start.x];
  visited[queue[0]] = 1;
  const dirs = [1, -1, GRID, -GRID]; // +x, -x, +y, -y (row-major)
  while (queue.length) {
    const cur = queue.shift();
    const cx = cur % GRID, cy = (cur - cx) / GRID;
    for (const d of dirs) {
      const nx = cx + (d === 1 ? 1 : d === -1 ? -1 : 0);
      const ny = cy + (d === GRID ? 1 : d === -GRID ? -1 : 0);
      if (nx < 0 || nx >= GRID || ny < 0 || ny >= GRID) continue;
      const ni = ny * GRID + nx;
      if (visited[ni]) continue;
      if (costs[terrain[ni]] === 0) continue; // impassable
      if (!edgeWalkable(terrain, elevation, cur, ni)) continue; // 2-tier cliff
      if (nx === end.x && ny === end.y) return true;
      visited[ni] = 1;
      queue.push(ni);
    }
  }
  return false;
}
