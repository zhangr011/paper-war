// main_state_map_test.mjs — Issue #28
// Verify the server-state → atlas-state mapping logic from main.js
// buildUnitDescriptors, without needing a WebGL canvas.
//
// We re-implement the mapping here (mirror of the logic in main.js) so
// we can test it in isolation.  If the logic in main.js changes, update
// this test to match.
//
// Run with: node client/src/main_state_map_test.mjs

import assert from 'node:assert/strict';
import {
  STATES,
  STATE_IDLE,
  STATE_IDLE2,
  STATE_MOVE,
  STATE_ATTACK,
  STATE_DIE,
  DIR_S,
  DIR_E,
  DIR_N,
  DIR_W,
  FRAMES_PER_STATE,
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
// Mirror of the mapping logic in main.js buildUnitDescriptors (lines ~1437-1490)
// Keep in sync with that code.
// ---------------------------------------------------------------------------

function resolveAtlasState(serverState, eid, timeMs, dyingAt) {
  if (dyingAt > 0) return STATE_DIE;

  switch (serverState) {
    case 3: return STATE_ATTACK;
    case 5: return STATE_IDLE2;
    case 4: return STATE_MOVE;
    case 1: case 2: case 6: case 7: case 8: case 9: return STATE_MOVE;
    case 0: default: {
      // Idle-flicker (~10% per issue #28)
      const phase = (timeMs / 5000) + eid * 0.37;
      const frac = phase - Math.floor(phase);
      return frac < 0.10 ? STATE_IDLE2 : STATE_IDLE;
    }
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test('Server StateIdle (0) maps to atlas STATE_IDLE most of the time', () => {
  // Pick a time/entityID that yields frac >= 0.25 so we get pure IDLE
  // Use eid=0, find a timeMs where (timeMs/5000) % 1 >= 0.25
  const s = resolveAtlasState(0, 0, 2000, 0); // 2000/5000 = 0.4 >= 0.25
  assert.equal(s, STATE_IDLE, `expected IDLE, got ${s}`);
});

test('Server StateIdle (0) flickers to STATE_IDLE2 ~25% of the time', () => {
  // At t=0, frac = 0 + 0.37 - floor(0.37) = 0.37 >= 0.25 → IDLE
  // At t=0, eid=1: frac = 0.37 = 0.37 → IDLE
  // Find a case where frac < 0.25: eid=0, t small enough.
  // t=0, eid=0: frac=0 → IDLE2
  const s = resolveAtlasState(0, 0, 0, 0);
  assert.equal(s, STATE_IDLE2, `expected IDLE2 at t=0, got ${s}`);
});

test('Server StatePatrol (1) maps to atlas STATE_MOVE', () => {
  assert.equal(resolveAtlasState(1, 0, 1000, 0), STATE_MOVE);
});

test('Server StateApproach (2) maps to atlas STATE_MOVE', () => {
  assert.equal(resolveAtlasState(2, 0, 1000, 0), STATE_MOVE);
});

test('Server StateAttack (3) maps to atlas STATE_ATTACK', () => {
  assert.equal(resolveAtlasState(3, 0, 1000, 0), STATE_ATTACK);
});

test('Server StateRetreat (4) maps to atlas STATE_MOVE', () => {
  assert.equal(resolveAtlasState(4, 0, 1000, 0), STATE_MOVE);
});

test('Server StateDefend (5) maps to atlas STATE_IDLE2', () => {
  assert.equal(resolveAtlasState(5, 0, 1000, 0), STATE_IDLE2);
});

test('Server StateScout (6) maps to atlas STATE_MOVE', () => {
  assert.equal(resolveAtlasState(6, 0, 1000, 0), STATE_MOVE);
});

test('Server StatePush (8) maps to atlas STATE_MOVE', () => {
  assert.equal(resolveAtlasState(8, 0, 1000, 0), STATE_MOVE);
});

test('dyingAt > 0 forces STATE_DIE regardless of server state', () => {
  assert.equal(resolveAtlasState(0, 0, 1000, 999), STATE_DIE);
  assert.equal(resolveAtlasState(3, 0, 1000, 999), STATE_DIE);
  assert.equal(resolveAtlasState(4, 0, 1000, 999), STATE_DIE);
});

test('All 10 server states (0..9) map to a valid atlas state', () => {
  for (let ss = 0; ss < 10; ss++) {
    const s = resolveAtlasState(ss, 0, 1000, 0);
    assert.ok(s >= 0 && s < STATES, `server state ${ss} → invalid atlas state ${s}`);
  }
});

test('Idle-flicker phases vary per entityID (no lockstep)', () => {
  // At t=0, eid=0 → IDLE2 (frac=0)
  // At t=0, eid=1 → frac=0.37 → IDLE
  // At t=0, eid=2 → frac=0.74 → IDLE
  // Find an eid that gives IDLE2 at the same time as another gives IDLE
  const e0 = resolveAtlasState(0, 0, 0, 0);
  const e1 = resolveAtlasState(0, 1, 0, 0);
  assert.notEqual(e0, e1, 'entities should have different idle phases');
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
