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

// Mutable copies of the imported constants. ES module imports are
// read-only, so to let the editor actually *tune* these (and have the
// copy-to-clipboard output reflect the tuned values), we copy them into
// local arrays on init. All editor logic reads from these, not the
// imports. Frame count is capped at MAX_FRAMES_PER_SPRITE (atlas slot
// reservation) and at 1 (a state must have at least one frame).
const framesPerState = FRAMES_PER_STATE.slice();
const animFps = ANIM_FPS.slice();
const MAX_FRAMES = MAX_FRAMES_PER_SPRITE; // 4 — atlas reserves this many per slot

const ui = {
  unitType: 0,
  state: STATE_ATTACK,
  dir: DIR_S,
  fps: animFps[STATE_ATTACK], // editable per-state FPS override for the preview
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
  const frameCount = framesPerState[ui.state];
  let frame;
  if (ui.scrubFrame !== null) {
    frame = Math.max(0, Math.min(frameCount - 1, ui.scrubFrame));
  } else if (ui.playing) {
    // Local frame advance so the FPS override slider takes effect.
    // Mirrors currentFrame()'s math without the hardcoded animFps.
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
  const frameCount = framesPerState[ui.state];
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
  const frameCount = framesPerState[ui.state];
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
  const frameCount = framesPerState[ui.state];
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
        const frameCount = framesPerState[state];
        for (let f = 0; f < frameCount; f++) {
          const cell = atlasCell(unitType, state, d, f);
          drawCell(ctx, cell.x, cell.y, unitType, state, d, f);
        }
      }
    }
  }
}

function renderConstants() {
  // Editable per-state inputs. Frame count 1..MAX_FRAMES; FPS 1..30.
  // Editing updates the mutable arrays and re-renders preview/grid/atlas
  // so the change is immediately visible. Copy-to-clipboard reads the
  // same arrays, so tuned values flow into the pasted source.
  let html = '<table class="tune-table"><thead><tr>' +
    '<th>state</th><th>frames</th><th>fps</th></tr></thead><tbody>';
  for (let s = 0; s < STATES; s++) {
    html += `<tr><td>${STATE_NAMES[s]}</td>` +
      `<td><input type="number" class="tune-input" data-kind="frames" data-state="${s}" ` +
      `min="1" max="${MAX_FRAMES}" value="${framesPerState[s]}"></td>` +
      `<td><input type="number" class="tune-input" data-kind="fps" data-state="${s}" ` +
      `min="1" max="30" value="${animFps[s]}"></td></tr>`;
  }
  html += '</tbody></table>';
  html += `<div class="info">ATLAS_W × ATLAS_H = ${ATLAS_W} × ${ATLAS_H} ` +
    `(${ATLAS_COLS} cols × ${ATLAS_ROWS} rows) · MAX_FRAMES_PER_SPRITE = ${MAX_FRAMES_PER_SPRITE}</div>`;
  html += `<div class="info">Dirty: <span id="dirty-flag">${isDirty() ? 'yes — copy to reflect edits' : 'no'}</span></div>`;
  $('constants').innerHTML = html;
  // Wire the inputs.
  for (const inp of $('constants').querySelectorAll('.tune-input')) {
    inp.addEventListener('input', (e) => {
      const v = Number(e.target.value);
      if (!Number.isFinite(v)) return;
      const s = Number(e.target.dataset.state);
      const clamped = e.target.dataset.kind === 'frames'
        ? Math.max(1, Math.min(MAX_FRAMES, Math.round(v)))
        : Math.max(1, Math.min(30, Math.round(v)));
      if (e.target.dataset.kind === 'frames') {
        framesPerState[s] = clamped;
      } else {
        animFps[s] = clamped;
        if (s === ui.state) { ui.fps = clamped; $('fps-input').value = clamped; }
      }
      ui.scrubFrame = null;
      // Re-render the bits that depend on frame count / fps. Constants
      // panel itself is rebuilt to update the dirty flag.
      renderScrubStrip();
      renderGridView();
      renderAtlas();
      renderPreview();
      // Re-wire the tune inputs (innerHTML wiped their listeners) but
      // preserve focus.
      const focus = document.activeElement;
      const selStart = focus && focus.selectionStart;
      const selEnd = focus && focus.selectionEnd;
      renderConstants();
      if (focus && focus.dataset && focus.dataset.kind) {
        const restored = $(`input[data-kind="${focus.dataset.kind}"][data-state="${focus.dataset.state}"]`);
        if (restored) { restored.focus(); restored.setSelectionRange(selStart, selEnd); }
      }
    });
  }
}

function isDirty() {
  for (let s = 0; s < STATES; s++) {
    if (framesPerState[s] !== FRAMES_PER_STATE[s]) return true;
    if (animFps[s] !== ANIM_FPS[s]) return true;
  }
  return false;
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
      ui.fps = animFps[id];
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
    const fc = framesPerState[ui.state];
    ui.scrubFrame = ((ui.scrubFrame ?? 0) - 1 + fc) % fc;
    ui.playing = false;
    $('play-btn').classList.remove('active');
    $('play-btn').textContent = '⏸ Pause';
    renderPreview();
    updateScrubSelection();
  });
  $('next-btn').addEventListener('click', () => {
    const fc = framesPerState[ui.state];
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
    // Canonical source identifiers — these are what unit_atlas.js exports.
    const dirty = isDirty();
    const text =
      (dirty ? '// TUNED in animation editor — paste into client/src/unit_atlas.js\n'
             : '// From animation editor (unchanged) — paste into client/src/unit_atlas.js\n') +
      `export const FRAMES_PER_STATE = [${framesPerState.join(', ')}];\n` +
      `export const ANIM_FPS = [${animFps.join(', ')}];\n`;
    const status = $('copy-status');
    try {
      await navigator.clipboard.writeText(text);
      status.textContent = dirty
        ? 'Copied tuned values — paste into client/src/unit_atlas.js'
        : 'Copied (no changes from current source)';
    } catch {
      // Fallback: select a prebuilt textarea.
      status.textContent = 'Clipboard blocked — constants printed to devtools console.';
      console.log(text);
    }
  });
  $('reset-btn').addEventListener('click', () => {
    for (let s = 0; s < STATES; s++) {
      framesPerState[s] = FRAMES_PER_STATE[s];
      animFps[s] = ANIM_FPS[s];
    }
    ui.fps = animFps[ui.state];
    $('fps-input').value = ui.fps;
    ui.scrubFrame = null;
    renderAll();
    $('copy-status').textContent = 'Reset to source values.';
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
