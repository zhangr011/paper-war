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
  { id: 0, key: 'LightInfantry',     name: 'Light Inf' },
  { id: 1, key: 'HeavyInfantry',     name: 'Heavy Inf' },
  { id: 2, key: 'Sniper',            name: 'Sniper' },
  { id: 3, key: 'AntiArmorInfantry', name: 'Anti-Armor' },
  { id: 4, key: 'MotorGun',          name: 'Motor Gun' },
  { id: 5, key: 'MotorArtillery',    name: 'Motor Art' },
  { id: 6, key: 'MotorMissile',      name: 'Motor Mis' },
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
  showCollision: false, // collision-radius overlay toggle (ADR-0030)
};

let previewTimeMs = 0; // accumulates while playing
let lastRaf = 0;

// CombatUnitTypeTable fetched from /editor/unit-stats — single server source,
// no hand-maintained mirror. Keyed by full unit name (UNIT_TYPES[].key). Used
// only for the read-only collision overlay; radius is authored in the units
// editor / unit_type.go (ADR-0030), not here.
let unitStats = {};

async function loadUnitStats() {
  try {
    const r = await fetch('/editor/unit-stats');
    if (r.ok) unitStats = await r.json();
  } catch { unitStats = {}; }
  renderPreview();
  renderScrubStrip();
  renderGridView();
}

function radiusForUnit(unitType) {
  const u = UNIT_TYPES.find((x) => x.id === unitType);
  if (!u || !unitStats[u.key]) return null;
  const r = Number(unitStats[u.key].radius);
  return Number.isFinite(r) && r > 0 ? r : null;
}

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
  if (ui.showCollision) drawCollisionOverlay(ctx, canvas, unitType);
}

// Read-only collision-radius overlay (ADR-0030). The sprite cell is treated
// as 1 tile (TILE_WIDTH = 32 game-px per tile, gl.js:1428), so the circle
// radius in canvas px = radiusTiles × canvas.width, centred on the cell.
// Sourced from /editor/unit-stats — not editable here.
function drawCollisionOverlay(ctx, canvas, unitType) {
  const r = radiusForUnit(unitType);
  if (r === null) return;
  const cx = canvas.width / 2;
  const cy = canvas.height / 2;
  const px = r * canvas.width;
  ctx.save();
  ctx.strokeStyle = '#c8503c'; // --bad red, readable over sprite art
  ctx.lineWidth = 1;
  ctx.setLineDash([2, 2]);
  ctx.beginPath();
  ctx.arc(cx, cy, px, 0, Math.PI * 2);
  ctx.stroke();
  ctx.setLineDash([]);
  ctx.fillStyle = '#c8503c';
  ctx.beginPath();
  ctx.arc(cx, cy, 1, 0, Math.PI * 2);
  ctx.fill();
  ctx.restore();
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
  const tog = $('collision-toggle');
  if (tog) {
    tog.addEventListener('change', (e) => {
      ui.showCollision = e.target.checked;
      renderPreview();
      renderScrubStrip();
      renderGridView();
    });
  }
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

// --- AI timing assistant (GLM via /editor/ai proxy) ---------------------
//
// Mirrors the combat-unit editor's AI panel. Sends the current
// framesPerState + animFps + a prompt to /editor/ai with kind:"animation";
// the proxy forwards to GLM with a system prompt that forces a strict
// {framesPerState?, animFps?} delta. We clamp + merge + re-render live.

const AI_DEFAULTS = {
  backend: 'claude',                       // 'claude' (CLI) or 'glm' (HTTP)
  base: 'https://open.bigmodel.cn/api/paas/v4',
  model: 'glm-5.2',
};

function loadAiConfig() {
  return {
    backend: localStorage.getItem('ai_backend') || AI_DEFAULTS.backend,
    key: localStorage.getItem('glm_api_key') || '',
    base: localStorage.getItem('glm_base_url') || AI_DEFAULTS.base,
    model: localStorage.getItem('glm_model') || AI_DEFAULTS.model,
  };
}

function syncBackendVisibility(backend) {
  for (const id of ['ai-key', 'ai-base']) {
    const el = $(id);
    if (el) el.disabled = (backend !== 'glm');
  }
}

function bindAi() {
  const cfg = loadAiConfig();
  $('ai-backend').value = cfg.backend;
  $('ai-key').value = cfg.key;
  $('ai-base').value = cfg.base;
  $('ai-model').value = cfg.model;
  syncBackendVisibility(cfg.backend);

  $('ai-backend').addEventListener('change', (e) => {
    localStorage.setItem('ai_backend', e.target.value);
    syncBackendVisibility(e.target.value);
  });
  $('ai-key').addEventListener('change', (e) =>
    localStorage.setItem('glm_api_key', e.target.value.trim()));
  $('ai-base').addEventListener('change', (e) =>
    localStorage.setItem('glm_base_url', e.target.value.trim() || AI_DEFAULTS.base));
  $('ai-model').addEventListener('change', (e) =>
    localStorage.setItem('glm_model', e.target.value.trim() || AI_DEFAULTS.model));

  const quick = [
    ['ai-quick-1', 'Make the attack animation snappier: 4 frames at 18 fps. Keep die at 4 frames but slow it to 6 fps for a heavier fall.'],
    ['ai-quick-2', 'Make idle breathing more lively: bump idle and idle2 to 10 fps. Keep idle2 occasional by leaving its frame count at 2.'],
    ['ai-quick-3', 'Speed up all movement: set move to 4 frames at 14 fps.'],
  ];
  for (const [id, text] of quick) $(id).addEventListener('click', () => { $('ai-prompt').value = text; });

  $('ai-apply-btn').addEventListener('click', aiApply);
}

function aiStatus(msg, kind) {
  const el = $('ai-status');
  el.textContent = msg;
  el.style.color = kind === 'bad' ? '#c8503c' : kind === 'good' ? '#5aa05a' : 'var(--muted)';
}

// Strip markdown fences and isolate the {...} block if the model wrapped it.
function extractJson(text) {
  if (!text) return null;
  let t = text.trim();
  const fence = t.match(/```(?:json)?\s*([\s\S]*?)```/i);
  if (fence) t = fence[1].trim();
  const start = t.indexOf('{');
  const end = t.lastIndexOf('}');
  if (start !== -1 && end !== -1 && end > start) t = t.slice(start, end + 1);
  try { return JSON.parse(t); } catch { return null; }
}

// Merge a model delta into framesPerState / animFps. Each array must be the
// full length-STATES array; values are clamped to valid ranges. Returns a
// short change summary.
function applyAiDelta(delta) {
  const changes = [];
  if (Array.isArray(delta.framesPerState) && delta.framesPerState.length === STATES) {
    for (let s = 0; s < STATES; s++) {
      const v = Math.max(1, Math.min(MAX_FRAMES, Math.round(delta.framesPerState[s])));
      if (v !== framesPerState[s]) {
        framesPerState[s] = v;
        changes.push(`frames[${STATE_NAMES[s]}]=${v}`);
      }
    }
  }
  if (Array.isArray(delta.animFps) && delta.animFps.length === STATES) {
    for (let s = 0; s < STATES; s++) {
      const v = Math.max(1, Math.min(30, Math.round(delta.animFps[s])));
      if (v !== animFps[s]) {
        animFps[s] = v;
        changes.push(`fps[${STATE_NAMES[s]}]=${v}`);
      }
    }
    ui.fps = animFps[ui.state];
    $('fps-input').value = ui.fps;
  }
  ui.scrubFrame = null;
  return changes;
}

async function aiApply() {
  const prompt = $('ai-prompt').value.trim();
  if (!prompt) { aiStatus('Enter a prompt first.', 'bad'); return; }
  const cfg = loadAiConfig();
  aiStatus(cfg.backend === 'claude' ? 'Asking Claude CLI…' : 'Asking GLM…', '');
  $('ai-apply-btn').disabled = true;
  try {
    const resp = await fetch('/editor/ai', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: 'animation',
        backend: cfg.backend,
        prompt,
        framesPerState,
        animFps,
        model: cfg.model,
        apiKey: cfg.key,
        baseUrl: cfg.base,
      }),
    });
    const body = await resp.text();
    if (!resp.ok) {
      aiStatus(`Proxy error ${resp.status}: ${body.slice(0, 160)}`, 'bad');
      return;
    }
    let envelope;
    try { envelope = JSON.parse(body); } catch {
      aiStatus('Bad response (not JSON).', 'bad'); return;
    }
    const content = envelope?.choices?.[0]?.message?.content;
    const delta = extractJson(content);
    if (!delta) {
      aiStatus('No JSON delta in model response.', 'bad');
      console.log('GLM raw content:', content);
      return;
    }
    const changes = applyAiDelta(delta);
    if (!changes.length) {
      aiStatus('Model returned no applicable changes.', 'bad'); return;
    }
    renderAll();
    aiStatus(`Applied ${changes.length} change(s): ${changes.join(', ')}`, 'good');
  } catch (err) {
    aiStatus('Request failed: ' + err.message, 'bad');
  } finally {
    $('ai-apply-btn').disabled = false;
  }
}

// --- Init ---------------------------------------------------------------

rebuildUnitRow();
rebuildStateRow();
rebuildDirRow();
bindPlayback();
bindCopy();
bindAi();
loadUnitStats();
renderAll();
requestAnimationFrame(loop);
