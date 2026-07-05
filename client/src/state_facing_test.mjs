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
const ATTACK_FREEZE_MS = 500; // mirrors state.js (#52 plant window)

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
  shiftTime(sm, 50); // well within ATTACK_FREEZE_MS (500ms)
  assert.equal(u.facing, 1, `facing must stay DIR_E during swing, got ${u.facing}`);
  // After the plant window elapses, the next movement delta can update facing again.
  mockTime = 1150 + Math.ceil(ATTACK_FREEZE_MS) + 50;
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
// Issue #51 — dead units must not persist on the map
// ---------------------------------------------------------------------------

const C_POS_51 = 1, C_HP_51 = 8; // CHANGED_POSITION, CHANGED_HP

await test('Issue #51: HP=0 snapshot with no death event still kills the unit', async () => {
  const sm = new StateManager();
  mockTime = 100;
  sm.applySnapshot(1, 0, [{
    entityID: 51, changedMask: C_POS_51 | C_HP_51,
    x: 5, y: 5, hp: 100, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  // Snapshot delivers HP=0 with NO death event
  mockTime = 200;
  sm.applySnapshot(2, 1, [{
    entityID: 51, changedMask: C_HP_51, hp: 0,
  }], []);
  sm.update(mockTime);
  const u = sm.getUnit(51);
  assert.equal(u.alive, false, 'alive must flip to false on HP=0');
  assert.ok(u.dyingAt > 0, 'dyingAt must be stamped so the die animation fires');
  assert.equal(sm.getRenderUnits().filter((x) => x.entityID === 51).length, 1,
    'unit still rendered inside the fade window');
  // Past the fade window — must be removed.
  mockTime = 200 + 800;
  sm.getRenderUnits(); // triggers the delete of faded units
  assert.equal(sm.getRenderUnits().filter((x) => x.entityID === 51).length, 0,
    'unit must be removed after DEATH_FADE_MS');
});

await test('Issue #51: EventDeath after HP=0 does not reset dyingAt (idempotent)', async () => {
  const sm = new StateManager();
  mockTime = 100;
  sm.applySnapshot(1, 0, [{
    entityID: 52, changedMask: C_POS_51 | C_HP_51,
    x: 0, y: 0, hp: 100, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  mockTime = 200;
  sm.applySnapshot(2, 1, [{ entityID: 52, changedMask: C_HP_51, hp: 0 }], []);
  sm.update(mockTime);
  const firstDyingAt = sm.getUnit(52).dyingAt;
  assert.ok(firstDyingAt > 0, 'prerequisite: HP=0 path stamped dyingAt');
  // Death event arrives a snapshot later
  mockTime = 300;
  sm.applySnapshot(3, 2, [], [{ type: 1, entityID: 52, x: 0, y: 0, tick: 3 }]);
  sm.update(mockTime);
  assert.equal(sm.getUnit(52).dyingAt, firstDyingAt,
    'late EventDeath must not reset dyingAt');
});

await test('Issue #51: trailing snapshot for a deleted unit does not resurrect it (hp>0)', async () => {
  const sm = new StateManager();
  mockTime = 100;
  sm.applySnapshot(1, 0, [{
    entityID: 53, changedMask: C_POS_51 | C_HP_51,
    x: 1, y: 1, hp: 50, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 100);
  // Kill via the canonical event path
  mockTime = 200;
  sm.applySnapshot(2, 1, [], [{ type: 1, entityID: 53, x: 1, y: 1, tick: 2 }]);
  sm.update(mockTime);
  // Wait past the fade window so getRenderUnits deletes it
  mockTime = 200 + 800;
  sm.getRenderUnits();
  assert.equal(sm.getUnit(53), undefined, 'prerequisite: unit deleted after fade');
  // A trailing/out-of-order snapshot arrives with the entity alive
  mockTime = 1100;
  sm.applySnapshot(3, 2, [{
    entityID: 53, changedMask: C_POS_51 | C_HP_51,
    x: 2, y: 2, hp: 30, unitType: 0, team: 1,
  }], []);
  sm.update(mockTime);
  assert.equal(sm.getUnit(53), undefined, 'trailing snapshot must not recreate the dead unit');
  assert.equal(sm.getRenderUnits().filter((x) => x.entityID === 53).length, 0,
    'dead unit must not reappear in the render list');
});

await test('Issue #51: trailing snapshot carrying hp<=0 for a deleted unit does not resurrect it', async () => {
  const sm = new StateManager();
  mockTime = 100;
  sm.applySnapshot(1, 0, [{
    entityID: 54, changedMask: C_POS_51 | C_HP_51,
    x: 1, y: 1, hp: 50, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 100);
  mockTime = 200;
  sm.applySnapshot(2, 1, [], [{ type: 1, entityID: 54, x: 1, y: 1, tick: 2 }]);
  sm.update(mockTime);
  mockTime = 200 + 800;
  sm.getRenderUnits();
  assert.equal(sm.getUnit(54), undefined, 'prerequisite: unit deleted after fade');
  mockTime = 1100;
  sm.applySnapshot(3, 2, [{
    entityID: 54, changedMask: C_POS_51 | C_HP_51,
    x: 9, y: 9, hp: 0, unitType: 0, team: 1,
  }], []);
  sm.update(mockTime);
  assert.equal(sm.getUnit(54), undefined,
    'trailing hp=0 snapshot must not recreate the dead unit');
});

await test('Issue #51: canonical event-death path is unchanged', async () => {
  const sm = new StateManager();
  mockTime = 100;
  sm.applySnapshot(1, 0, [{
    entityID: 55, changedMask: C_POS_51 | C_HP_51,
    x: 1, y: 1, hp: 50, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 100);
  mockTime = 200;
  sm.applySnapshot(2, 1, [], [{ type: 1, entityID: 55, x: 1, y: 1, tick: 2 }]);
  sm.update(mockTime);
  const u = sm.getUnit(55);
  assert.equal(u.alive, false, 'event-death flips alive to false');
  assert.ok(u.dyingAt > 0, 'event-death stamps dyingAt');
  assert.equal(sm.getRenderUnits().filter((x) => x.entityID === 55).length, 1,
    'event-death unit still rendered inside the fade window');
});

// ---------------------------------------------------------------------------
// Issue #52 — units plant during the attack swing, then resume moving
// ---------------------------------------------------------------------------

const EVENT_PROJECTILE_52 = 4;

await test('Issue #52: render position freezes for the attack swing then resumes', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 60, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 100, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100); // snap1 active, t=1.0
  // Snap2 moves the unit to x=1.
  sm.applySnapshot(2, 1, [{
    entityID: 60, changedMask: CHANGED_POSITION, x: 1, y: 0,
  }], []);
  shiftTime(sm, 100); // elapsed≥tickDuration → snap2 activates, t=0
  shiftTime(sm, 50);  // t=0.5 → renderX = 0.5
  const beforeSwingX = sm.getUnit(60).renderX;
  assert.ok(beforeSwingX > 0, `prerequisite: unit has interpolated forward (got ${beforeSwingX})`);

  // Fire attack event — freeze begins.
  mockTime += 10;
  sm.applySnapshot(3, 2, [], [{ type: EVENT_PROJECTILE_52, entityID: 60, tick: 3 }]);
  sm.update(mockTime);
  const frozenAt = sm.getUnit(60).renderX;

  // Time advances WITHIN the swing window. The unit would normally keep
  // interpolating toward x=1, but the freeze must hold renderX.
  mockTime += 100;
  sm.update(mockTime);
  assert.equal(sm.getUnit(60).renderX, frozenAt,
    'renderX must not advance during the attack swing');

  // Past ATTACK_FREEZE_MS — the freeze releases and interpolation
  // resumes (catching up via the accelerated-correction path).
  mockTime += Math.ceil(ATTACK_FREEZE_MS) + 200;
  sm.update(mockTime);
  const afterSwingX = sm.getUnit(60).renderX;
  assert.ok(afterSwingX > frozenAt,
    `renderX must resume advancing after the swing (frozen=${frozenAt}, after=${afterSwingX})`);
});

// ---------------------------------------------------------------------------
// Issue #52 follow-up — no teleport on attack-freeze release
// ---------------------------------------------------------------------------

const ATTACK_FREEZE_MS_52 = 500;
const ATTACK_CATCHUP_MS_52 = 250;

await test('Issue #52: unit does not teleport when the attack freeze releases (small delta)', async () => {
  const sm = new StateManager();
  mockTime = 1000;
  sm.applySnapshot(1, 0, [{
    entityID: 70, changedMask: CHANGED_POSITION | CHANGED_HP,
    x: 0, y: 0, hp: 100, unitType: 0, team: 1,
  }], []);
  shiftTime(sm, 0);
  shiftTime(sm, 100);
  sm.applySnapshot(2, 1, [{ entityID: 70, changedMask: CHANGED_POSITION, x: 1, y: 0 }], []);
  shiftTime(sm, 100);
  shiftTime(sm, 50); // renderX ≈ 0.5
  // Fire → freeze.
  mockTime += 10;
  sm.applySnapshot(3, 2, [], [{ type: EVENT_PROJECTILE_52, entityID: 70, tick: 3 }]);
  sm.update(mockTime);
  const frozenAt = sm.getUnit(70).renderX;

  // During the freeze, server advances the unit by a SMALL amount (under
  // CORRECTION_THRESHOLD). Pre-fix this would snap on release.
  mockTime += 100;
  sm.applySnapshot(4, 3, [{ entityID: 70, changedMask: CHANGED_POSITION, x: 1.4, y: 0 }], []);
  sm.update(mockTime);
  // Advance through the rest of the freeze window.
  mockTime = mockTime + (ATTACK_FREEZE_MS_52 - (mockTime - (frozenAt !== undefined ? mockTime : mockTime)));
  // Drive well past ATTACK_FREEZE_MS but inside the catch-up window.
  // Recompute from the fire timestamp: fire happened at the snapshot-3
  // apply time; we just need to land just past ATTACK_FREEZE_MS.
  while (mockTime < 1000 + 100 + 100 + 50 + 10 + ATTACK_FREEZE_MS_52 + 10) {
    mockTime += 30;
    sm.update(mockTime);
  }
  const justAfterRelease = sm.getUnit(70).renderX;
  // Drive a few more frames through the catch-up window.
  const samples = [justAfterRelease];
  for (let i = 0; i < 4; i++) {
    mockTime += 50;
    sm.update(mockTime);
    samples.push(sm.getUnit(70).renderX);
  }
  // The first post-release sample must NOT equal the final target — i.e.
  // the unit did not teleport to the live position in a single frame.
  const finalTarget = samples[samples.length - 1];
  const firstDelta = Math.abs(samples[0] - frozenAt);
  const totalDelta = Math.abs(finalTarget - frozenAt);
  assert.ok(firstDelta < totalDelta,
    `first-frame delta (${firstDelta.toFixed(3)}) must be < total catch-up delta (${totalDelta.toFixed(3)}) — unit teleported on release`);
  // And the samples must be monotonically progressing (smooth blend, no snap).
  for (let i = 1; i < samples.length; i++) {
    assert.ok(samples[i] >= samples[i-1] - 0.001,
      `renderX went backwards (${samples[i-1]} → ${samples[i]}) — non-monotonic catch-up`);
  }
});

// ---------------------------------------------------------------------------
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
