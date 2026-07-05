// Paper War — Animation Editor (issue #50)
//
// Browser dev tool for browsing and tuning combat-unit animations.
// Imports the live atlas module so what you see is exactly what the game
// renders. Read-only against sprite code — tunes parameters only.
//
// Open via the Go server's static-file handler:
//   http://localhost:<port>/editor/animation.html

import {
  drawCell,
  currentFrame,
  atlasCell,
  ATLAS_CELL,
  ATLAS_COLS,
  ATLAS_ROWS,
  ATLAS_W,
  ATLAS_H,
  FRAMES_PER_STATE,
  ANIM_FPS,
  STATES,
  DIRECTIONS,
  MAX_FRAMES_PER_SPRITE,
  STATE_IDLE, STATE_IDLE2, STATE_MOVE, STATE_ATTACK, STATE_DIE,
  DIR_S, DIR_E, DIR_N, DIR_W,
} from '../src/unit_atlas.js';

// --- Constants ----------------------------------------------------------

const UNIT_TYPES = [
  { id: 0, name: 'Light Inf' },
  { id: 1, name: 'Heavy Inf' },
  { id: 2, name: 'Sniper' },
  { id: 3, name: 'Anti-Armor' },
  { id: 4, name: 'Motor Gun' },
  { id: 5, name: 'Motor Art' },
  { id: 6, name: 'Motor Mis' },
];

const STATE_NAMES = ['Idle', 'Idle2', 'Move', 'Attack', 'Die'];
const DIR_NAMES = ['S', 'E', 'N', 'W'];

// --- State --------------------------------------------------------------

const ui = {
  unitType: 0,
  state: STATE_ATTACK,
  dir: DIR_S,
  fps: ANIM_FPS[STATE_ATTACK], // editable override for the preview
  playing: true,
  scrubFrame: null, // when non-null, preview holds on this frame
};

let previewTimeMs = 0; // accumulates while playing
let lastRaf = 0;

// --- DOM ----------------------------------------------------------------

const $ = (id) => document.getElementById(id);

function makeButtonRow(containerId, items, current, onSelect) {
  const el = $(containerId);
  el.innerHTML = '';
  const label = document.createElement('label');
  label.textContent = containerId.replace('-row', '').replace('unit', 'unit type');
  el.appendChild(label);
  for (const it of items) {
    const b = document.createElement('button');
    b.textContent = it.name;
    b.dataset.id = it.id;
    if (it.id === current) b.classList.add('active');
    b.addEventListener('click', () => {
      ui.scrubFrame = null;
      onSelect(it.id);
      Array.from(el.querySelectorAll('button')).forEach((x) =>
        x.classList.toggle('active', Number(x.dataset.id) === it.id));
    });
    el.appendChild(b);
  }
}

// --- Rendering ----------------------------------------------------------

function paintCell(canvas, unitType, state, dir, frame) {
  const ctx = canvas.getContext('2d');
  ctx.imageSmoothingEnabled = false;
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  drawCell(ctx, 0, 0, unitType, state, dir, frame);
}

function renderPreview() {
  const frameCount = FRAMES_PER_STATE[ui.state];
  let frame;
  if (ui.scrubFrame !== null) {
    frame = Math.max(0, Math.min(frameCount - 1, ui.scrubFrame));
  } else if (ui.playing) {
    // Local frame advance so the FPS override slider takes effect.
    // Mirrors currentFrame()'s math without the hardcoded ANIM_FPS.
    const fps = ui.fps;
    const fc = frameCount;
    const t = (previewTimeMs / 1000) * fps;
    frame = fc > 0 ? Math.floor(t) % fc : 0;
  } else {
    frame = ui.scrubFrame ?? 0;
  }
  paintCell($('preview'), ui.unitType, ui.state, ui.dir, frame);
  $('frame-info').textContent = `frame ${frame + 1}/${frameCount}  ·  ${ui.fps} fps`;
}

function renderScrubStrip() {
  const strip = $('scrub-strip');
  strip.innerHTML = '';
  const frameCount = FRAMES_PER_STATE[ui.state];
  for (let f = 0; f < frameCount; f++) {
    const c = document.createElement('canvas');
    c.width = ATLAS_CELL;
    c.height = ATLAS_CELL;
    paintCell(c, ui.unitType, ui.state, ui.dir, f);
    c.title = `frame ${f}`;
    c.addEventListener('click', () => {
      ui.scrubFrame = f;
      ui.playing = false;
      $('play-btn').classList.remove('active');
      $('play-btn').textContent = '▶ Play';
      renderPreview();
      updateScrubSelection();
    });
    strip.appendChild(c);
  }
  updateScrubSelection();
}

function updateScrubSelection() {
  const frameCount = FRAMES_PER_STATE[ui.state];
  const active = ui.scrubFrame ?? -1;
  Array.from($('scrub-strip').children).forEach((c, i) => {
    c.classList.toggle('selected',
      active === -1 ? false : i === active);
    void frameCount;
  });
}

function renderGridView() {
  const grid = $('grid-view');
  grid.innerHTML = '';
  const frameCount = FRAMES_PER_STATE[ui.state];
  for (let d = 0; d < DIRECTIONS; d++) {
    const row = document.createElement('div');
    row.className = 'grid-row';
    const label = document.createElement('div');
    label.className = 'grid-label';
    label.textContent = DIR_NAMES[d];
    row.appendChild(label);
    for (let f = 0; f < frameCount; f++) {
      const c = document.createElement('canvas');
      c.className = 'grid-cell';
      c.width = ATLAS_CELL;
      c.height = ATLAS_CELL;
      paintCell(c, ui.unitType, ui.state, d, f);
      row.appendChild(c);
    }
    grid.appendChild(row);
  }
}

function renderAtlas() {
  const canvas = $('atlas');
  canvas.width = ATLAS_W;
  canvas.height = ATLAS_H;
  const ctx = canvas.getContext('2d');
  ctx.imageSmoothingEnabled = false;
  ctx.clearRect(0, 0, ATLAS_W, ATLAS_H);
  // Render every sprite slot in atlas order, mirroring atlasCell()'s
  // layout so this view matches what the GPU samples at runtime.
  for (let unitType = 0; unitType < UNIT_TYPES.length; unitType++) {
    for (let state = 0; state < STATES; state++) {
      for (let d = 0; d < DIRECTIONS; d++) {
        const frameCount = FRAMES_PER_STATE[state];
        for (let f = 0; f < frameCount; f++) {
          const cell = atlasCell(unitType, state, d, f);
          drawCell(ctx, cell.x, cell.y, unitType, state, d, f);
        }
      }
    }
  }
}

function renderConstants() {
  $('constants').innerHTML =
    `<div><code>FRAMES_PER_STATE = [${FRAMES_PER_STATE.join(', ')}]</code></div>` +
    `<div><code>ANIM_FPS = [${ANIM_FPS.join(', ')}]</code></div>` +
    `<div>ATLAS_W × ATLAS_H = ${ATLAS_W} × ${ATLAS_H} ` +
    `(${ATLAS_COLS} cols × ${ATLAS_ROWS} rows)</div>` +
    `<div>MAX_FRAMES_PER_SPRITE = ${MAX_FRAMES_PER_SPRITE}</div>`;
}

// --- Controls -----------------------------------------------------------

function rebuildUnitRow() {
  makeButtonRow('unit-row', UNIT_TYPES, ui.unitType, (id) => {
    ui.unitType = id;
    renderAll();
  });
}
function rebuildStateRow() {
  makeButtonRow('state-row',
    STATE_NAMES.map((name, id) => ({ id, name })),
    ui.state, (id) => {
      ui.state = id;
      ui.fps = ANIM_FPS[id];
      $('fps-input').value = ui.fps;
      renderAll();
    });
}
function rebuildDirRow() {
  makeButtonRow('dir-row',
    DIR_NAMES.map((name, id) => ({ id, name })),
    ui.dir, (id) => {
      ui.dir = id;
      renderScrubStrip();
      renderPreview();
    });
}

function bindPlayback() {
  $('play-btn').addEventListener('click', () => {
    ui.playing = !ui.playing;
    if (ui.playing) ui.scrubFrame = null;
    $('play-btn').classList.toggle('active', ui.playing);
    $('play-btn').textContent = ui.playing ? '▶ Play' : '⏸ Pause';
    updateScrubSelection();
  });
  $('prev-btn').addEventListener('click', () => {
    const fc = FRAMES_PER_STATE[ui.state];
    ui.scrubFrame = ((ui.scrubFrame ?? 0) - 1 + fc) % fc;
    ui.playing = false;
    $('play-btn').classList.remove('active');
    $('play-btn').textContent = '⏸ Pause';
    renderPreview();
    updateScrubSelection();
  });
  $('next-btn').addEventListener('click', () => {
    const fc = FRAMES_PER_STATE[ui.state];
    ui.scrubFrame = ((ui.scrubFrame ?? -1) + 1) % fc;
    ui.playing = false;
    $('play-btn').classList.remove('active');
    $('play-btn').textContent = '⏸ Pause';
    renderPreview();
    updateScrubSelection();
  });
  $('fps-input').addEventListener('input', (e) => {
    const v = Number(e.target.value);
    if (Number.isFinite(v) && v >= 1 && v <= 30) {
      ui.fps = v;
      previewTimeMs = 0; // restart cycle so the new rate is audible immediately
      renderPreview();
    }
  });
}

function bindCopy() {
  $('copy-btn').addEventListener('click', async () => {
    const text =
      `// From animation editor — paste into client/src/unit_atlas.js\n` +
      `export const FRAMES_PER_STATE = [${FRAMES_PER_STATE.join(', ')}];\n` +
      `export const ANIM_FPS = [${ANIM_FPS.join(', ')}];\n`;
    const status = $('copy-status');
    try {
      await navigator.clipboard.writeText(text);
      status.textContent = 'Copied — paste into client/src/unit_atlas.js';
    } catch {
      // Fallback: select a prebuilt textarea.
      status.textContent = 'Clipboard blocked — constants printed to devtools console.';
      console.log(text);
    }
  });
}

// --- Loop ---------------------------------------------------------------

function loop(t) {
  if (!lastRaf) lastRaf = t;
  const dt = t - lastRaf;
  lastRaf = t;
  if (ui.playing && ui.scrubFrame === null) {
    previewTimeMs += dt;
    renderPreview();
  }
  requestAnimationFrame(loop);
}

function renderAll() {
  renderScrubStrip();
  renderGridView();
  renderAtlas();
  renderConstants();
  renderPreview();
}

// --- Init ---------------------------------------------------------------

rebuildUnitRow();
rebuildStateRow();
rebuildDirRow();
bindPlayback();
bindCopy();
renderAll();
requestAnimationFrame(loop);
