// particles_test.mjs — Tests for the combat particle system (Issue #37)
// Run with: node --test client/src/particles_test.mjs
//
// Covers pool lifecycle, LRU overflow, processEvents spawning, update
// integration, and reset.

import assert from 'node:assert/strict';
import {
  ParticleSystem,
  MAX_PARTICLES,
  PARTICLE_TYPES,
} from './particles.js';

let passed = 0, failed = 0;
function test(name, fn) {
  try { fn(); passed++; console.log(`  ✓ ${name}`); }
  catch (e) { failed++; console.error(`  ✗ ${name}\n    ${e.message}`); }
}

// ---------------------------------------------------------------------------

console.log('ParticleSystem tests (Issue #37)\n');

test('new system starts empty with activeCount=0', () => {
  const ps = new ParticleSystem(16);
  assert.equal(ps.activeCount, 0);
  ps.forEachActive(() => { throw new Error('no particles should be active'); });
});

test('spawn activates one particle', () => {
  const ps = new ParticleSystem(16);
  ps.spawn(PARTICLE_TYPES.DUST_PUFF, 5, 7);
  assert.equal(ps.activeCount, 1);
  let seen = null;
  ps.forEachActive((p) => { seen = p; });
  assert.equal(seen.type, PARTICLE_TYPES.DUST_PUFF);
  assert.equal(seen.x, 5);
  assert.equal(seen.y, 7);
  assert.ok(seen.life > 0);
});

test('spawn fills pool without allocating new slots', () => {
  const ps = new ParticleSystem(8);
  for (let i = 0; i < 8; i++) {
    ps.spawn(PARTICLE_TYPES.DUST_PUFF, i, 0);
  }
  assert.equal(ps.activeCount, 8);
  // All 8 slots should be active
  const positions = [];
  ps.forEachActive((p) => positions.push(p.x));
  positions.sort((a, b) => a - b);
  assert.deepEqual(positions, [0, 1, 2, 3, 4, 5, 6, 7]);
});

test('overflowing the pool overwrites oldest (LRU via ring cursor)', () => {
  // Spawn 9 particles into a pool of 8. The 9th should overwrite the
  // first-spawned slot (cursor wraps). activeCount stays at 8.
  const ps = new ParticleSystem(8);
  for (let i = 0; i < 9; i++) {
    ps.spawn(PARTICLE_TYPES.DUST_PUFF, i, 0);
  }
  assert.equal(ps.activeCount, 8, 'activeCount caps at pool size');
  const xs = [];
  ps.forEachActive((p) => xs.push(p.x));
  xs.sort((a, b) => a - b);
  // Particle x=0 was overwritten by x=8 (LRU) — values are 1..8.
  assert.deepEqual(xs, [1, 2, 3, 4, 5, 6, 7, 8]);
});

test('processEvents spawns 4 particles on EVENT_DAMAGE (1 flash + 3 sparks)', () => {
  const ps = new ParticleSystem(64);
  // sourceX/sourceY are fixed-point (FractionBits=12). 10 tiles * 4096.
  const events = [{
    type: 0, // EVENT_DAMAGE
    targetID: 0,
    damage: 10,
    sourceX: 10 * 4096,
    sourceY: 20 * 4096,
  }];
  ps.processEvents(events, 10, 20); // camera at (10, 20) — in cull radius
  assert.equal(ps.activeCount, 4, '1 muzzle flash + 3 impact sparks');
  let flashCount = 0, sparkCount = 0;
  ps.forEachActive((p) => {
    if (p.type === PARTICLE_TYPES.MUZZLE_FLASH) flashCount++;
    else if (p.type === PARTICLE_TYPES.IMPACT_SPARK) sparkCount++;
  });
  assert.equal(flashCount, 1);
  assert.equal(sparkCount, 3);
});

test('processEvents spawns 13 particles on EVENT_DEATH (1 flash + 12 smoke)', () => {
  const ps = new ParticleSystem(64);
  const events = [{
    type: 1, // EVENT_DEATH
    entityID: 42,
    x: 5 * 4096,
    y: 5 * 4096,
    tick: 100,
  }];
  ps.processEvents(events, 5, 5);
  assert.equal(ps.activeCount, 13);
  let flashCount = 0, smokeCount = 0;
  ps.forEachActive((p) => {
    if (p.type === PARTICLE_TYPES.DEATH_FLASH) flashCount++;
    else if (p.type === PARTICLE_TYPES.DEATH_SMOKE) smokeCount++;
  });
  assert.equal(flashCount, 1);
  assert.equal(smokeCount, 12);
});

test('processEvents culls events beyond VIEW_CULL_RADIUS', () => {
  const ps = new ParticleSystem(64);
  // Source 100 tiles from camera — well beyond the 30-tile cull radius.
  const events = [{
    type: 0, // EVENT_DAMAGE
    sourceX: 100 * 4096,
    sourceY: 100 * 4096,
  }];
  ps.processEvents(events, 0, 0);
  assert.equal(ps.activeCount, 0, 'event beyond cull radius → no spawns');
});

test('update advances age and deactivates particles past their life', () => {
  const ps = new ParticleSystem(16);
  ps.spawn(PARTICLE_TYPES.MUZZLE_FLASH, 0, 0); // life=0.060
  assert.equal(ps.activeCount, 1);
  // Advance past life
  ps.update(0.030);
  assert.equal(ps.activeCount, 1, 'still alive at half-life');
  ps.update(0.040); // total 0.070 > 0.060 life
  assert.equal(ps.activeCount, 0, 'deactivated past life');
});

test('update integrates velocity and applies gravity to impact sparks', () => {
  const ps = new ParticleSystem(16);
  ps.spawn(PARTICLE_TYPES.IMPACT_SPARK, 0, 0, { vx: 10, vy: -5 });
  let p;
  ps.forEachActive((pp) => { p = pp; });
  // Impact spark life = 0.100s; advance 0.05s (half-life) so particle
  // stays active and we can read post-update state.
  ps.update(0.05);
  // vx constant (no drag), vy increases by gAccel*dt = 9.8*0.05 = 0.49
  assert.ok(Math.abs(p.x - 0.5) < 0.001, `x advanced by vx*dt: ${p.x}`);
  // dy = vy*dt = -5*0.05 = -0.25; vy after = -5 + 0.49 = -4.51
  assert.ok(Math.abs(p.y - (-0.25)) < 0.001, `y advanced by vy*dt: ${p.y}`);
  assert.ok(Math.abs(p.vy - (-4.51)) < 0.001, `vy increased by gravity: ${p.vy}`);
});

test('reset deactivates all particles and resets cursor', () => {
  const ps = new ParticleSystem(16);
  for (let i = 0; i < 5; i++) {
    ps.spawn(PARTICLE_TYPES.DUST_PUFF, i, 0);
  }
  assert.equal(ps.activeCount, 5);
  ps.reset();
  assert.equal(ps.activeCount, 0);
  assert.equal(ps.cursor, 0);
});

test('particle pool reuses slots across spawn/reset cycles (no growth)', () => {
  // Spawn, age out, spawn again — the pool array length must stay constant.
  const ps = new ParticleSystem(16);
  const initialLen = ps.pool.length;
  for (let cycle = 0; cycle < 3; cycle++) {
    for (let i = 0; i < 16; i++) {
      ps.spawn(PARTICLE_TYPES.DUST_PUFF, i, 0);
    }
    ps.update(1.0); // age everything out
    assert.equal(ps.activeCount, 0, `cycle ${cycle}: all aged out`);
  }
  assert.equal(ps.pool.length, initialLen, 'pool array length stable');
});

test('processEvents handles empty and missing events array', () => {
  const ps = new ParticleSystem(16);
  ps.processEvents(null, 0, 0);
  ps.processEvents([], 0, 0);
  assert.equal(ps.activeCount, 0);
});

test('impact sparks spawn with varied velocities (random angle/speed)', () => {
  // Sanity: 3 sparks from one damage event should not all have identical vx/vy.
  const ps = new ParticleSystem(64);
  ps.processEvents([{
    type: 0,
    sourceX: 5 * 4096,
    sourceY: 5 * 4096,
  }], 5, 5);
  const vels = [];
  ps.forEachActive((p) => {
    if (p.type === PARTICLE_TYPES.IMPACT_SPARK) vels.push(`${p.vx.toFixed(2)},${p.vy.toFixed(2)}`);
  });
  assert.equal(vels.length, 3);
  const unique = new Set(vels);
  assert.ok(unique.size >= 2, `expected variation in spark velocities, got ${[...unique]}`);
});

// ---------------------------------------------------------------------------
console.log(`\n${'─'.repeat(50)}`);
console.log(`${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
