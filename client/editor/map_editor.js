// Paper War — Clash Map Editor (terrain + elevation authoring)
//
// Browser dev tool for authoring the hand-authored 32×32 Clash Maps in
// server/pkg/tilemap/clash_maps.go (ClashPlains, ClashForest, …) used by the
// spectator/balance harness (start_clash). Read-only against game code: paint
// terrain + hill elevation, export Go source to paste back.
//
// Mirrors the animation + units editors. Two differences from those:
//   - Snapshot loading is live (GET /editor/clash-maps), not a hand-copied
//     JS constant — stays in sync with clash_maps.go automatically.
//   - Spawns and Objective are NOT authored: clash mode overrides them at
//     runtime (main.go:348,377-389), so they'd be inert controls. See ADR-0022.
//
// Pure logic (brushes, fills, connectivity) lives in map_core.mjs — DOM-free
// and node-tested. This file is DOM wiring only.
//
// Open via the Go server's static-file handler:
//   http://localhost:<port>/editor/map.html

import {
  GRID, TERRAIN_NAMES, RESERVED_TERRAIN, TERRAIN_COLORS,
  hillShadeRGB, RUNTIME_SPAWNS, PROFILE_COSTS, RAMP_TERRAIN,
  brushOffsets, rectIndices, floodFill, isConnected, rampElevationAt,
  ZOOM_LEVELS, screenToTile, tileToScreen, zoomAround, clampPan,
} from './map_core.mjs';
import { Renderer } from '../src/gl.js';

// Last connectivity result — { Light: bool, Heavy: bool }. render() reads
// this to ring the spawn markers red when a profile can't connect.
let connState = { Light: true, Heavy: true };

// --- Model -----------------------------------------------------------------
const N = GRID * GRID;
let terrain = new Uint8Array(N);   // all 0 (Plain)
let elevation = new Uint8Array(N); // all 0

let tool = 'terrain';        // WHAT you paint: 'terrain' | 'elevation' | 'ramp'
let mode = 'paint';          // HOW you apply it: 'paint' | 'rect' | 'fill' | 'eyedropper'
let lastMode = 'paint';      // eyedropper returns here after a pick
let brushTerrain = 0;        // active terrain type
let brushElev = 1;           // active elevation layer
let brushSize = 1;           // 1 | 2 | 3 | 5
let brushShape = 'square';   // 'square' | 'circle'
let mirror = true;
let painting = false;        // drag-paint in progress
let rectAnchor = null;       // {x,y} tile anchor while rect-dragging
let hoverTile = null;        // {x,y} tile under the cursor (overlay)
let showGrid = true;         // tile grid overlay toggle (G)

// View transform — px per tile + map-origin offset in canvas pixels. All
// screen↔tile conversions go through screenToTile/tileToScreen (map_core).
let view = { px: 16, ox: 0, oy: 0 };
let panning = false;         // space/middle/right-drag in progress
let panStart = null;         // {sx, sy, ox, oy} at pan begin
let spaceDown = false;

const BRUSH_SIZES = [1, 2, 3, 5];

// --- Undo / redo -----------------------------------------------------------
// Stroke-level history: one gesture (paint drag, rect commit, fill click, one
// Clear, or one Load) = one undo step. Each entry is a full terrain+elevation
// snapshot (2 KB); capped at HIST_MAX entries. Standard semantics — any new
// edit after an undo discards the redo branch.
const HIST_MAX = 50;
let undoStack = [];
let redoStack = [];

function snapshot() {
  return { t: terrain.slice(), e: elevation.slice() };
}
function restore(s) {
  terrain = s.t.slice();
  elevation = s.e.slice();
}
// pushSnapshot records an already-taken pre-edit snapshot (used when we only
// know AFTER mutating whether the gesture changed anything — fill, rect).
function pushSnapshot(s) {
  undoStack.push(s);
  if (undoStack.length > HIST_MAX) undoStack.shift();
  redoStack = [];
  updateUndoButtons();
}
// pushHistory records the CURRENT state BEFORE an impending edit, so undo
// returns to it. Call at the start of a paint stroke / Clear / Load.
function pushHistory() {
  pushSnapshot(snapshot());
}
function undo() {
  if (!undoStack.length) return;
  redoStack.push(snapshot());
  restore(undoStack.pop());
  afterEdit();
  setStatus('Undo.');
}
function redo() {
  if (!redoStack.length) return;
  undoStack.push(snapshot());
  restore(redoStack.pop());
  afterEdit();
  setStatus('Redo.');
}
// Re-render + re-validate after any history restore.
function afterEdit() {
  buildTerrainPalette(); // refresh active-swatch highlight vs restored tile
  buildElevPalette();
  render();
  checkConnectivity();
  updateUndoButtons();
}
function updateUndoButtons() {
  document.getElementById('undo-btn').disabled = undoStack.length === 0;
  document.getElementById('redo-btn').disabled = redoStack.length === 0;
}

const TILE = 16;             // base px-per-tile; actual scale is view.px

// --- DOM -------------------------------------------------------------------
const canvas = document.getElementById('canvas');
const ctx = canvas.getContext('2d');
const overlay = document.getElementById('overlay');
const octx = overlay.getContext('2d');
const statusEl = document.getElementById('status');
const minimap = document.getElementById('minimap');
const mctx = minimap ? minimap.getContext('2d') : null;

// --- Art preview (WebGL) -----------------------------------------------------
// Dual-view: the 2D canvas is the EDIT surface (flat colors, unambiguous tile
// identity, overlay stack). The GL canvas is the ART preview — the game's
// actual Renderer, sharing the same zoom/pan view. Tab toggles.
// NOTE: headless test browsers lack WebGL2; degrade to edit-only there.
const glCanvas = document.getElementById('gl-canvas');
let renderer = null;
let artMode = false;
try {
  if (glCanvas) {
    renderer = new Renderer(glCanvas);
    renderer.resize();
  }
} catch (err) {
  console.info('map editor: WebGL2 unavailable — art preview disabled (' + err.message + ')');
  renderer = null;
}

// uploadMapTextures pushes the terrain/elevation grids to the R8UI sampler
// textures the terrain shader texelFetches (same upload path as the game).
function uploadMapTextures() {
  if (!renderer) return;
  renderer.setMapTerrainTexture(terrain, GRID, GRID);
  renderer.setMapElevationTexture(elevation, GRID, GRID);
}

// renderGL drives the game Renderer for the whole 32×32 map under the shared
// view transform. Descriptors mirror main.js buildTerrainTiles (simplified —
// base palette color, patchwork brightness, hillShade layer tint, seed hash).
const PATCHWORK_TERRAINS = new Set([0, 4, 6, 10, 17]);
let glTiles = [];
function renderGL() {
  if (!renderer || !artMode) return;
  // World units: 1 tile = 1 unit, scaled by view.px (screen px per tile).
  // The shader's u_tileSize must equal the on-screen tile size in CSS px so
  // texelFetch lands on the right grid cell.
  const px = view.px;
  const camera = { x: -view.ox, y: -view.oy };
  glTiles.length = 0;
  for (let ty = 0; ty < GRID; ty++) {
    for (let tx = 0; tx < GRID; tx++) {
      const idx = ty * GRID + tx;
      const t = terrain[idx];
      const col = TERRAIN_COLORS[t] || TERRAIN_COLORS[0];
      let [r, g, b] = col;
      const seed = (((tx * 374761393 + ty * 668265263) >>> 0) % 1000) / 100;
      if (PATCHWORK_TERRAINS.has(t)) {
        const px1 = Math.floor(tx / 3), py1 = Math.floor(ty / 3);
        const px2 = Math.floor(tx / 5 + 1), py2 = Math.floor(ty / 4 + 2);
        const h1 = (((px1 * 374761393 + py1 * 668265263) >>> 0) % 1000) / 1000;
        const h2 = (((px2 * 2246822519 + py2 * 3266489917) >>> 0) % 1000) / 1000;
        const brightness = 0.74 + (h1 + h2) * 0.24;
        r *= brightness; g *= brightness;
        b *= brightness + Math.max(0, brightness - 1.0) * 0.8;
      }
      const layer = elevation[idx];
      if (layer !== 1) [r, g, b] = hillShadeRGB(r, g, b, layer);
      glTiles.push({
        x: tx * px, y: ty * px, w: px, h: px,
        r, g, b,
        tileType: t || 1, // 0 takes the flat-color path; default to textured Plain
        seed,
      });
    }
  }
  renderer.beginFrame();
  renderer.drawTerrain(glTiles, camera);
  renderer.endFrame({
    cameraX: camera.x,
    cameraY: camera.y,
    tileSize: px,
  });
}

// setArt toggles Edit ⇄ Art. The GL canvas overlays the 2D canvas; the
// overlay (brush cursor) stays visible in both — it is position-accurate
// because both canvases share the view transform.
function setArt(on) {
  artMode = on && !!renderer;
  if (glCanvas) glCanvas.classList.toggle('hidden', !artMode);
  const btn = document.getElementById('art-btn');
  if (btn) {
    btn.classList.toggle('active', artMode);
    btn.disabled = !renderer;
  }
  if (artMode) {
    renderer.resize(); // canvas was display:none before — layout size was 0
    uploadMapTextures();
    renderGL();
  }
}
const artBtn = document.getElementById('art-btn');
if (artBtn) artBtn.onclick = () => setArt(!artMode);

// --- Rendering -------------------------------------------------------------
function tileColor(t, elev) {
  const [r, g, b] = TERRAIN_COLORS[t] || TERRAIN_COLORS[0];
  if (t === 5 || elev > 0) return hillShadeRGB(r, g, b, elev); // shade by layer
  return [r, g, b];
}
function toCss([r, g, b]) {
  return `rgb(${Math.round(r * 255)},${Math.round(g * 255)},${Math.round(b * 255)})`;
}

// checkConnectivity runs both movement profiles between the runtime spawns
// and updates the live status badge. Called after every paint/load/clear.
// Cliff-aware: a 2-tier elevation step is impassable unless a Ramp sits on
// either end (edgeWalkable — the server's EdgeWalkableFor rule).
function checkConnectivity() {
  const [a, b] = RUNTIME_SPAWNS;
  connState = {
    Light: isConnected(terrain, elevation, a, b, PROFILE_COSTS.Light),
    Heavy: isConnected(terrain, elevation, a, b, PROFILE_COSTS.Heavy),
  };
  const el = document.getElementById('conn-status');
  if (connState.Light && connState.Heavy) {
    el.innerHTML = 'connectivity: <span class="ok">both profiles OK</span>';
  } else {
    const failed = [
      !connState.Light ? 'Light' : null,
      !connState.Heavy ? 'Heavy' : null,
    ].filter(Boolean).join(' + ');
    el.innerHTML = `connectivity: <span class="warn">${failed} stranded ⚠</span>`;
  }
}

// viewApply re-renders everything that depends on the view transform.
function viewApply() {
  view = clampPan(view, canvas.width, canvas.height);
  render();
  renderOverlay();
  renderMinimap();
  renderGL();
}

// setZoom steps between ZOOM_LEVELS, anchored on a screen point.
function setZoom(nextPx, sx, sy) {
  view = zoomAround(sx, sy, nextPx, view);
  viewApply();
}
function cycleZoom(dir) {
  const idx = ZOOM_LEVELS.indexOf(view.px);
  const next = ZOOM_LEVELS[Math.min(ZOOM_LEVELS.length - 1, Math.max(0, idx + dir))];
  setZoom(next, canvas.width / 2, canvas.height / 2);
}

function render() {
  ctx.fillStyle = '#0d0d0d';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const px = view.px;
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      const i = y * GRID + x;
      const s = tileToScreen(x, y, view);
      ctx.fillStyle = toCss(tileColor(terrain[i], elevation[i]));
      ctx.fillRect(s.x, s.y, px, px);
    }
  }
  // Faint grid lines so individual tiles are legible while painting. Skipped
  // below 8px/tile where it would read as noise.
  if (showGrid && px >= 8) {
    ctx.strokeStyle = 'rgba(0,0,0,0.25)';
    ctx.lineWidth = 1;
    for (let i = 0; i <= GRID; i++) {
      const gx = view.ox + i * px;
      const gy = view.oy + i * px;
      if (gx >= 0 && gx <= canvas.width) {
        ctx.beginPath(); ctx.moveTo(gx + 0.5, Math.max(0, view.oy)); ctx.lineTo(gx + 0.5, Math.min(canvas.height, view.oy + GRID * px)); ctx.stroke();
      }
      if (gy >= 0 && gy <= canvas.height) {
        ctx.beginPath(); ctx.moveTo(Math.max(0, view.ox), gy + 0.5); ctx.lineTo(Math.min(canvas.width, view.ox + GRID * px), gy + 0.5); ctx.stroke();
      }
    }
  }
  // Runtime spawn markers — squads appear here regardless of authored terrain.
  // Ringed red when a movement profile can't connect them (stranded spawn).
  const stranded = !(connState.Light && connState.Heavy);
  for (const s of RUNTIME_SPAWNS) {
    const c = tileToScreen(s.x, s.y, view);
    const cx = c.x + px / 2, cy = c.y + px / 2;
    ctx.fillStyle = s.color;
    ctx.beginPath();
    ctx.arc(cx, cy, px * 0.35, 0, Math.PI * 2);
    ctx.fill();
    if (stranded) {
      ctx.strokeStyle = '#c8503c';
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(cx, cy, px * 0.7, 0, Math.PI * 2);
      ctx.stroke();
    }
  }
}

// Minimap: whole-map thumbnail + viewport rectangle. Click/drag pans.
function renderMinimap() {
  if (!mctx) return;
  const w = minimap.width, h = minimap.height;
  const cell = w / GRID;
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      const i = y * GRID + x;
      mctx.fillStyle = toCss(tileColor(terrain[i], elevation[i]));
      mctx.fillRect(x * cell, y * cell, cell + 0.5, cell + 0.5);
    }
  }
  // Viewport rect in minimap coordinates.
  mctx.strokeStyle = '#fff';
  mctx.lineWidth = 1;
  const vx = (-view.ox / (GRID * view.px)) * w;
  const vy = (-view.oy / (GRID * view.px)) * h;
  const vw = (canvas.width / (GRID * view.px)) * w;
  const vh = (canvas.height / (GRID * view.px)) * h;
  mctx.strokeRect(vx + 0.5, vy + 0.5, vw, vh);
}

// Overlay: brush cursor footprint + rect-drag ghost. Redrawn on hover/mode
// change; separate canvas so the map itself never flickers.
function renderOverlay() {
  octx.clearRect(0, 0, overlay.width, overlay.height);
  if (!hoverTile) return;
  const px = view.px;
  if (mode === 'rect' && rectAnchor) {
    const xs = Math.min(rectAnchor.x, hoverTile.x), xe = Math.max(rectAnchor.x, hoverTile.x);
    const ys = Math.min(rectAnchor.y, hoverTile.y), ye = Math.max(rectAnchor.y, hoverTile.y);
    const a = tileToScreen(xs, ys, view);
    octx.fillStyle = 'rgba(255,255,255,0.15)';
    octx.strokeStyle = 'rgba(255,255,255,0.8)';
    octx.lineWidth = 1;
    octx.fillRect(a.x, a.y, (xe - xs + 1) * px, (ye - ys + 1) * px);
    octx.strokeRect(a.x + 0.5, a.y + 0.5, (xe - xs + 1) * px - 1, (ye - ys + 1) * px - 1);
    return;
  }
  if (mode !== 'paint') return;
  // Brush footprint. Even sizes anchor at the footprint's top-left, so the
  // preview uses the same offsets the brush does.
  octx.fillStyle = 'rgba(255,255,255,0.18)';
  octx.strokeStyle = 'rgba(255,255,255,0.7)';
  octx.lineWidth = 1;
  for (const { dx, dy } of brushOffsets(brushSize, brushShape)) {
    const x = hoverTile.x + dx, y = hoverTile.y + dy;
    if (x < 0 || x >= GRID || y < 0 || y >= GRID) continue;
    const s = tileToScreen(x, y, view);
    octx.fillRect(s.x, s.y, px, px);
    octx.strokeRect(s.x + 0.5, s.y + 0.5, px - 1, px - 1);
  }
}

// --- Painting --------------------------------------------------------------
function mirrorX(x) { return GRID - 1 - x; }

// applyPayload writes the active tool's value at one tile. Returns whether
// the grid actually changed (no-op gestures don't get undo entries).
function applyPayload(i) {
  if (tool === 'terrain') {
    if (terrain[i] === brushTerrain) return false;
    terrain[i] = brushTerrain;
    return true;
  }
  if (tool === 'ramp') {
    // Ramp tiles carry auto-graded elevation: one tier below the highest
    // 4-neighbour tier (map_core.rampElevationAt). The elevation is computed
    // from the grid as-it-stands at this tile's write time — during a drag
    // that includes previously painted ramp tiles, so a stroke crossing a
    // multi-tier cliff grades itself stepwise.
    const x = i % GRID, y = (i - (i % GRID)) / GRID;
    const elev = rampElevationAt(elevation, x, y);
    if (terrain[i] === RAMP_TERRAIN && elevation[i] === elev) return false;
    terrain[i] = RAMP_TERRAIN;
    elevation[i] = elev;
    return true;
  }
  // Elevation tool: paints the tier on any terrain (ADR-0033 era — the JSON
  // wire format carries the full elevation grid).
  if (elevation[i] === brushElev) return false;
  elevation[i] = brushElev;
  return true;
}

function applyBrushAt(x, y) {
  for (const { dx, dy } of brushOffsets(brushSize, brushShape)) {
    const bx = x + dx, by = y + dy;
    if (bx < 0 || bx >= GRID || by < 0 || by >= GRID) continue;
    applyPayload(by * GRID + bx);
  }
}

// paintStroke applies one brush stamp (+mirror) and refreshes.
function paintStroke(x, y) {
  applyBrushAt(x, y);
  if (mirror) applyBrushAt(mirrorX(x), y);
  render();
  checkConnectivity();
}

// applyRect commits a finished rect drag (+mirror). Returns changed count.
function applyRect(x0, y0, x1, y1) {
  let changed = 0;
  for (const i of rectIndices(x0, y0, x1, y1)) {
    const x = i % GRID, y = (i - x) / GRID;
    if (applyPayload(i)) changed++;
    if (mirror && applyPayload(y * GRID + mirrorX(x))) changed++;
  }
  return changed;
}

// fillClick floods the 4-connected region (+mirror origin). Returns total
// changed count (0 when both fills were no-ops).
function fillClick(x, y) {
  const payload = tool === 'terrain' ? { terrain: brushTerrain } : { elev: brushElev };
  let n = floodFill(terrain, elevation, x, y, payload);
  if (mirror) n += floodFill(terrain, elevation, mirrorX(x), y, payload);
  return n;
}

// Note: the ramp tool uses paint mode only (rect/fill would make grading
// ill-defined); gestureDown routes it through paintStroke.

// eyedrop picks terrain + elevation from a tile and returns to the previous
// mode. No history — it changes brush settings, not the grid.
function eyedrop(x, y) {
  const i = y * GRID + x;
  brushTerrain = terrain[i];
  brushElev = elevation[i];
  setMode(lastMode);
  buildTerrainPalette();
  buildElevPalette();
  setStatus(`Picked ${TERRAIN_NAMES[brushTerrain]} (elev ${brushElev}).`);
}

function edited() {
  render();
  checkConnectivity();
  if (artMode) { uploadMapTextures(); renderGL(); }
}

// --- Pointer dispatch --------------------------------------------------------
// One funnel for mouse + touch: all gestures arrive as tile coordinates.
function gestureDown(x, y, altKey) {
  if (x < 0 || x >= GRID || y < 0 || y >= GRID) return;
  if (altKey || mode === 'eyedropper') { eyedrop(x, y); return; }
  if (mode === 'fill') {
    const s = snapshot();
    const n = fillClick(x, y);
    if (n > 0) { pushSnapshot(s); edited(); setStatus(`Filled ${n} tiles.`); }
    else setStatus('Fill: nothing to change.');
    return;
  }
  if (mode === 'rect') {
    rectAnchor = { x, y };
    renderOverlay();
    return; // committed on up
  }
  // paint
  pushHistory();            // one undo step per stroke (down→up)
  painting = true;
  paintStroke(x, y);
}
function gestureMove(x, y) {
  hoverTile = (x >= 0 && x < GRID && y >= 0 && y < GRID) ? { x, y } : null;
  if (painting) paintStroke(x, y);
  renderOverlay();
}
function gestureUp() {
  painting = false;
  if (mode === 'rect' && rectAnchor && hoverTile) {
    const s = snapshot();
    const n = applyRect(rectAnchor.x, rectAnchor.y, hoverTile.x, hoverTile.y);
    rectAnchor = null;
    if (n > 0) { pushSnapshot(s); edited(); setStatus(`Rect: ${n} tiles changed.`); }
    else renderOverlay();
  }
}

function eventTile(e) {
  // offsetX/offsetY are relative to the padding box — they exclude the CSS
  // border, and with no CSS sizing they ARE backing-store pixels. (Dividing
  // by getBoundingClientRect().width instead shifts clicks by the border at
  // high zoom — a real off-by-one-tile bug.)
  return screenToTile(e.offsetX, e.offsetY, view);
}

function eventScreen(e) {
  return { x: e.offsetX, y: e.offsetY };
}

// Pan interception: space-drag, middle button, or right button.
function isPanGesture(e) {
  return spaceDown || e.button === 1 || e.button === 2;
}

canvas.addEventListener('contextmenu', (e) => e.preventDefault());
canvas.addEventListener('mousedown', (e) => {
  if (isPanGesture(e)) {
    panning = true;
    const s = eventScreen(e);
    panStart = { sx: s.x, sy: s.y, ox: view.ox, oy: view.oy };
    e.preventDefault();
    return;
  }
  const { x, y } = eventTile(e);
  gestureDown(x, y, e.altKey);
});
window.addEventListener('mouseup', (e) => {
  if (panning) { panning = false; return; }
  gestureUp();
});
canvas.addEventListener('mousemove', (e) => {
  if (panning && panStart) {
    const s = eventScreen(e);
    view = clampPan({ px: view.px, ox: panStart.ox + (s.x - panStart.sx), oy: panStart.oy + (s.y - panStart.sy) }, canvas.width, canvas.height);
    render(); renderOverlay(); renderMinimap(); renderGL();
    return;
  }
  const t = eventTile(e);
  gestureMove(t.x, t.y);
});
canvas.addEventListener('mouseleave', () => { hoverTile = null; renderOverlay(); });
canvas.addEventListener('wheel', (e) => {
  e.preventDefault();
  const dir = e.deltaY < 0 ? 1 : -1;
  const idx = ZOOM_LEVELS.indexOf(view.px);
  const next = ZOOM_LEVELS[Math.min(ZOOM_LEVELS.length - 1, Math.max(0, idx + dir))];
  const s = eventScreen(e);
  setZoom(next, s.x, s.y);
}, { passive: false });
// Touch support — drag-paint on trackpads/tablets.
canvas.addEventListener('touchstart', (e) => {
  e.preventDefault();
  const t = e.touches[0];
  const { x, y } = eventTile(t);
  gestureDown(x, y, false);
}, { passive: false });
canvas.addEventListener('touchmove', (e) => {
  e.preventDefault();
  const t = e.touches[0];
  const { x, y } = eventTile(t);
  gestureMove(x, y);
}, { passive: false });
canvas.addEventListener('touchend', () => gestureUp());

// Minimap click/drag = pan jump (center the viewport on the clicked tile).
function minimapPan(e) {
  const r = minimap.getBoundingClientRect();
  const mx = (e.clientX - r.left) * (minimap.width / r.width);
  const my = (e.clientY - r.top) * (minimap.height / r.height);
  const tx = (mx / minimap.width) * GRID;
  const ty = (my / minimap.height) * GRID;
  const s = tileToScreen(Math.floor(tx), Math.floor(ty), view);
  view = clampPan({ px: view.px, ox: canvas.width / 2 - s.x, oy: canvas.height / 2 - s.y }, canvas.width, canvas.height);
  render(); renderOverlay(); renderMinimap(); renderGL();
}
if (minimap) {
  let mmDragging = false;
  minimap.addEventListener('mousedown', (e) => { mmDragging = true; minimapPan(e); });
  window.addEventListener('mousemove', (e) => { if (mmDragging) minimapPan(e); });
  window.addEventListener('mouseup', () => { mmDragging = false; });
}

// Space-hold pans (SC convention). Track via keydown/keyup on window.
window.addEventListener('keydown', (e) => {
  if (e.code === 'Space') {
    const tag = (document.activeElement && document.activeElement.tagName) || '';
    if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT') {
      spaceDown = true;
      canvas.style.cursor = 'grab';
    }
  }
});
window.addEventListener('keyup', (e) => {
  if (e.code === 'Space') {
    spaceDown = false;
    canvas.style.cursor = mode === 'eyedropper' ? 'copy' : 'crosshair';
  }
});

// --- Palettes --------------------------------------------------------------
function buildTerrainPalette() {
  const el = document.getElementById('terrain-palette');
  el.innerHTML = '';
  TERRAIN_NAMES.forEach((name, t) => {
    if (RESERVED_TERRAIN.has(t)) return; // skip retired stronghold ids (#54)
    const sw = document.createElement('div');
    sw.className = 'swatch' + (t === brushTerrain ? ' active' : '');
    sw.innerHTML = `<div class="chip" style="background:${toCss(TERRAIN_COLORS[t])}"></div>` +
                   `<div class="lbl">${name}</div>`;
    sw.onclick = () => {
      brushTerrain = t;
      tool = 'terrain';
      setActiveTool('terrain');
      buildTerrainPalette();
    };
    el.appendChild(sw);
  });
}

function buildElevPalette() {
  const el = document.getElementById('elev-palette');
  el.innerHTML = '';
  // Discrete 3-layer model from generate.go: 0 low (valley shadow), 1 mid, 2 peak.
  const opts = [
    { v: 0, name: 'Low (0)' },
    { v: 1, name: 'Mid (1)' },
    { v: 2, name: 'Peak (2)' },
  ];
  opts.forEach((o) => {
    const sw = document.createElement('div');
    sw.className = 'swatch' + (o.v === brushElev ? ' active' : '');
    const chipColor = toCss(hillShadeRGB(...TERRAIN_COLORS[5], o.v));
    sw.innerHTML = `<div class="chip" style="background:${chipColor}"></div><div class="lbl">${o.name}</div>`;
    sw.onclick = () => { brushElev = o.v; buildElevPalette(); };
    el.appendChild(sw);
  });
}

function setActiveTool(t) {
  tool = t;
  document.getElementById('tool-terrain').classList.toggle('active', t === 'terrain');
  document.getElementById('tool-elevation').classList.toggle('active', t === 'elevation');
  document.getElementById('tool-ramp').classList.toggle('active', t === 'ramp');
  const terrainOn = t === 'terrain';
  const rampOn = t === 'ramp';
  document.getElementById('terrain-palette').classList.toggle('hidden', !terrainOn);
  document.getElementById('elev-palette').classList.toggle('hidden', terrainOn || rampOn);
  document.getElementById('elev-info').classList.toggle('hidden', terrainOn || rampOn);
  document.getElementById('ramp-info').classList.toggle('hidden', !rampOn);
  document.getElementById('palette-title').textContent =
    terrainOn ? 'Terrain palette' : rampOn ? 'Ramp' : 'Elevation (layers, any terrain)';
}

function setMode(m) {
  if (m !== 'eyedropper') lastMode = m;
  mode = m;
  for (const k of ['paint', 'rect', 'fill', 'eyedropper']) {
    document.getElementById(`mode-${k}`).classList.toggle('active', m === k);
  }
  canvas.style.cursor = m === 'eyedropper' ? 'copy' : 'crosshair';
  rectAnchor = null;
  renderOverlay();
}

document.getElementById('tool-terrain').onclick = () => setActiveTool('terrain');
document.getElementById('tool-elevation').onclick = () => setActiveTool('elevation');
document.getElementById('tool-ramp').onclick = () => { setActiveTool('ramp'); setMode('paint'); };
for (const k of ['paint', 'rect', 'fill', 'eyedropper']) {
  document.getElementById(`mode-${k}`).onclick = () => setMode(k);
}

// Brush size / shape selectors.
function cycleBrushSize(dir) {
  const idx = BRUSH_SIZES.indexOf(brushSize);
  const next = BRUSH_SIZES[Math.min(BRUSH_SIZES.length - 1, Math.max(0, idx + dir))];
  setBrushSize(next);
}
function setBrushSize(s) {
  brushSize = s;
  document.getElementById('brush-size').value = String(s);
  renderOverlay();
}
document.getElementById('brush-size').onchange = (e) => {
  brushSize = parseInt(e.target.value, 10);
  renderOverlay();
};
document.getElementById('brush-shape').onchange = (e) => {
  brushShape = e.target.value;
  renderOverlay();
};

document.getElementById('mirror-btn').onclick = (e) => {
  mirror = !mirror;
  e.target.classList.toggle('active', mirror);
  e.target.textContent = mirror ? 'On' : 'Off';
};

// Undo / redo buttons + keyboard. Ignore shortcuts when focus is in a text
// input so typing there isn't hijacked.
document.getElementById('undo-btn').onclick = undo;
document.getElementById('redo-btn').onclick = redo;
window.addEventListener('keydown', (e) => {
  const tag = (document.activeElement && document.activeElement.tagName) || '';
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key.toLowerCase() === 'z') {
    e.preventDefault();
    if (e.shiftKey) redo(); else undo();
    return;
  }
  if (e.key === '[') { e.preventDefault(); cycleBrushSize(-1); return; }
  if (e.key === ']') { e.preventDefault(); cycleBrushSize(1); return; }
  if (e.key.toLowerCase() === 'g' && !mod) { showGrid = !showGrid; render(); return; }
  if ((e.key === '+' || e.key === '=') && !mod) { cycleZoom(1); return; }
  if (e.key === '-' && !mod) { cycleZoom(-1); return; }
  if (e.key === 'Tab') {
    e.preventDefault();
    setArt(!artMode);
    return;
  }
  const modeKeys = { b: 'paint', r: 'rect', f: 'fill', i: 'eyedropper' };
  const mk = modeKeys[e.key.toLowerCase()];
  if (mk && !mod) { setMode(mk); return; }
  const toolKeys = { t: 'terrain', e: 'elevation', a: 'ramp' };
  const tk = toolKeys[e.key.toLowerCase()];
  if (tk && !mod) {
    setActiveTool(tk);
    if (tk === 'ramp') setMode('paint');
  }
});

// Zoom + grid buttons.
const zoomInBtn = document.getElementById('zoom-in-btn');
const zoomOutBtn = document.getElementById('zoom-out-btn');
if (zoomInBtn) zoomInBtn.onclick = () => cycleZoom(1);
if (zoomOutBtn) zoomOutBtn.onclick = () => cycleZoom(-1);
const gridBtn = document.getElementById('grid-btn');
if (gridBtn) gridBtn.onclick = (e) => {
  showGrid = !showGrid;
  e.target.classList.toggle('active', showGrid);
  render();
};

document.getElementById('clear-btn').onclick = () => {
  pushHistory();
  terrain = new Uint8Array(N);
  elevation = new Uint8Array(N);
  edited();
  updateUndoButtons();
  setStatus('Cleared.');
};

// --- Snapshot loading ------------------------------------------------------
async function loadSnapshots() {
  const sel = document.getElementById('snapshot-select');
  try {
    const res = await fetch('/editor/clash-maps');
    const data = await res.json();
    for (const name of Object.keys(data)) {
      const opt = document.createElement('option');
      opt.value = name;
      opt.textContent = name;
      sel.appendChild(opt);
    }
    snapshots = data;
  } catch (err) {
    setStatus('Could not load snapshots: ' + err.message, true);
  }
}
let snapshots = {};

document.getElementById('load-btn').onclick = () => {
  pushHistory();            // loading replaces the canvas — make it undoable
  const name = document.getElementById('snapshot-select').value;
  if (!name || !snapshots[name]) {
    terrain = new Uint8Array(N);
    elevation = new Uint8Array(N);
    edited();
    setStatus('Blank canvas.');
    return;
  }
  const s = snapshots[name];
  // Snapshots are server-sized (always 32×32 for clash maps). Copy by index.
  for (let i = 0; i < N && i < s.terrain.length; i++) {
    terrain[i] = s.terrain[i];
    elevation[i] = s.elevation[i];
  }
  document.getElementById('map-name').value = capitalize(name);
  edited();
  if (artMode) uploadMapTextures();
  updateUndoButtons();
  setStatus(`Loaded "${name}".`);
};

function capitalize(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

// --- Save / export -----------------------------------------------------------
// ADR-0033: clash maps are JSON data files (server/data/clash_maps/<name>.json).
// The editor POSTs to /editor/clash-maps/save; the map is loadable by the next
// start_clash immediately — no rebuild, no restart, no Go source to paste.

// mapSlug normalizes the map-name field into a saveable slug ([a-z0-9_]{1,32}).
function mapSlug() {
  const raw = (document.getElementById('map-name').value || 'custom')
    .toLowerCase().replace(/[^a-z0-9_]/g, '_').replace(/^_+|_+$/g, '');
  return (raw || 'custom').slice(0, 32);
}

async function doSave() {
  const name = mapSlug();
  try {
    const res = await fetch('/editor/clash-maps/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name,
        w: GRID,
        h: GRID,
        terrain: Array.from(terrain),
        elevation: Array.from(elevation),
      }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || res.statusText);
    document.getElementById('map-name').value = name;
    setStatus(`Saved "${name}" — loadable via start_clash immediately.`);
    await refreshSnapshotList();
  } catch (err) {
    setStatus(`Save failed: ${err.message}`, true);
  }
}

function doDownloadJson() {
  const name = mapSlug();
  const blob = new Blob([JSON.stringify({
    name, w: GRID, h: GRID, terrain: Array.from(terrain), elevation: Array.from(elevation),
  }, null, 1)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${name}.json`;
  a.click();
  URL.revokeObjectURL(a.href);
  setStatus(`Downloaded ${name}.json (backup copy).`);
}

// refreshSnapshotList re-fetches /editor/clash-maps so a just-saved map
// appears in the Load dropdown without a page reload.
async function refreshSnapshotList() {
  const sel = document.getElementById('snapshot-select');
  sel.innerHTML = '<option value="">(blank 32×32)</option>';
  await loadSnapshots();
}

function setStatus(msg, bad = false) {
  statusEl.textContent = msg;
  statusEl.className = 'info' + (bad ? ' warn' : '');
}

// Gate save when connectivity is failing — confirm before shipping a map
// whose runtime spawns can't reach each other (one side would be unwinnable).
// "Save anyway" is offered because there are legitimate reasons to save a
// knowingly-broken map (debugging pathfinding, testing stranded-spawn logic).
function maybeSave(action) {
  if (connState.Light && connState.Heavy) {
    action();
    return;
  }
  const failed = [
    !connState.Light ? 'Light' : null,
    !connState.Heavy ? 'Heavy' : null,
  ].filter(Boolean).join(' + ');
  if (confirm(
    `Connectivity check FAILED for ${failed} profile(s) between the runtime ` +
    `spawns (16,12) and (16,20). One side may be unwinnable.\n\nSave anyway?`
  )) {
    action();
  }
}

document.getElementById('save-btn').onclick = () => maybeSave(doSave);
document.getElementById('download-btn').onclick = () => doDownloadJson();

// --- Init ------------------------------------------------------------------
buildTerrainPalette();
buildElevPalette();
setActiveTool('terrain');
setMode('paint');
if (renderer) { renderer.resize(); uploadMapTextures(); }
setArt(false); // initialize the Art button state (disabled without WebGL2)
viewApply();
checkConnectivity();
updateUndoButtons();
loadSnapshots();
