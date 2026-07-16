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
  'Wall', 'Snow', 'Desert', 'Stronghold1', 'Stronghold2', 'Stronghold3',
  'Stronghold4', 'Stronghold5',
];
// Go identifier for each terrain type, as used by clash_maps.go SetTerrain.
const TERRAIN_GO = [
  'component.TerrainPlain', 'component.TerrainRoad', 'component.TerrainShallow',
  'component.TerrainDeep', 'component.TerrainForest', 'component.TerrainHill',
  'component.TerrainSwamp', 'component.TerrainBridge', 'component.TerrainWall',
  'component.TerrainSnow', 'component.TerrainDesert',
  'component.TerrainStronghold1', 'component.TerrainStronghold2',
  'component.TerrainStronghold3', 'component.TerrainStronghold4',
  'component.TerrainStronghold5',
];

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

// --- Model -----------------------------------------------------------------
const N = GRID * GRID;
let terrain = new Uint8Array(N);   // all 0 (Plain)
let elevation = new Uint8Array(N); // all 0

let tool = 'terrain';        // 'terrain' | 'elevation'
let brushTerrain = 0;        // active terrain type
let brushElev = 1;           // active elevation layer
let mirror = true;
let painting = false;

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
  for (const s of RUNTIME_SPAWNS) {
    ctx.fillStyle = s.color;
    ctx.beginPath();
    ctx.arc(s.x * TILE + TILE / 2, s.y * TILE + TILE / 2, TILE * 0.35, 0, Math.PI * 2);
    ctx.fill();
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

canvas.addEventListener('mousedown', (e) => { painting = true; paintAt(e.offsetX, e.offsetY); });
window.addEventListener('mouseup', () => { painting = false; });
canvas.addEventListener('mousemove', (e) => { if (painting) paintAt(e.offsetX, e.offsetY); });
// Touch support — drag-paint on trackpads/tablets.
canvas.addEventListener('touchstart', (e) => {
  e.preventDefault(); painting = true;
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

document.getElementById('clear-btn').onclick = () => {
  terrain = new Uint8Array(N);
  elevation = new Uint8Array(N);
  render();
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
  const name = document.getElementById('snapshot-select').value;
  if (!name || !snapshots[name]) {
    terrain = new Uint8Array(N);
    elevation = new Uint8Array(N);
    render();
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
      if (t !== 0) {
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

document.getElementById('show-go-btn').onclick = () => {
  document.getElementById('export-wrap').classList.remove('hidden');
  document.getElementById('export-text').value = generateGo();
  setStatus('Go source generated.');
};

document.getElementById('copy-go-btn').onclick = async () => {
  const src = generateGo();
  try {
    await navigator.clipboard.writeText(src);
    setStatus('Copied Go source to clipboard.');
  } catch {
    // Fallback: show it for manual copy.
    document.getElementById('export-wrap').classList.remove('hidden');
    document.getElementById('export-text').value = src;
    setStatus('Clipboard blocked — source shown below.', true);
  }
};

// --- Init ------------------------------------------------------------------
buildTerrainPalette();
buildElevPalette();
setActiveTool('terrain');
render();
loadSnapshots();
