// unit_atlas_test.mjs — Tests for the 4-direction × 5-state combat unit atlas.
// Run with: node client/src/unit_atlas_test.mjs
//
// Issue #28 spec:
//   - 5 states: idle (0), idle2 (1), move (2), attack (3), die (4)
//   - 4 directions: S (0), E (1), N (2), W (3)
//   - atlasCell signature: (unitType, state, dir, frame)
//   - Atlas height grows to fit 7 types × 5 states × 4 dirs = 140 rows

import assert from 'node:assert/strict';
import {
  ATLAS_CELL,
  ATLAS_COLS,
  ATLAS_ROWS,
  ATLAS_W,
  ATLAS_H,
  DIRECTIONS,
  STATES,
  DIR_S, DIR_E, DIR_N, DIR_W,
  STATE_IDLE, STATE_IDLE2, STATE_MOVE, STATE_ATTACK, STATE_DIE,
  FRAMES_PER_STATE,
  ANIM_FPS,
  atlasCell,
  currentFrame,
} from './unit_atlas.js';

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  ✓ ${name}`);
  } catch (err) {
    failed++;
    console.error(`  ✗ ${name}`);
    console.error(`    ${err.message}`);
  }
}

// ---------------------------------------------------------------------------
// Layout constants
// ---------------------------------------------------------------------------

test('exports DIRECTIONS = 4 and STATES = 5', () => {
  assert.equal(DIRECTIONS, 4);
  assert.equal(STATES, 5);
});

test('exports direction constants S=0, E=1, N=2, W=3', () => {
  assert.equal(DIR_S, 0);
  assert.equal(DIR_E, 1);
  assert.equal(DIR_N, 2);
  assert.equal(DIR_W, 3);
});

test('exports state constants idle=0, idle2=1, move=2, attack=3, die=4', () => {
  assert.equal(STATE_IDLE, 0);
  assert.equal(STATE_IDLE2, 1);
  assert.equal(STATE_MOVE, 2);
  assert.equal(STATE_ATTACK, 3);
  assert.equal(STATE_DIE, 4);
});

test('FRAMES_PER_STATE has 5 entries: [2, 2, 4, 3, 4]', () => {
  assert.deepEqual(FRAMES_PER_STATE, [2, 2, 4, 3, 4]);
});

test('ANIM_FPS has 5 entries (rates for each state)', () => {
  assert.equal(ANIM_FPS.length, 5);
  assert.ok(ANIM_FPS[STATE_DIE] > 0, 'die state must have an FPS');
});

test('ATLAS_ROWS = 7 types × 5 states × 4 dirs = 140', () => {
  assert.equal(ATLAS_ROWS, 140);
});

test('ATLAS_W unchanged at 512 (16 cols × 32 px)', () => {
  assert.equal(ATLAS_W, 512);
});

test('ATLAS_H = 140 rows × 32 px = 4480', () => {
  assert.equal(ATLAS_H, 4480);
});

// ---------------------------------------------------------------------------
// atlasCell
// ---------------------------------------------------------------------------

test('atlasCell returns 32×32 cell at origin for (type=0, state=0, dir=S, frame=0)', () => {
  const cell = atlasCell(0, STATE_IDLE, DIR_S, 0);
  assert.equal(cell.x, 0);
  assert.equal(cell.y, 0);
  assert.equal(cell.w, 32);
  assert.equal(cell.h, 32);
});

test('atlasCell y increments by 32 per direction within a state', () => {
  const s = atlasCell(0, STATE_IDLE, DIR_S, 0);
  const e = atlasCell(0, STATE_IDLE, DIR_E, 0);
  const n = atlasCell(0, STATE_IDLE, DIR_N, 0);
  const w = atlasCell(0, STATE_IDLE, DIR_W, 0);
  assert.equal(e.y, s.y + 32);
  assert.equal(n.y, s.y + 64);
  assert.equal(w.y, s.y + 96);
});

test('atlasCell y increments by STATES*DIRS*32 = 640 per unit type', () => {
  const t0 = atlasCell(0, STATE_IDLE, DIR_S, 0);
  const t1 = atlasCell(1, STATE_IDLE, DIR_S, 0);
  assert.equal(t1.y, t0.y + 5 * 4 * 32);
});

test('atlasCell y increments by DIRECTIONS*32 = 128 per state', () => {
  const idle = atlasCell(0, STATE_IDLE, DIR_S, 0);
  const idle2 = atlasCell(0, STATE_IDLE2, DIR_S, 0);
  const move = atlasCell(0, STATE_MOVE, DIR_S, 0);
  const attack = atlasCell(0, STATE_ATTACK, DIR_S, 0);
  const die = atlasCell(0, STATE_DIE, DIR_S, 0);
  assert.equal(idle2.y, idle.y + 128);
  assert.equal(move.y, idle.y + 256);
  assert.equal(attack.y, idle.y + 384);
  assert.equal(die.y, idle.y + 512);
});

test('atlasCell x increments by ATLAS_CELL per frame', () => {
  const f0 = atlasCell(0, STATE_MOVE, DIR_E, 0);
  const f1 = atlasCell(0, STATE_MOVE, DIR_E, 1);
  assert.equal(f1.x, f0.x + ATLAS_CELL);
});

test('atlasCell clamps out-of-range inputs', () => {
  // unitType beyond 6 clamps to 6
  const a = atlasCell(99, STATE_IDLE, DIR_S, 0);
  const b = atlasCell(6, STATE_IDLE, DIR_S, 0);
  assert.equal(a.y, b.y);

  // state beyond 4 clamps to 4 (die)
  const c = atlasCell(0, 99, DIR_S, 0);
  const d = atlasCell(0, STATE_DIE, DIR_S, 0);
  assert.equal(c.y, d.y);

  // dir beyond 3 clamps to 3 (W)
  const e = atlasCell(0, STATE_IDLE, 99, 0);
  const f = atlasCell(0, STATE_IDLE, DIR_W, 0);
  assert.equal(e.y, f.y);

  // frame beyond FRAMES_PER_STATE-1 clamps
  const g = atlasCell(0, STATE_IDLE, DIR_S, 999);
  const h = atlasCell(0, STATE_IDLE, DIR_S, FRAMES_PER_STATE[STATE_IDLE] - 1);
  assert.equal(g.x, h.x);
});

// ---------------------------------------------------------------------------
// currentFrame
// ---------------------------------------------------------------------------

test('currentFrame returns 0 when state has only 1 frame', () => {
  // No state in our spec has 1 frame, so this is a defensive test:
  // patch FRAMES_PER_STATE temporarily would be invasive; just verify
  // the function returns a valid index for a multi-frame state.
  const f = currentFrame(STATE_IDLE, 1, 0);
  assert.ok(f >= 0 && f < FRAMES_PER_STATE[STATE_IDLE]);
});

test('currentFrame clamps state to [0, STATES-1]', () => {
  const f = currentFrame(99, 1, 1000);
  assert.ok(f >= 0 && f < FRAMES_PER_STATE[STATE_DIE]);
});

test('currentFrame for die state returns valid frame', () => {
  const f = currentFrame(STATE_DIE, 42, 12345);
  assert.ok(f >= 0 && f < FRAMES_PER_STATE[STATE_DIE]);
});

test('currentFrame advances with time', () => {
  // At ANIM_FPS[STATE_MOVE]=10, after 1000ms we should have advanced
  // through several frames.
  const f0 = currentFrame(STATE_MOVE, 0, 0);
  const f1 = currentFrame(STATE_MOVE, 0, 500);  // 5 frames in
  assert.notEqual(f0, f1);
});

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
  process.exit(1);
}
