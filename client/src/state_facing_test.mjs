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

await test('dyingAt set when client receives EventDeath', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  // Spawn the unit alive
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  // Issue #28 — death is now event-driven, not detected via HP diff.
  // The server emits an enriched EventDeath payload with the unit's
  // position at the moment of death.  HP may or may not be 0 in the
  // same snapshot; the event is the authoritative trigger.
  mockTime = 2000;
  sm.applySnapshot(2, 1, [], [{
    type: 1 /* EVENT_DEATH */,
    entityID: 1,
    x: 0, y: 0, // fixed-point (0,0) — irrelevant for this test
    tick: 2,
  }]);
  sm.update(mockTime);
  const u = sm.getUnit(1);
  assert.ok(u.dyingAt > 0, `expected dyingAt > 0 after EventDeath, got ${u.dyingAt}`);
  assert.equal(u.alive, false, 'unit should be marked not alive');
});

await test('EventDeath is idempotent — second event does not reset dyingAt', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  // First death event
  mockTime = 2000;
  sm.applySnapshot(2, 1, [], [{
    type: 1, entityID: 1, x: 0, y: 0, tick: 2,
  }]);
  sm.update(mockTime);
  const firstDeath = sm.getUnit(1).dyingAt;
  assert.ok(firstDeath > 0, 'first EventDeath should set dyingAt');
  // Second death event arrives in a later snapshot — must NOT reset
  mockTime = 2500;
  sm.applySnapshot(3, 2, [], [{
    type: 1, entityID: 1, x: 0, y: 0, tick: 3,
  }]);
  sm.update(mockTime);
  const secondDeath = sm.getUnit(1).dyingAt;
  assert.equal(secondDeath, firstDeath, 'second EventDeath must not reset dyingAt');
});

await test('EventDeath snaps render position to authoritative death location', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  // Unit is alive at origin
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  // Unit has moved to (10, 20) in the most recent snapshot
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 10, y: 20, hp: 50, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 100);
  // Server says the unit actually died at fixed-point (5*4096, 8*4096)
  // = world (5, 8).  The interpolated render position (somewhere
  // between (0,0) and (10,20)) would have overshot/undershot — the
  // event payload is authoritative and should snap render pos to (5,8).
  mockTime = 2000;
  const FIXED_ONE = 1 << 12;
  sm.applySnapshot(3, 2, [], [{
    type: 1, entityID: 1,
    x: 5 * FIXED_ONE, y: 8 * FIXED_ONE,
    tick: 3,
  }]);
  sm.update(mockTime);
  const u = sm.getUnit(1);
  assert.equal(u.currX, 5, `currX snapped to death X (got ${u.currX})`);
  assert.equal(u.currY, 8, `currY snapped to death Y (got ${u.currY})`);
  assert.equal(u.prevX, 5, 'prevX also snapped (no residual interp drift)');
  assert.equal(u.prevY, 8, 'prevY also snapped');
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
  // Kill via event (not HP diff)
  mockTime = 2000;
  sm.applySnapshot(2, 1, [], [{
    type: 1, entityID: 1, x: 0, y: 0, tick: 2,
  }]);
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
// Issue #48 — attack-fire event + facing dithering
// ---------------------------------------------------------------------------

const EVENT_PROJECTILE = 4;
const ATTACK_DURATION_MS = (3 / 14) * 1000; // mirrors state.js

await test('Issue #48: attack-fire event stamps attackTriggeredAt on the attacker', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 7, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 5, y: 5, hp: 100, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  const before = sm.getUnit(7).attackTriggeredAt;
  assert.equal(before, 0, 'attackTriggeredAt starts at 0');
  // Fire event mid-tick
  mockTime = 1150;
  sm.applySnapshot(2, 1, [], [{
    type: EVENT_PROJECTILE, entityID: 7, tick: 2,
  }]);
  sm.update(mockTime);
  const after = sm.getUnit(7).attackTriggeredAt;
  assert.equal(after, 1150, `attackTriggeredAt should be the arrival time, got ${after}`);
});

await test('Issue #48: attack-fire event for unknown entity is a no-op', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [], [{
    type: EVENT_PROJECTILE, entityID: 999, tick: 1,
  }]);
  sm.update(mockTime);
  // Should not throw, should not create the unit.
  assert.equal(sm.getUnit(999), undefined, 'unknown attacker must not be materialised');
});

await test('Issue #48: facing locks for the duration of an attack swing', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  // Spawn + commit an initial eastward facing.
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 1, 'prerequisite: moving east → DIR_E');
  // Fire attack event — locks facing.
  mockTime = 1150;
  sm.applySnapshot(3, 2, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 5, // big southward delta that would normally flip facing to S
  }], [{
    type: EVENT_PROJECTILE, entityID: 1, tick: 3,
  }]);
  sm.update(mockTime);
  shiftTime(sm, 50); // well within ATTACK_DURATION_MS (~214ms)
  assert.equal(u.facing, 1, `facing must stay DIR_E during swing, got ${u.facing}`);
  // After the swing elapses, the next movement delta can update facing again.
  mockTime = 1150 + Math.ceil(ATTACK_DURATION_MS) + 50;
  sm.applySnapshot(4, 3, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 10,
  }], []);
  sm.update(mockTime);
  shiftTime(sm, 50);
  assert.equal(u.facing, 0, `facing releases to DIR_S after swing, got ${u.facing}`);
});

await test('Issue #48: axis-lock hysteresis prevents diagonal-flip dithering', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  // Spawn at origin, commit eastward facing first.
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 0, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 1, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 1, 'prerequisite: committed to DIR_E');

  // Now walk a noisy near-diagonal: dx and dy nearly equal, oscillating
  // which axis is marginally larger.  Without hysteresis, facing flips
  // E/N every snapshot.  With hysteresis, vertical must beat horizontal
  // by 1.5× before switching.
  let changes = 0;
  let prev = u.facing;
  // Snapshots: x grows by 0.10 each tick; y grows alternately by 0.09 / 0.11.
  // dy never exceeds dx * 1.5, so facing should stay DIR_E throughout.
  let x = 1, y = 0;
  for (let t = 3; t <= 12; t++) {
    x += 0.10;
    y += (t % 2 === 0) ? 0.09 : 0.11;
    mockTime += 100;
    sm.applySnapshot(t, t - 1, [{
      entityID: 1, changedMask: CHANGED_POSITION,
      x, y, unitType: 0, team: 0,
    }], []);
    sm.update(mockTime);
    shiftTime(sm, 50);
    if (u.facing !== prev) { changes++; prev = u.facing; }
  }
  assert.equal(changes, 0, `expected 0 facing flips on near-diagonal, got ${changes} (final facing ${u.facing})`);
});

await test('Issue #48: axis switches only when the other axis wins by 1.5×', async () => {
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
    x: 1, y: 0, unitType: 0, team: 0,
  }], []);
  shiftTime(sm, 50);
  const u = sm.getUnit(1);
  assert.equal(u.facing, 1, 'prerequisite: DIR_E');
  // Sub-ratio vertical movement: dy = 1.0, dx = 1.0 → 1.0 not > 1.0*1.5.
  mockTime += 100;
  sm.applySnapshot(3, 2, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 2, y: 1, unitType: 0, team: 0,
  }], []);
  sm.update(mockTime);
  shiftTime(sm, 50);
  assert.equal(u.facing, 1, `equal-magnitude delta keeps DIR_E, got ${u.facing}`);
  // Clear vertical win: dy = 2.0, dx = 0.2 → 2.0 > 0.2 * 1.5.
  mockTime += 100;
  sm.applySnapshot(4, 3, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 2.2, y: 3, unitType: 0, team: 0,
  }], []);
  sm.update(mockTime);
  shiftTime(sm, 50);
  assert.equal(u.facing, 0, `clear vertical win flips to DIR_S, got ${u.facing}`);
});

// ---------------------------------------------------------------------------
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
