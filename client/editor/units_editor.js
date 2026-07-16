// Paper War — Combat Unit Editor (AI-driven balance authoring)
//
// Browser dev tool for tuning the CombatUnitTypeTable + damage matrix in
// server/pkg/component/unit_type.go. These numbers drive the AI commander
// (server/pkg/ai/ai.go) and the playtest / clash-balance tests.
//
// Like the animation editor, this is read-only against game code: it
// holds a source-of-truth snapshot in JS and exports Go source to paste
// back. It also exports/imports JSON so an AI agent can round-trip a
// candidate balance, run the playtest matrix, and iterate.
//
// Open via the Go server's static-file handler:
//   http://localhost:<port>/editor/units.html

// --- Constants (mirror server/pkg/component/unit_type.go) -----------------
//
// Keep in sync with the Go source. The "Reset to source" button restores
// these. If you change the Go file, paste the new values here too.

const WEAPONS = ['Gun', 'Cannon', 'Sniper', 'Missile'];
const ARMORS = ['Light', 'Heavy', 'Building'];
const TICK_SECONDS = 0.2; // server tick is 5 Hz (see server/pkg/combat/recruit.go)

const UNIT_KEYS = [
  'LightInfantry',
  'HeavyInfantry',
  'Sniper',
  'AntiArmorInfantry',
  'MotorGun',
  'MotorArtillery',
  'MotorMissile',
];

const UNIT_NAMES = {
  LightInfantry:     'Light Infantry',
  HeavyInfantry:     'Heavy Infantry',
  Sniper:            'Sniper',
  AntiArmorInfantry: 'Anti-Armor Inf.',
  MotorGun:          'Motor Gun',
  MotorArtillery:    'Motor Artillery',
  MotorMissile:      'Motor Missile',
};

// Tactical role — copied from server/pkg/ai/ai.go unitRole map.
const UNIT_ROLE = {
  LightInfantry:     'Frontline',
  HeavyInfantry:     'Frontline',
  Sniper:            'Ranged',
  AntiArmorInfantry: 'Ranged',
  MotorGun:          'Heavy',
  MotorArtillery:    'Heavy',
  MotorMissile:      'Heavy',
};

// AI's target army-composition ratio per role (ai.go roleTargetRatio).
const ROLE_TARGET_RATIO = { Frontline: 0.40, Ranged: 0.30, Heavy: 0.30 };
// AI's recruit floor — recruitDecisions() no-ops below 15 gold.
const AI_GOLD_FLOOR = 15;

// Source-of-truth snapshot of CombatUnitTypeTable (unit_type.go:50).
const SOURCE_STATS = {
  LightInfantry:     { Weapon: 'Gun',     Armor: 'Light', Cost: 1, HP: 100, Damage: 15, Range: 5, Cooldown: 3,  RecruitCost: 15, KillBounty: 12 },
  HeavyInfantry:     { Weapon: 'Cannon',  Armor: 'Light', Cost: 2, HP: 60,  Damage: 25, Range: 7, Cooldown: 5,  RecruitCost: 25, KillBounty: 20 },
  Sniper:            { Weapon: 'Sniper',  Armor: 'Light', Cost: 1, HP: 30,  Damage: 20, Range: 8, Cooldown: 12, RecruitCost: 50, KillBounty: 40 },
  AntiArmorInfantry: { Weapon: 'Missile', Armor: 'Light', Cost: 2, HP: 60,  Damage: 35, Range: 8, Cooldown: 6,  RecruitCost: 30, KillBounty: 24 },
  MotorGun:          { Weapon: 'Gun',     Armor: 'Heavy', Cost: 2, HP: 120, Damage: 15, Range: 5, Cooldown: 2,  RecruitCost: 25, KillBounty: 20 },
  MotorArtillery:    { Weapon: 'Cannon',  Armor: 'Heavy', Cost: 4, HP: 150, Damage: 40, Range: 7, Cooldown: 5,  RecruitCost: 50, KillBounty: 40 },
  MotorMissile:      { Weapon: 'Missile', Armor: 'Heavy', Cost: 4, HP: 130, Damage: 50, Range: 9, Cooldown: 7,  RecruitCost: 60, KillBounty: 48 },
};

// Source-of-truth snapshot of damageMatrix (unit_type.go:89).
// [weapon][armor] → percent.
const SOURCE_MATRIX = [
  // Light  Heavy  Building
  [100, 50,  0],  // Gun
  [50,  100, 25], // Cannon
  [100, 25,  0],  // Sniper
  [25,  150, 25], // Missile
];

// --- Mutable working state ----------------------------------------------

function cloneStats(s) {
  const out = {};
  for (const k of UNIT_KEYS) out[k] = { ...s[k] };
  return out;
}

let stats = cloneStats(SOURCE_STATS);
let matrix = SOURCE_MATRIX.map((r) => r.slice());

// --- DOM helpers ---------------------------------------------------------

const $ = (id) => document.getElementById(id);

function weaponConst(w) { return 'Weapon' + w; }
function armorConst(a)  { return 'Armor' + a; }
function unitConst(k)   { return 'Unit' + k; }

function isDirty() {
  for (const k of UNIT_KEYS) {
    const a = stats[k], b = SOURCE_STATS[k];
    for (const f of Object.keys(b)) if (a[f] !== b[f]) return true;
  }
  for (let w = 0; w < WEAPONS.length; w++)
    for (let a = 0; a < ARMORS.length; a++)
      if (matrix[w][a] !== SOURCE_MATRIX[w][a]) return true;
  return false;
}

// --- Validation ----------------------------------------------------------
//
// Balance invariants enforced elsewhere in the codebase. We surface them
// here so an author (or AI agent) catches a broken table before pasting.

function validateUnit(k) {
  const s = stats[k];
  const warns = [];
  // KillBounty is recomputed as 80% of RecruitCost (recruit path rounds).
  const expectedBounty = Math.round(s.RecruitCost * 0.8);
  if (Math.abs(s.KillBounty - expectedBounty) > 1) {
    warns.push(`KillBounty ≈ ${expectedBounty} (80% of RecruitCost)`);
  }
  if (s.RecruitCost < AI_GOLD_FLOOR) {
    warns.push(`RecruitCost < AI gold floor (${AI_GOLD_FLOOR})`);
  }
  if (s.Cost < 1) warns.push('Cost ≥ 1 (Leading Skill budget)');
  if (s.Cooldown < 1) warns.push('Cooldown ≥ 1 tick');
  if (s.HP <= 0) warns.push('HP > 0');
  if (s.Damage <= 0) warns.push('Damage > 0');
  if (s.Range < 1) warns.push('Range ≥ 1 tile');
  return warns;
}

// --- Derived metrics -----------------------------------------------------

function dps(s) {
  // Damage per second. Cooldown is in ticks; tick = 0.2 s.
  return s.Damage / (Math.max(1, s.Cooldown) * TICK_SECONDS);
}

function effDps(s, armorIdx) {
  const wIdx = WEAPONS.indexOf(s.Weapon);
  return dps(s) * matrix[wIdx][armorIdx] / 100;
}

// --- Render: stats table -------------------------------------------------

function renderStats() {
  const fields = [
    { key: 'Weapon',      kind: 'select', options: WEAPONS },
    { key: 'Armor',       kind: 'select', options: ARMORS },
    { key: 'Cost',        kind: 'int', min: 1,  max: 20 },
    { key: 'HP',          kind: 'int', min: 1,  max: 9999 },
    { key: 'Damage',      kind: 'int', min: 0,  max: 999 },
    { key: 'Range',       kind: 'int', min: 1,  max: 32 },
    { key: 'Cooldown',    kind: 'int', min: 1,  max: 127 },
    { key: 'RecruitCost', kind: 'int', min: 0,  max: 9999 },
    { key: 'KillBounty',  kind: 'int', min: 0,  max: 9999 },
  ];

  let html = '<table class="stats"><thead><tr>' +
    '<th>unit</th><th>role</th>' +
    fields.map((f) => `<th>${f.key}</th>`).join('') +
    '<th>warn</th></tr></thead><tbody>';
  for (const k of UNIT_KEYS) {
    const s = stats[k];
    const warns = validateUnit(k);
    html += `<tr><td class="unit">${UNIT_NAMES[k]}</td>` +
      `<td><span class="role-pill role-${UNIT_ROLE[k]}">${UNIT_ROLE[k]}</span></td>`;
    for (const f of fields) {
      const dirty = s[f.key] !== SOURCE_STATS[k][f.key];
      const cls = `num${dirty ? ' dirty' : ''}`;
      if (f.kind === 'select') {
        html += `<td><select class="${cls}" data-unit="${k}" data-field="${f.key}">` +
          f.options.map((o) =>
            `<option value="${o}"${o === s[f.key] ? ' selected' : ''}>${o}</option>`).join('') +
          `</select></td>`;
      } else {
        html += `<td><input type="number" class="${cls}" data-unit="${k}" data-field="${f.key}" ` +
          `min="${f.min}" max="${f.max}" value="${s[f.key]}"></td>`;
      }
    }
    html += `<td class="${warns.length ? 'warn' : ''}">${warns.length ? warns.join('; ') : '<span class="ok-text">ok</span>'}</td>`;
    html += '</tr>';
  }
  html += '</tbody></table>';
  $('stats-table').innerHTML = html;

  for (const inp of $('stats-table').querySelectorAll('input,select')) {
    inp.addEventListener('input', onStatInput);
    inp.addEventListener('change', onStatInput);
  }
  $('dirty-flag').textContent = isDirty()
    ? 'Dirty — copy / show Go source to reflect edits.'
    : 'Clean (matches source).';
}

function onStatInput(e) {
  const k = e.target.dataset.unit;
  const f = e.target.dataset.field;
  if (!k || !f) return;
  let v = e.target.value;
  if (typeof SOURCE_STATS[k][f] === 'number') {
    v = Number(v);
    if (!Number.isFinite(v)) return;
    v = Math.round(v);
  }
  stats[k][f] = v;
  // Editing weapon changes derived columns; rebuild downstream views.
  renderMetrics();
  renderAiView();
  // Toggle the dirty class on this cell without a full rebuild (preserves focus).
  e.target.classList.toggle('dirty', v !== SOURCE_STATS[k][f]);
  $('dirty-flag').textContent = isDirty()
    ? 'Dirty — copy / show Go source to reflect edits.'
    : 'Clean (matches source).';
}

// --- Render: damage matrix ----------------------------------------------

function renderMatrix() {
  let html = '<table class="matrix"><thead><tr><th>weapon ＼ armor</th>' +
    ARMORS.map((a) => `<th>${a}</th>`).join('') + '</tr></thead><tbody>';
  for (let w = 0; w < WEAPONS.length; w++) {
    html += `<tr><td>${WEAPONS[w]}</td>`;
    for (let a = 0; a < ARMORS.length; a++) {
      const dirty = matrix[w][a] !== SOURCE_MATRIX[w][a];
      html += `<td><input type="number" class="num${dirty ? ' dirty' : ''}" ` +
        `data-w="${w}" data-a="${a}" min="0" max="999" value="${matrix[w][a]}"></td>`;
    }
    html += '</tr>';
  }
  html += '</tbody></table>';
  $('matrix-table').innerHTML = html;
  for (const inp of $('matrix-table').querySelectorAll('input')) {
    inp.addEventListener('input', onMatrixInput);
  }
}

function onMatrixInput(e) {
  const w = Number(e.target.dataset.w);
  const a = Number(e.target.dataset.a);
  const v = Math.max(0, Math.round(Number(e.target.value)));
  if (!Number.isFinite(v)) return;
  matrix[w][a] = v;
  e.target.classList.toggle('dirty', v !== SOURCE_MATRIX[w][a]);
  renderMetrics();
  $('dirty-flag').textContent = isDirty()
    ? 'Dirty — copy / show Go source to reflect edits.'
    : 'Clean (matches source).';
}

// --- Render: derived metrics --------------------------------------------

function renderMetrics() {
  let html = '<table class="metrics"><thead><tr>' +
    '<th>unit</th><th>DPS</th>' +
    ARMORS.map((a) => `<th>vs ${a}</th>`).join('') +
    '<th>HP/gold</th><th>DPS/gold</th><th>range</th></tr></thead><tbody>';
  for (const k of UNIT_KEYS) {
    const s = stats[k];
    const d = dps(s);
    const gold = Math.max(1, s.RecruitCost);
    html += `<tr><td>${UNIT_NAMES[k]}</td>` +
      `<td class="num-c">${d.toFixed(1)}</td>` +
      ARMORS.map((_, ai) =>
        `<td class="num-c">${effDps(s, ai).toFixed(1)}</td>`).join('') +
      `<td class="num-c">${(s.HP / gold).toFixed(2)}</td>` +
      `<td class="num-c">${(d / gold).toFixed(3)}</td>` +
      `<td class="num-c">${s.Range}</td></tr>`;
  }
  html += '</tbody></table>';
  $('metrics-table').innerHTML = html;
}

// --- Render: AI view -----------------------------------------------------

function renderAiView() {
  // Role-ratio target bar.
  const roles = ['Frontline', 'Ranged', 'Heavy'];
  const tints = { Frontline: 'var(--front)', Ranged: 'var(--ranged)', Heavy: 'var(--heavy)' };
  let bar = '<div class="pillbar" style="height:14px">';
  for (const r of roles) {
    bar += `<div class="seg" title="${r} ${(ROLE_TARGET_RATIO[r] * 100).toFixed(0)}%" ` +
      `style="width:${ROLE_TARGET_RATIO[r] * 100}%;background:${tints[r]}"></div>`;
  }
  bar += '</div>';

  let html = `<div class="info">Target army ratio (ai.go roleTargetRatio):</div>${bar}`;
  html += '<table class="metrics" style="margin-top:8px"><thead><tr>' +
    '<th>unit</th><th>role</th><th>cost</th><th>cheapest?</th></tr></thead><tbody>';
  const cheapest = UNIT_KEYS
    .map((k) => ({ k, c: stats[k].RecruitCost }))
    .reduce((m, x) => (x.c < m.c ? x : m), { c: Infinity });
  for (const k of UNIT_KEYS) {
    const isCheapest = k === cheapest.k;
    html += `<tr><td>${UNIT_NAMES[k]}</td>` +
      `<td><span class="role-pill role-${UNIT_ROLE[k]}">${UNIT_ROLE[k]}</span></td>` +
      `<td class="num-c">${stats[k].RecruitCost}</td>` +
      `<td>${isCheapest ? '<span class="ok-text">← AI floor</span>' : ''}</td></tr>`;
  }
  html += '</tbody></table>';
  html += `<p class="info">AI recruitDecisions() no-ops while gold &lt; ${AI_GOLD_FLOOR}; ` +
    `cheapest unit is the effective gate on early waves.</p>`;
  $('ai-view').innerHTML = html;
}

// --- Export: Go source ---------------------------------------------------

function goSource() {
  let out = '// Generated by client/editor/units_editor.js — paste into\n' +
    '// server/pkg/component/unit_type.go (replacing the table + matrix).\n\n';

  out += 'var CombatUnitTypeTable = map[CombatUnitType]CombatUnitStats{\n';
  for (const k of UNIT_KEYS) {
    const s = stats[k];
    out += `\t${unitConst(k)}: {\n` +
      `\t\tType: ${unitConst(k)}, Weapon: ${weaponConst(s.Weapon)}, Armor: ${armorConst(s.Armor)},\n` +
      `\t\tCost: ${s.Cost}, HP: ${s.HP}, Damage: ${s.Damage}, Range: ${s.Range}, Cooldown: ${s.Cooldown},\n` +
      `\t\tRecruitCost: ${s.RecruitCost}, KillBounty: ${s.KillBounty},\n` +
      `\t},\n`;
  }
  out += '}\n\n';

  out += 'var damageMatrix = [4][3]int32{\n';
  out += '\t//              Light  Heavy  Building\n';
  for (let w = 0; w < WEAPONS.length; w++) {
    const row = matrix[w].map((n) => String(n).padStart(3)).join(', ');
    out += `\t{${row}}, // ${WEAPONS[w]}\n`;
  }
  out += '}\n';
  return out;
}

// --- Export / import: JSON ----------------------------------------------

function exportJson() {
  const payload = {
    _format: 'paper-war.combat-unit-balance.v1',
    stats,
    damageMatrix: matrix,
  };
  return JSON.stringify(payload, null, 2);
}

function importJson(text) {
  const p = JSON.parse(text);
  if (!p || typeof p !== 'object' || !p.stats || !p.damageMatrix) {
    throw new Error('Not a combat-unit-balance JSON file');
  }
  const next = cloneStats(SOURCE_STATS);
  for (const k of UNIT_KEYS) {
    if (!p.stats[k]) throw new Error(`missing unit ${k}`);
    next[k] = { ...SOURCE_STATS[k], ...p.stats[k] };
  }
  if (!Array.isArray(p.damageMatrix) || p.damageMatrix.length !== WEAPONS.length) {
    throw new Error('damageMatrix must be [4][3]');
  }
  stats = next;
  matrix = p.damageMatrix.map((r) => r.slice());
  renderAll();
}

// --- Status flash --------------------------------------------------------

let statusTimer = 0;
function flash(msg, bad = false) {
  const el = $('status');
  el.textContent = msg;
  el.style.color = bad ? 'var(--bad)' : 'var(--good)';
  clearTimeout(statusTimer);
  statusTimer = setTimeout(() => { el.textContent = ''; }, 4000);
}

// --- Wire buttons --------------------------------------------------------

function bindControls() {
  $('reset-btn').addEventListener('click', () => {
    stats = cloneStats(SOURCE_STATS);
    matrix = SOURCE_MATRIX.map((r) => r.slice());
    renderAll();
    flash('Reset to source values.');
  });

  $('copy-go-btn').addEventListener('click', async () => {
    const text = goSource();
    try {
      await navigator.clipboard.writeText(text);
      flash(isDirty() ? 'Copied tuned Go source.' : 'Copied (no changes from source).');
    } catch {
      console.log(text);
      flash('Clipboard blocked — printed to devtools console.', true);
    }
  });

  $('show-go-btn').addEventListener('click', () => {
    $('export-text').value = goSource();
    $('export-wrap').classList.remove('hidden');
    flash('Go source shown below.');
  });

  $('export-json-btn').addEventListener('click', async () => {
    const blob = new Blob([exportJson()], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'combat-unit-balance.json';
    a.click();
    URL.revokeObjectURL(url);
    flash('Exported JSON.');
  });

  $('import-json-btn').addEventListener('click', () => $('json-file').click());
  $('json-file').addEventListener('change', (e) => {
    const f = e.target.files[0];
    if (!f) return;
    f.text().then((txt) => {
      try {
        importJson(txt);
        flash('Imported JSON.');
      } catch (err) {
        flash('Import failed: ' + err.message, true);
      }
    });
    e.target.value = ''; // allow re-picking the same file
  });
}

// --- AI balance assistant (GLM via /editor/ai proxy) ---------------------
//
// The editor sends the current stats + damage matrix + the user's prompt to
// the server's /editor/ai proxy, which forwards to the GLM chat-completions
// API with a system prompt that forces a strict JSON delta. We parse the
// OpenAI-shaped response, merge the delta, and re-validate. This lets an
// author (or an AI agent) iterate on balance in natural language.

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
  $('ai-key').addEventListener('change', (e) => {
    localStorage.setItem('glm_api_key', e.target.value.trim());
  });
  $('ai-base').addEventListener('change', (e) => {
    localStorage.setItem('glm_base_url', e.target.value.trim() || AI_DEFAULTS.base);
  });
  $('ai-model').addEventListener('change', (e) => {
    localStorage.setItem('glm_model', e.target.value.trim() || AI_DEFAULTS.model);
  });

  for (const [id, text] of [
    ['ai-quick-1', 'Snipers are overpowered: reduce Sniper HP to 22 and raise Cooldown to 16.'],
    ['ai-quick-2', 'Speed up the early game: cut the cheapest unit (LightInfantry) RecruitCost to 10 and reduce all Cooldowns by 1 (min 1).'],
    ['ai-quick-3', 'Make heavy armor tankier vs guns: set damageMatrix Gun row vs Heavy to 35.'],
  ]) {
    $(id).addEventListener('click', () => { $('ai-prompt').value = text; });
  }

  $('ai-apply-btn').addEventListener('click', aiApply);
}

function aiStatus(msg, kind) {
  const el = $('ai-status');
  el.textContent = msg;
  el.style.color = kind === 'bad' ? 'var(--bad)' : kind === 'good' ? 'var(--good)' : 'var(--muted)';
}

// Merge a model-returned delta into stats + matrix. Returns a short summary.
function applyAiDelta(delta) {
  const changes = [];
  if (delta && delta.stats) {
    for (const k of UNIT_KEYS) {
      if (!delta.stats[k]) continue;
      for (const f of Object.keys(delta.stats[k])) {
        if (f in SOURCE_STATS[k]) {
          stats[k][f] = delta.stats[k][f];
          changes.push(`${k}.${f}=${delta.stats[k][f]}`);
        }
      }
    }
  }
  if (Array.isArray(delta.damageMatrix) && delta.damageMatrix.length === WEAPONS.length) {
    matrix = delta.damageMatrix.map((r) => r.slice());
    changes.push('damageMatrix replaced');
  }
  return changes;
}

// Strip markdown fences and find the first {...} block if the model wrapped
// its JSON. The system prompt asks for raw JSON, but be defensive.
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

// GLM-only config (key/base) is irrelevant when the Claude CLI backend is
// selected. Dim those inputs so the UI reflects what the server will use.
function syncBackendVisibility(backend) {
  const glmOnly = ['ai-key', 'ai-base'];
  for (const id of glmOnly) {
    const el = $(id);
    if (el) el.disabled = (backend !== 'glm');
  }
}

async function aiApply() {
  const prompt = $('ai-prompt').value.trim();
  if (!prompt) {
    aiStatus('Enter a prompt first.', 'bad');
    return;
  }
  const cfg = loadAiConfig();
  aiStatus(cfg.backend === 'claude' ? 'Asking Claude CLI…' : 'Asking GLM…', '');
  $('ai-apply-btn').disabled = true;
  try {
    const resp = await fetch('/editor/ai', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: 'combat',
        backend: cfg.backend,
        prompt,
        stats,
        damageMatrix: matrix,
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
      aiStatus('Bad response (not JSON).', 'bad');
      return;
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
      aiStatus('Model returned no applicable changes.', 'bad');
      return;
    }
    renderAll();
    aiStatus(`Applied ${changes.length} change(s): ${changes.join(', ')}`, 'good');
  } catch (err) {
    aiStatus('Request failed: ' + err.message, 'bad');
  } finally {
    $('ai-apply-btn').disabled = false;
  }
}

// --- Init ----------------------------------------------------------------

function renderAll() {
  renderStats();
  renderMatrix();
  renderMetrics();
  renderAiView();
}

bindControls();
bindAi();
renderAll();
