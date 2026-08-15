// range_ring_test.mjs — tests for the high-ground range ring math/geometry.
// Run with: node client/src/range_ring_test.mjs
//
// Includes a drift guard: UNIT_RANGES is hand-mirrored from
// server/pkg/component/unit_type.go CombatUnitTypeTable — this test parses
// the Go source literals and fails if the mirror diverges.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import {
  UNIT_RANGES, effectiveRange, elevationAt, ringTriangles,
} from './range_ring.js';

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  ok  ${name}`);
  } catch (err) {
    failed++;
    console.error(`FAIL  ${name}\n      ${err.message}`);
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const goPath = join(here, '..', '..', 'server', 'pkg', 'component', 'unit_type.go');

// --- drift guard -------------------------------------------------------------

test('UNIT_RANGES matches server CombatUnitTypeTable (drift guard)', () => {
  const go = readFileSync(goPath, 'utf8');
  // Parse the table body in order: each entry has "Range: N". The table's
  // map entries are written in CombatUnitType order 0..6 (verified by the
  // "Type: UnitX" lines), so positional matching is sound.
  const body = go.slice(go.indexOf('CombatUnitTypeTable'), go.indexOf('damageMatrix'));
  const typeNames = [...body.matchAll(/Type:\s+(Unit\w+)/g)].map(m => m[1]);
  const ranges = [...body.matchAll(/Range:\s+(\d+)/g)].map(m => parseInt(m[1], 10));
  assert.equal(typeNames.length, 7, `expected 7 types, got ${typeNames.length}`);
  assert.equal(ranges.length, 7, `expected 7 ranges, got ${ranges.length}`);
  assert.deepEqual(
    UNIT_RANGES,
    ranges,
    `UNIT_RANGES drifted from ${goPath}:\n  client: ${UNIT_RANGES}\n  server: ${ranges} (${typeNames.join(', ')})`,
  );
});

// --- effectiveRange ------------------------------------------------------------

test('flat ground: base ring only (max === base)', () => {
  const r = effectiveRange(0, 0);
  assert.deepEqual(r, { base: 5, max: 5 });
});

test('mid elevation (1): +1 tile envelope', () => {
  const r = effectiveRange(1, 1);
  assert.deepEqual(r, { base: 7, max: 8 });
});

test('peak elevation (2): +2 tile envelope (ADR-0029 TestLiveHillAssault case)', () => {
  // LightInfantry base 5 → effective 7 from a peak: the exact numbers the
  // hill-assault integration test exercises server-side.
  const r = effectiveRange(0, 2);
  assert.deepEqual(r, { base: 5, max: 7 });
});

test('negative elevation clamps to base (uphill never shortens)', () => {
  const r = effectiveRange(2, -3);
  assert.deepEqual(r, { base: 8, max: 8 });
});

test('unknown unit type falls back to 5 (LightInfantry)', () => {
  assert.equal(effectiveRange(99, 1).base, 5);
});

// --- elevationAt -----------------------------------------------------------------

test('elevationAt: floor + row-major indexing', () => {
  // 3×2 grid, row-major: row0 = [0,1,2], row1 = [1,2,0]
  const grid = new Uint8Array([0, 1, 2, 1, 2, 0]);
  assert.equal(elevationAt(grid, 3, 2, 0.9, 0.9), 0); // tile (0,0) → idx 0
  assert.equal(elevationAt(grid, 3, 2, 1.1, 0.5), 1); // tile (1,0) → idx 1
  assert.equal(elevationAt(grid, 3, 2, 1.5, 1.5), 2); // tile (1,1) → idx 4
  assert.equal(elevationAt(grid, 3, 2, 2.9, 1.9), 0); // tile (2,1) → idx 5
});

test('elevationAt: out-of-bounds clamps to 0', () => {
  const grid = new Uint8Array([2, 2, 2, 2]);
  assert.equal(elevationAt(grid, 2, 2, -0.5, 1), 0);
  assert.equal(elevationAt(grid, 2, 2, 5, 5), 0);
});

// --- ringTriangles ------------------------------------------------------------------

test('ringTriangles: segment count × 2 triangles × 3 vertices', () => {
  const out = [];
  ringTriangles(0, 0, 50, 4, 32, out);
  assert.equal(out.length, 32 * 2 * 3 * 2); // floats (x,y pairs)
});

test('ringTriangles: all vertices within [r-thickness, r] of center', () => {
  const out = [];
  ringTriangles(100, 100, 60, 6, 16, out);
  for (let i = 0; i < out.length; i += 2) {
    const d = Math.hypot(out[i] - 100, out[i + 1] - 100);
    assert.ok(d <= 60 + 1e-6, `vertex outside ring: d=${d}`);
    assert.ok(d >= 60 - 6 - 1e-6, `vertex inside hole: d=${d}`);
  }
});

test('ringTriangles: closes the loop (first and last angle overlap)', () => {
  const out = [];
  ringTriangles(0, 0, 10, 2, 8, out);
  // The last segment's outer end should be at angle 2π ≈ angle 0.
  const lastOuter = [out[out.length - 2], out[out.length - 1]];
  const d = Math.hypot(lastOuter[0] - 10, lastOuter[1] - 0);
  assert.ok(Math.abs(d) < 1e-6, `ring does not close: endpoint distance ${d}`);
});

test('ringTriangles: zero radius degenerates safely (empty annulus)', () => {
  const out = [];
  ringTriangles(0, 0, 0, 4, 8, out);
  assert.equal(out.length, 8 * 2 * 3 * 2); // still emits geometry — caller guards radius > 0
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
