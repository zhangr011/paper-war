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
// Open via the Go server's static-file handler:
//   http://localhost:<port>/editor/map.html

// --- Constants (mirror server/pkg/component/movement.go) -------------------
// Keep in sync with the Go source. TerrainType ids line up with TERRAIN_COLORS.
const GRID = 32;
const TERRAIN_NAMES = [
  'Plain', 'Road', 'Shallow', 'Deep', 'Forest', 'Hill', 'Swamp', 'Bridge',
  'Wall', 'Snow', 'Desert',
  '(reserved)', '(reserved)', '(reserved)', '(reserved)', '(reserved)', // 11-15 retired stronghold terrain (#54)
  'Rock', 'Brush',
];
// Go identifier for each terrain type, as used by clash_maps.go SetTerrain.
// Indices must equal terrain ids, so retired stronghold slots (11-15) are kept
// as empty placeholders — never painted, never exported.
const TERRAIN_GO = [
  'component.TerrainPlain', 'component.TerrainRoad', 'component.TerrainShallow',
  'component.TerrainDeep', 'component.TerrainForest', 'component.TerrainHill',
  'component.TerrainSwamp', 'component.TerrainBridge', 'component.TerrainWall',
  'component.TerrainSnow', 'component.TerrainDesert',
  '', '', '', '', '', // 11-15 reserved (stronghold → entity, #54)
  'component.TerrainRock', 'component.TerrainBrush',
];
// Terrain ids retired from the enum (stronghold moved to entity, #54). Skipped
// in the palette and in export.
const RESERVED_TERRAIN = new Set([11, 12, 13, 14, 15]);

// Terrain type colors — copied verbatim from client/src/main.js (TERRAIN_COLORS).
// Histogram-tuned to the dark earthy pixel-art palette of design/map.png.
const TERRAIN_COLORS = [
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
];

// hillShadeRGB — copied verbatim from client/src/main.js. Layer 0 = valley
// shadow, 1 = mid (unchanged), 2 = stone-gray peak. Applied to Hill tiles in
// the preview so elevation reads. Returns [r,g,b] in 0..1.
function hillShadeRGB(r, g, b, layer) {
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
const RUNTIME_SPAWNS = [
  { x: 16, y: 12, color: '#6fa3d6' },
  { x: 16, y: 20, color: '#d6a36f' },
];

// Movement-profile terrain costs — copied verbatim from
// server/pkg/component/profiles.go (StandardMovementProfiles). cost 0 =
// impassable. Used by the live connectivity check (mirrors isConnected in
// map_validate.go) so the editor flags a clash map whose runtime spawns
// can't reach each other before you export it.
const PROFILE_COSTS = {
  Light: [1,1,2,0,2,3,3,1,0,2,2,1,1,1,1,1],
  Heavy: [1,1,0,0,3,4,4,1,0,3,2,1,1,1,1,1],
};

// Last connectivity result — { Light: bool, Heavy: bool }. render() reads
// this to ring the spawn markers red when a profile can't connect.
let connState = { Light: true, Heavy: true };

// --- Model -----------------------------------------------------------------
const N = GRID * GRID;
let terrain = new Uint8Array(N);   // all 0 (Plain)
let elevation = new Uint8Array(N); // all 0

let tool = 'terrain';        // 'terrain' | 'elevation'
let brushTerrain = 0;        // active terrain type
let brushElev = 1;           // active elevation layer
let mirror = true;
let painting = false;

// --- Undo / redo -----------------------------------------------------------
// Stroke-level history: one mousedown→mouseup drag, one Clear, or one Load
// = one undo step. Each entry is a full terrain+elevation snapshot (2 KB);
// capped at HIST_MAX entries. Standard semantics — any new edit after an
// undo discards the redo branch.
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
// pushHistory records the CURRENT state BEFORE an impending edit, so undo
// returns to it. Call at the start of a stroke / Clear / Load.
function pushHistory() {
  undoStack.push(snapshot());
  if (undoStack.length > HIST_MAX) undoStack.shift();
  redoStack = [];
  updateUndoButtons();
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
  render();
  checkConnectivity();
  updateUndoButtons();
}
function updateUndoButtons() {
  document.getElementById('undo-btn').disabled = undoStack.length === 0;
  document.getElementById('redo-btn').disabled = redoStack.length === 0;
}

const TILE = 16;             // canvas is 512×512, 16px per tile

// --- DOM -------------------------------------------------------------------
const canvas = document.getElementById('canvas');
const ctx = canvas.getContext('2d');
const statusEl = document.getElementById('status');

// --- Rendering -------------------------------------------------------------
function tileColor(t, elev) {
  const [r, g, b] = TERRAIN_COLORS[t] || TERRAIN_COLORS[0];
  if (t === 5) return hillShadeRGB(r, g, b, elev); // Hill: shade by layer
  return [r, g, b];
}
function toCss([r, g, b]) {
  return `rgb(${Math.round(r * 255)},${Math.round(g * 255)},${Math.round(b * 255)})`;
}

// isConnected — faithful port of server/pkg/tilemap/map_validate.go. 4-dir
// BFS from start to end; a tile is traversable when its profile cost > 0.
// Note (matches server): the start tile's own cost is NOT checked — only
// neighbors — so the editor flags exactly what GenerateMap's validation would.
function isConnected(start, end, costs) {
  if (start.x === end.x && start.y === end.y) return true;
  const visited = new Uint8Array(N);
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
      if (nx === end.x && ny === end.y) return true;
      visited[ni] = 1;
      queue.push(ni);
    }
  }
  return false;
}

// checkConnectivity runs both movement profiles between the runtime spawns
// and updates the live status badge. Called after every paint/load/clear.
function checkConnectivity() {
  const [a, b] = RUNTIME_SPAWNS;
  connState = {
    Light: isConnected(a, b, PROFILE_COSTS.Light),
    Heavy: isConnected(a, b, PROFILE_COSTS.Heavy),
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

function render() {
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      const i = y * GRID + x;
      ctx.fillStyle = toCss(tileColor(terrain[i], elevation[i]));
      ctx.fillRect(x * TILE, y * TILE, TILE, TILE);
    }
  }
  // Faint grid lines so individual tiles are legible while painting.
  ctx.strokeStyle = 'rgba(0,0,0,0.25)';
  ctx.lineWidth = 1;
  for (let i = 0; i <= GRID; i++) {
    ctx.beginPath(); ctx.moveTo(i * TILE + 0.5, 0); ctx.lineTo(i * TILE + 0.5, GRID * TILE); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(0, i * TILE + 0.5); ctx.lineTo(GRID * TILE, i * TILE + 0.5); ctx.stroke();
  }
  // Runtime spawn markers — squads appear here regardless of authored terrain.
  // Ringed red when a movement profile can't connect them (stranded spawn).
  const stranded = !(connState.Light && connState.Heavy);
  for (const s of RUNTIME_SPAWNS) {
    ctx.fillStyle = s.color;
    ctx.beginPath();
    ctx.arc(s.x * TILE + TILE / 2, s.y * TILE + TILE / 2, TILE * 0.35, 0, Math.PI * 2);
    ctx.fill();
    if (stranded) {
      ctx.strokeStyle = '#c8503c';
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(s.x * TILE + TILE / 2, s.y * TILE + TILE / 2, TILE * 0.7, 0, Math.PI * 2);
      ctx.stroke();
    }
  }
}

// --- Painting --------------------------------------------------------------
function mirrorX(x) { return GRID - 1 - x; }

function paintAt(px, py) {
  const x = Math.floor(px / TILE);
  const y = Math.floor(py / TILE);
  if (x < 0 || x >= GRID || y < 0 || y >= GRID) return;
  applyBrush(x, y);
  if (mirror) applyBrush(mirrorX(x), y);
  render();
  checkConnectivity();
}

function applyBrush(x, y) {
  const i = y * GRID + x;
  if (tool === 'terrain') {
    terrain[i] = brushTerrain;
  } else {
    // Elevation is meaningful only on Hill tiles — painting it elsewhere
    // would be an inert control (export skips non-hill elevation).
    if (terrain[i] === 5) elevation[i] = brushElev;
  }
}

canvas.addEventListener('mousedown', (e) => {
  pushHistory();            // one undo step per stroke (mousedown→mouseup)
  painting = true;
  paintAt(e.offsetX, e.offsetY);
});
window.addEventListener('mouseup', () => { painting = false; });
canvas.addEventListener('mousemove', (e) => { if (painting) paintAt(e.offsetX, e.offsetY); });
// Touch support — drag-paint on trackpads/tablets.
canvas.addEventListener('touchstart', (e) => {
  e.preventDefault();
  pushHistory();            // stroke boundary for touch too
  painting = true;
  const t = e.touches[0], r = canvas.getBoundingClientRect();
  paintAt(t.clientX - r.left, t.clientY - r.top);
}, { passive: false });
canvas.addEventListener('touchmove', (e) => {
  e.preventDefault(); if (!painting) return;
  const t = e.touches[0], r = canvas.getBoundingClientRect();
  paintAt(t.clientX - r.left, t.clientY - r.top);
}, { passive: false });
canvas.addEventListener('touchend', () => { painting = false; });

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
  const terrainOn = t === 'terrain';
  document.getElementById('terrain-palette').classList.toggle('hidden', !terrainOn);
  document.getElementById('elev-palette').classList.toggle('hidden', terrainOn);
  document.getElementById('elev-info').classList.toggle('hidden', terrainOn);
  document.getElementById('palette-title').textContent = terrainOn ? 'Terrain palette' : 'Elevation (hill layers)';
}

document.getElementById('tool-terrain').onclick = () => setActiveTool('terrain');
document.getElementById('tool-elevation').onclick = () => setActiveTool('elevation');

document.getElementById('mirror-btn').onclick = (e) => {
  mirror = !mirror;
  e.target.classList.toggle('active', mirror);
  e.target.textContent = mirror ? 'On' : 'Off';
};

// Undo / redo buttons + keyboard. Ignore shortcuts when focus is in the map
// name text field so typing there isn't hijacked.
document.getElementById('undo-btn').onclick = undo;
document.getElementById('redo-btn').onclick = redo;
window.addEventListener('keydown', (e) => {
  const mod = e.metaKey || e.ctrlKey;
  if (!mod || e.key.toLowerCase() !== 'z') return;
  const tag = (document.activeElement && document.activeElement.tagName) || '';
  if (tag === 'INPUT' || tag === 'TEXTAREA') return;
  e.preventDefault();
  if (e.shiftKey) redo(); else undo();
});

document.getElementById('clear-btn').onclick = () => {
  pushHistory();
  terrain = new Uint8Array(N);
  elevation = new Uint8Array(N);
  render();
  checkConnectivity();
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
    render();
    checkConnectivity();
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
  render();
  checkConnectivity();
  updateUndoButtons();
  setStatus(`Loaded "${name}".`);
};

function capitalize(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

// --- Export ----------------------------------------------------------------
function goFuncName(name) {
  // PascalCase + Clash prefix: "custom" → "ClashCustom".
  const clean = name.replace(/[^A-Za-z0-9]/g, '');
  if (!clean) return 'ClashCustom';
  return 'Clash' + clean.charAt(0).toUpperCase() + clean.slice(1);
}
function goCaseName(name) {
  return name.toLowerCase().replace(/[^a-z0-9]/g, '');
}

function generateGo() {
  const name = document.getElementById('map-name').value || 'Custom';
  const func = goFuncName(name);
  const key = goCaseName(name) || 'custom';

  const lines = [];
  lines.push(`// Clash${capitalizeName(name)} — authored via client/editor/map.html.`);
  lines.push(`// Paste into server/pkg/tilemap/clash_maps.go, then register the`);
  lines.push(`// LoadClashMap case below.`);
  lines.push(`func ${func}() *GameMap {`);
  lines.push(`\tm := NewGameMap(${GRID}, ${GRID})`);
  lines.push('');
  lines.push('\t// Terrain (non-default tiles only; default is Plain).');
  let wroteTerrain = false;
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      const t = terrain[y * GRID + x];
      if (t !== 0 && !RESERVED_TERRAIN.has(t)) {
        lines.push(`\tm.SetTerrain(${x}, ${y}, ${TERRAIN_GO[t]})`);
        wroteTerrain = true;
      }
    }
  }
  if (!wroteTerrain) lines.push('\t// (all Plain — no SetTerrain calls)');
  lines.push('');
  lines.push('\t// Hill elevation layers (0=low, 1=mid, 2=peak).');
  let wroteElev = false;
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      if (terrain[y * GRID + x] === 5 && elevation[y * GRID + x] > 0) {
        lines.push(`\tm.TileAt(${x}, ${y}).Elevation = ${elevation[y * GRID + x]}`);
        wroteElev = true;
      }
    }
  }
  if (!wroteElev) lines.push('\t// (no peak/mid hills)');
  lines.push('');
  lines.push('\treturn m');
  lines.push('}');
  lines.push('');
  lines.push(`// Register in LoadClashMap (clash_maps.go):`);
  lines.push(`//\tcase "${key}":`);
  lines.push(`//\t\treturn ${func}()`);

  return lines.join('\n');
}

function capitalizeName(s) {
  // For the leading doc comment — best-effort prettified name.
  return s.replace(/[^A-Za-z0-9]/g, ' ').replace(/\b\w/g, c => c.toUpperCase()).trim() || 'Custom';
}

function setStatus(msg, bad = false) {
  statusEl.textContent = msg;
  statusEl.className = 'info' + (bad ? ' warn' : '');
}

// Gate export when connectivity is failing — confirm before shipping a map
// whose runtime spawns can't reach each other (one side would be unwinnable).
// "Export anyway" is offered because there are legitimate reasons to export a
// knowingly-broken map (debugging pathfinding, testing stranded-spawn logic).
function maybeExport(action) {
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
    `spawns (16,12) and (16,20). One side may be unwinnable.\n\nExport anyway?`
  )) {
    action();
  }
}

function doShowGo() {
  document.getElementById('export-wrap').classList.remove('hidden');
  document.getElementById('export-text').value = generateGo();
  setStatus('Go source generated.');
}
async function doCopyGo() {
  const src = generateGo();
  try {
    await navigator.clipboard.writeText(src);
    setStatus('Copied Go source to clipboard.');
  } catch {
    document.getElementById('export-wrap').classList.remove('hidden');
    document.getElementById('export-text').value = src;
    setStatus('Clipboard blocked — source shown below.', true);
  }
}

document.getElementById('show-go-btn').onclick = () => maybeExport(doShowGo);
document.getElementById('copy-go-btn').onclick = () => maybeExport(doCopyGo);

// --- Init ------------------------------------------------------------------
buildTerrainPalette();
buildElevPalette();
setActiveTool('terrain');
render();
checkConnectivity();
updateUndoButtons();
loadSnapshots();
