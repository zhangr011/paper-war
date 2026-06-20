// state_facing_test.mjs — Tests for issue #28 facing + dyingAt on UnitState.
// Run with: node client/src/state_facing_test.mjs

import assert from 'node:assert/strict';
import { StateManager } from './state.js';

let mockTime = 0;
globalThis.performance = { now: () => mockTime };

let passed = 0;
let failed = 0;
function test(name, fn) {
  return Promise.resolve(fn()).then(() => {
    passed++;
    console.log(`  ✓ ${name}`);
  }).catch((err) => {
    failed++;
    console.error(`  ✗ ${name}`);
    console.error(`    ${err.message}`);
  });
}

// ---------------------------------------------------------------------------
// Helpers — build a snapshot with one unit
// ---------------------------------------------------------------------------

const CHANGED_POSITION   = 1;   // 1 << 0
const CHANGED_VELOCITY   = 2;   // 1 << 1
const CHANGED_HP         = 8;   // 1 << 3
const CHANGED_STATE      = 64;  // 1 << 6

function snap(tick, fields = {}) {
  return {
    tick,
    prevTick: tick - 1,
    unitUpdates: [{
      entityID: 1,
      changedMask: fields.mask ?? 0,
      x: fields.x ?? 0,
      y: fields.y ?? 0,
      vx: fields.vx ?? 0,
      vy: fields.vy ?? 0,
      hp: fields.hp ?? 100,
      state: fields.state ?? 0,
      unitType: fields.unitType ?? 0,
      team: fields.team ?? 0,
    }],
    events: [],
  };
}

function shiftTime(sm, ms) {
  mockTime += ms;
  sm.update(mockTime);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

await test('UnitState has facing and dyingAt fields after creation', async () => {
  const sm = new StateManager();
  mockTime = 0;
  sm.applySnapshot(1, 0, [{
    entityID: 1,
    changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 10, y: 20, hp: 100, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  const u = sm.getUnit(1);
  assert.ok(u, 'unit should exist');
  assert.equal(typeof u.facing, 'number', 'facing must be a number');
  assert.equal(typeof u.dyingAt, 'number', 'dyingAt must be a number');
  assert.equal(u.dyingAt, 0, 'dyingAt starts at 0 (alive)');
});

await test('Facing defaults to DIR_S (0) when stationary', async () => {
  const sm = new StateManager();
  mockTime = 1000; // non-zero so lastSnapshotTime != 0 after activation
  sm.applySnapshot(1, 0, [{
    entityID: 1,
    changedMask: CHANGED_POSITION,
    x: 10, y: 10, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100); // advance one tick
  const u = sm.getUnit(1);
  assert.equal(u.facing, 0); // DIR_S
});

await test('Facing computes as DIR_E (1) when moving +x', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  // Spawn at origin
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  // Move east
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50); // mid-tick so render position has shifted
  const u = sm.getUnit(1);
  assert.equal(u.facing, 1, `expected DIR_E (1), got ${u.facing}`);
});

await test('Facing computes as DIR_W (3) when moving -x', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 3, `expected DIR_W (3), got ${u.facing}`);
});

await test('Facing computes as DIR_S (0) when moving +y', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 1, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 0, `expected DIR_S (0), got ${u.facing}`);
});

await test('Facing computes as DIR_N (2) when moving -y', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 1, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 2, `expected DIR_N (2), got ${u.facing}`);
});

await test('dyingAt set when unit HP transitions to 0', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  // Kill it
  mockTime = 2000;
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_HP,
    hp: 0, unitType: 0, team: 0,
  }], []);
  sm.update(mockTime);
  const u = sm.getUnit(1);
  assert.ok(u.dyingAt > 0, `expected dyingAt > 0 after HP→0, got ${u.dyingAt}`);
});

await test('getRenderUnits includes dying units within 600ms window', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  // Kill
  mockTime = 2000;
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_HP,
    hp: 0, unitType: 0, team: 0,
  }], []);
  sm.update(mockTime);
  // Within window
  shiftTime(sm, 300);
  let units = sm.getRenderUnits();
  assert.equal(units.length, 1, 'dying unit should still render within 600ms');
  // After window
  shiftTime(sm, 400); // total 700ms after death
  units = sm.getRenderUnits();
  assert.equal(units.length, 0, 'dying unit should be removed after 600ms');
});

// ---------------------------------------------------------------------------
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
