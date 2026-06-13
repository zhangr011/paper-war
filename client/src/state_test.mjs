// state_test.mjs — Tests for jitter-buffered interpolation (ADR-0016)
// Run with: node client/src/state_test.mjs
//
// Tests the StateManager's core interpolation logic:
//   - Tick rate (10Hz, not legacy 5Hz)
//   - Basic prev→curr linear interpolation
//   - Jitter buffer: early-arriving snapshots queue, don't cause jumps
//   - Velocity extrapolation with decay
//   - Accelerated correction for large position deltas
//   - Out-of-order/duplicate snapshot rejection
//   - New unit initialization (prev = curr = initial)

import assert from 'node:assert/strict';
import { StateManager } from './state.js';

// ---------------------------------------------------------------------------
// Mock performance.now() so we control time precisely.
// _activateSnapshot() calls performance.now() internally; update() takes
// `now` as a parameter. Both must use the same time source.
// ---------------------------------------------------------------------------
let mockTime = 0;
globalThis.performance = { now: () => mockTime };

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    console.log(`  ✓ ${name}`);
    passed++;
  } catch (e) {
    console.error(`  ✗ ${name}`);
    console.error(`    ${e.message}`);
    failed++;
  }
}

// ChangedMask bits (must match state.js)
const CHANGED_POSITION  = 1 << 0;
const CHANGED_VELOCITY  = 1 << 1;
const CHANGED_ANGLE     = 1 << 2;
const CHANGED_HP        = 1 << 3;

// Fixed-point conversion (server uses int64 with 12-bit fraction)
const FIXED_ONE = 1 << 12;

function fixed(f) { return Math.round(f * FIXED_ONE); }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

console.log('StateManager interpolation tests\n');

// --- Tick rate ---

test('tick duration is 100ms (10Hz), not legacy 200ms (5Hz)', () => {
  const sm = new StateManager();
  assert.equal(sm.tickDuration, 100, 'tickDuration should be 100ms at 10Hz');
});

// --- Basic interpolation ---

test('unit interpolates linearly from prev to curr', () => {
  const sm = new StateManager();

  // Tick 1: unit appears at x=10.0 (realistic speed: 0.5 u/tick)
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 10.0, y: 0.0,
  }], null, null);

  // First snapshot activates immediately
  mockTime = 100;
  sm.update(100);
  const unit = sm.getUnit(1);
  assert.ok(unit, 'unit should exist');
  assert.ok(Math.abs(unit.renderX - 10.0) < 0.01, 'unit at prev position after activation');

  // Tick 2: unit moves to x=10.5 (goes into pending queue)
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION,
    x: 10.5, y: 0.0,
  }], null, null);

  // At 200ms: elapsed since activation (100ms) = 100ms >= tick → activate tick 2
  mockTime = 200;
  sm.update(200);

  // Now interpolating 10→10.5. At t=0 (just activated), should be at 10
  assert.ok(Math.abs(unit.renderX - 10.0) < 0.01,
    `unit at prev (10.0) right after activation, got ${unit.renderX}`);

  // 50ms later: t=0.5, should be at midpoint (10.25)
  mockTime = 250;
  sm.update(250);
  assert.ok(Math.abs(unit.renderX - 10.25) < 0.05,
    `unit at midpoint (10.25) at t=0.5, got ${unit.renderX}`);
});

// --- Jitter buffer ---

test('early-arriving snapshot queues, does not jump', () => {
  const sm = new StateManager();

  // Tick 1: unit at x=0 (realistic speed: 0.5 u/tick)
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100); // activate tick 1

  // Tick 2: unit moves to x=0.5
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.5, y: 0.0,
  }], null, null);

  // Tick 2 arrives early (50ms into the interpolation window).
  // Snapshot should NOT activate yet — it sits in the queue.
  mockTime = 150;
  sm.update(150);
  const unit = sm.getUnit(1);
  assert.equal(sm.pendingQueue.length, 1, 'snapshot should be queued');
  assert.equal(sm.currTick, 1, 'currTick still 1 (not activated)');

  // Wait until t≥1.0 (elapsed >= 100ms since last activation)
  mockTime = 200; // 200 - 100 = 100ms elapsed → activate tick 2
  sm.update(200);
  assert.equal(sm.currTick, 2, 'currTick now 2 after activation');
  assert.equal(sm.pendingQueue.length, 0, 'queue drained');

  // Unit should be at prev position (0.0), not jumped to 0.5
  assert.ok(Math.abs(unit.renderX - 0.0) < 0.05,
    `unit at prev (0.0) after activation, not jumped, got ${unit.renderX}`);
});

// --- Extrapolation with velocity decay ---

test('extrapolation uses velocity with linear decay', () => {
  const sm = new StateManager();

  // Tick 1: unit at x=0, velocity 0.5/tick (realistic speed)
  sm.applySnapshot(1, 0, [{
    entityID: 1,
    changedMask: CHANGED_POSITION | CHANGED_VELOCITY,
    x: 0.0, y: 0.0, vx: 0.5, vy: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  // Tick 2: unit at x=0.5 (moved by velocity), same velocity
  sm.applySnapshot(2, 1, [{
    entityID: 1,
    changedMask: CHANGED_POSITION | CHANGED_VELOCITY,
    x: 0.5, y: 0.0, vx: 0.5, vy: 0.0,
  }], null, null);

  // Activate tick 2 at 200ms
  mockTime = 200;
  sm.update(200);

  // At 250ms: t = 50/100 = 0.5, interpolate 0→0.5 = 0.25
  mockTime = 250;
  sm.update(250);
  const unit = sm.getUnit(1);
  assert.ok(Math.abs(unit.renderX - 0.25) < 0.05,
    `t=0.5: x should be 0.25, got ${unit.renderX}`);

  // At 350ms: t = 150/100 = 1.5, extrapolate past curr (0.5)
  // base = lerp(0, 0.5, 1.5) = 0.75. Then velocity: extraT=0.5,
  // velocityScale = 1 - 0.5*(1-0.7) = 0.85
  // delta = 0.5 * 0.5 * 0.85 = 0.21. Total = 0.96
  mockTime = 350;
  sm.update(350);
  assert.ok(unit.renderX > 0.5,
    `t=1.5 (extrapolating): x should be > 0.5, got ${unit.renderX}`);
  assert.ok(unit.renderX < 1.5,
    `t=1.5 (decayed): x should be < 1.5, got ${unit.renderX}`);
});

// --- Accelerated correction ---

test('large position delta triggers accelerated correction', () => {
  const sm = new StateManager();

  // Tick 1: unit at x=0
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  // Simulate a massive desync: unit's renderX is way off (e.g. after reconnect)
  const unit = sm.getUnit(1);
  unit.renderX = 50.0;
  unit.renderY = 0.0;

  // Tick 2: server says unit is at x=1.0 (normal movement)
  sm.applySnapshot(2, 1, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 1.0, y: 0.0,
  }], null, null);
  mockTime = 200;
  sm.update(200); // activate tick 2

  // At t=0: rx = lerp(0, 1, 0) = 0. renderX = 50.
  // dist = 50 >> CORRECTION_THRESHOLD (5.0).
  // correctionT = 0.3. renderX = lerp(50, 0, 0.3) = 35.
  mockTime = 200;
  sm.update(200);
  assert.ok(unit.renderX < 50.0,
    `correction should move renderX toward target (< 50), got ${unit.renderX}`);
  assert.ok(unit.renderX > 0.0,
    `correction should blend, not snap to 0, got ${unit.renderX}`);
});

// --- Out-of-order rejection ---

test('out-of-order snapshot is rejected', () => {
  const sm = new StateManager();

  sm.applySnapshot(5, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  // Tick 3 (older) should be rejected by nextTick check
  sm.applySnapshot(3, 2, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 5.0, y: 0.0,
  }], null, null);
  assert.equal(sm.pendingQueue.length, 0, 'older snapshot should be rejected');
  assert.equal(sm.nextTick, 5, 'nextTick unchanged');
});

test('duplicate tick is rejected', () => {
  const sm = new StateManager();

  sm.applySnapshot(5, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  // Same tick 5 again
  sm.applySnapshot(5, 4, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 9.0, y: 0.0,
  }], null, null);
  assert.equal(sm.pendingQueue.length, 0, 'duplicate tick rejected');
});

// --- New unit initialization ---

test('new unit initializes prev = curr = initial position', () => {
  const sm = new StateManager();

  sm.applySnapshot(1, 0, [{
    entityID: 42, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 15.0, y: 25.0, hp: 100,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  const unit = sm.getUnit(42);
  assert.ok(unit, 'unit created');
  assert.equal(unit.prevX, 15.0, 'prevX = initial');
  assert.equal(unit.currX, 15.0, 'currX = initial');
  assert.equal(unit.renderX, 15.0, 'renderX = initial');
  assert.equal(unit.prevHP, 100, 'prevHP = initial');
  assert.equal(unit.currHP, 100, 'currHP = initial');
});

// --- Events processed immediately ---

test('events are processed immediately on arrival', () => {
  const sm = new StateManager();
  let damageCount = 0;
  sm.onDamage = () => { damageCount++; };

  sm.applySnapshot(1, 0, [], [{
    type: 0, // EVENT_DAMAGE
    data: new Uint8Array(8),
  }], null);

  assert.equal(damageCount, 1, 'damage event processed immediately');
});

// --- Fog updated immediately ---

test('fog grid is updated immediately on arrival', () => {
  const sm = new StateManager();
  const fogData = new Uint8Array([1, 0, 1, 0]);

  sm.applySnapshot(1, 0, [], null, {
    width: 2, height: 2, visible: fogData,
  });

  assert.equal(sm.fogWidth, 2, 'fog width set');
  assert.equal(sm.fogHeight, 2, 'fog height set');
  assert.equal(sm.fogVisible, fogData, 'fog data set');
});

// --- Queue overflow ---

test('queue overflow drops oldest snapshot', () => {
  const sm = new StateManager();

  // First snapshot activates immediately
  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);

  // Queue many without calling update (simulating lag spike)
  for (let i = 2; i <= 6; i++) {
    sm.applySnapshot(i, i - 1, [{
      entityID: 1, changedMask: CHANGED_POSITION,
      x: i * 1.0, y: 0.0,
    }], null, null);
  }

  // MAX_PENDING_SNAPSHOTS = 3, so queue should be capped
  assert.ok(sm.pendingQueue.length <= 3,
    `queue should be capped at 3, got ${sm.pendingQueue.length}`);

  // nextTick should still be the latest
  assert.equal(sm.nextTick, 6, 'nextTick tracks latest');
});

// --- Clear and reset ---

test('clearEntities resets all state including queue', () => {
  const sm = new StateManager();

  sm.applySnapshot(1, 0, [{
    entityID: 1, changedMask: CHANGED_POSITION, x: 0.0, y: 0.0,
  }], null, null);
  mockTime = 100;
  sm.update(100);

  sm.clearEntities();
  assert.equal(sm.units.size, 0, 'units cleared');
  assert.equal(sm.pendingQueue.length, 0, 'queue cleared');
  assert.equal(sm.currTick, 0, 'currTick reset');
  assert.equal(sm.nextTick, 0, 'nextTick reset');
});

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

console.log(`\n${'─'.repeat(50)}`);
console.log(`${passed} passed, ${failed} failed`);
if (failed > 0) {
  process.exit(1);
}
