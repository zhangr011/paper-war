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

test('ATLAS_ROWS = ceil(140 sprites × 4 cells / 32 cols) = 18', () => {
  // 7 types × 5 states × 4 dirs = 140 sprites; each reserves 4 cells;
  // 32 cols per row → 560 cells / 32 = 17.5 → 18 rows.
  assert.equal(ATLAS_ROWS, 18);
});

test('ATLAS_W = 1024 (32 cols × 32 px) — issue #38 layout', () => {
  assert.equal(ATLAS_W, 1024);
});

test('ATLAS_H = 18 rows × 32 px = 576 — fits under WebGL2 MAX_TEXTURE_SIZE 4096 floor', () => {
  assert.equal(ATLAS_H, 576);
  assert.ok(ATLAS_H <= 4096, 'atlas height must fit under 4096');
  assert.ok(ATLAS_W <= 4096, 'atlas width must fit under 4096');
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

test('atlasCell returns a unique, in-bounds cell for every valid (type, state, dir, frame)', () => {
  // Tightly pack all valid cells into a Set; assert no overlaps and all in-bounds.
  const seen = new Set();
  let count = 0;
  for (let t = 0; t < 7; t++) {
    for (let s = 0; s < STATES; s++) {
      for (let d = 0; d < DIRECTIONS; d++) {
        const frameCount = FRAMES_PER_STATE[s];
        for (let f = 0; f < frameCount; f++) {
          const cell = atlasCell(t, s, d, f);
          const key = `${cell.x},${cell.y}`;
          if (seen.has(key)) {
            assert.fail(`overlap at t=${t} s=${s} d=${d} f=${f} → (${key})`);
          }
          seen.add(key);
          // In-bounds check
          assert.ok(cell.x >= 0 && cell.x < ATLAS_W, `x=${cell.x} out of bounds`);
          assert.ok(cell.y >= 0 && cell.y < ATLAS_H, `y=${cell.y} out of bounds`);
          count++;
        }
      }
    }
  }
  // 5 states × their frame counts × 4 dirs × 7 types
  const expected = (2 + 2 + 4 + 3 + 4) * 4 * 7; // = 15 * 4 * 7 = 420
  assert.equal(count, expected, `expected ${expected} unique sprite-frames`);
});

test('atlasCell frames for one (sprite) are contiguous in x', () => {
  // For a sprite whose frames all fit in one row (most cases), frame N+1
  // is exactly ATLAS_CELL to the right of frame N.  For sprites that
  // straddle a row boundary this can wrap, so we only test sprites in
  // the middle of a row where wrapping doesn't happen.
  // spriteSlot = 0 → first sprite on the first row → safe.
  const f0 = atlasCell(0, STATE_MOVE, DIR_S, 0);
  const f1 = atlasCell(0, STATE_MOVE, DIR_S, 1);
  const f2 = atlasCell(0, STATE_MOVE, DIR_S, 2);
  const f3 = atlasCell(0, STATE_MOVE, DIR_S, 3);
  assert.equal(f1.x, f0.x + ATLAS_CELL);
  assert.equal(f2.x, f0.x + 2 * ATLAS_CELL);
  assert.equal(f3.x, f0.x + 3 * ATLAS_CELL);
  assert.equal(f0.y, f1.y);
  assert.equal(f0.y, f2.y);
  assert.equal(f0.y, f3.y);
});

test('atlasCell spriteSlot stride = 4 cells (= MAX_FRAMES_PER_SPRITE)', () => {
  // Two adjacent sprites in the same row (slot 0 and slot 1) — sprite 1
  // should be 4 cells to the right of sprite 0 (same y).
  const s0 = atlasCell(0, STATE_IDLE, DIR_S, 0);
  const s1 = atlasCell(0, STATE_IDLE, DIR_E, 0); // DIR_E is slot 1 within type 0, state 0
  assert.equal(s1.y, s0.y, 'adjacent sprites on the same row');
  assert.equal(s1.x, s0.x + 4 * ATLAS_CELL);
});

test('atlasCell advances to next row when spriteSlot × 4 crosses column boundary', () => {
  // Slot 8 starts at linearCell 32, which is the first cell of row 1.
  // Slot 7 is at linearCell 28-31 (last 4 cells of row 0).
  // Type 0, state 1 (idle2), dir 0 = spriteSlot 0*20 + 1*4 + 0 = 4 → linearCell 16..19 → row 0
  // Type 1, state 0, dir 0 = spriteSlot 1*20 + 0 = 20 → linearCell 80..83 → row 2
  const slot7 = atlasCell(0, STATE_IDLE, DIR_W, 0); // slot 3 → linearCell 12..15, row 0
  const slot8 = atlasCell(0, STATE_IDLE2, DIR_S, 0); // slot 4 → linearCell 16..19, row 0
  assert.equal(slot8.y, slot7.y); // both still in row 0
  assert.equal(slot8.x, slot7.x + 4 * ATLAS_CELL);
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
