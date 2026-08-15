// map_core_test.mjs — Tests for the clash map editor's pure logic module.
// Run with: node client/editor/map_core_test.mjs

import assert from 'node:assert/strict';
import {
  GRID, RESERVED_TERRAIN, PROFILE_COSTS, RUNTIME_SPAWNS, RAMP_TERRAIN,
  brushOffsets, rectIndices, floodFill, isConnected, rampElevationAt,
  ZOOM_LEVELS, MIN_ZOOM, MAX_ZOOM,
  screenToTile, tileToScreen, zoomAround, clampPan,
} from './map_core.mjs';

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

// --- brushOffsets -------------------------------------------------------------

test('square brush 1 is exactly the anchor', () => {
  assert.deepEqual(brushOffsets(1, 'square'), [{ dx: 0, dy: 0 }]);
});

test('square brush 3 is a 3×3 block centered on anchor', () => {
  const offs = brushOffsets(3, 'square');
  assert.equal(offs.length, 9);
  assert.ok(offs.some(o => o.dx === -1 && o.dy === -1));
  assert.ok(offs.some(o => o.dx === 1 && o.dy === 1));
  assert.ok(offs.some(o => o.dx === 0 && o.dy === 0));
});

test('square brush 2 is a 2×2 block, anchor at top-left', () => {
  const offs = brushOffsets(2, 'square');
  assert.equal(offs.length, 4);
  for (const o of offs) {
    assert.ok(o.dx === 0 || o.dx === 1);
    assert.ok(o.dy === 0 || o.dy === 1);
  }
});

test('square brush 5 has 25 tiles and spans ±2', () => {
  const offs = brushOffsets(5, 'square');
  assert.equal(offs.length, 25);
  assert.ok(offs.some(o => o.dx === -2 && o.dy === -2));
  assert.ok(offs.some(o => o.dx === 2 && o.dy === 2));
});

test('circle brush 5 is smaller than square 5 (corner cut)', () => {
  const sq = brushOffsets(5, 'square');
  const ci = brushOffsets(5, 'circle');
  assert.ok(ci.length < sq.length);
  assert.ok(ci.length > 9, 'should keep the bulk of the disc');
  // corners cut
  assert.ok(!ci.some(o => Math.abs(o.dx) === 2 && Math.abs(o.dy) === 2));
  // center + orthogonal kept
  assert.ok(ci.some(o => o.dx === 0 && o.dy === 0));
  assert.ok(ci.some(o => o.dx === 2 && o.dy === 0));
});

test('circle brush 1 equals square brush 1', () => {
  assert.deepEqual(brushOffsets(1, 'circle'), brushOffsets(1, 'square'));
});

test('circle brush 3 keeps corners (radius 1.5 > corner distance √2)', () => {
  // Circle only rounds from size 5 up: at size 3 the disc radius (1.5) still
  // covers the corners (√2 ≈ 1.41), so it equals the full 3×3 block.
  assert.deepEqual(brushOffsets(3, 'circle'), brushOffsets(3, 'square'));
});

test('brushOffsets memoizes (same reference)', () => {
  assert.equal(brushOffsets(3, 'square'), brushOffsets(3, 'square'));
});

// --- rectIndices ---------------------------------------------------------------

test('rectIndices forward drag', () => {
  const idx = rectIndices(2, 3, 4, 5);
  assert.equal(idx.length, 3 * 3);
  assert.ok(idx.includes(3 * GRID + 3));
});

test('rectIndices backward drag normalizes', () => {
  const a = rectIndices(4, 5, 2, 3);
  const b = rectIndices(2, 3, 4, 5);
  assert.deepEqual(a, b);
});

test('rectIndices clamps to map bounds', () => {
  const idx = rectIndices(-5, -5, 99, 99);
  assert.equal(idx.length, GRID * GRID);
});

// --- floodFill -------------------------------------------------------------------

function blankArrays() {
  return {
    terrain: new Uint8Array(GRID * GRID),
    elevation: new Uint8Array(GRID * GRID),
  };
}

test('floodFill repaints the connected same-value region', () => {
  const { terrain, elevation } = blankArrays();
  // paint a 3-wide vertical stripe of Forest (4) at x=5
  for (let y = 0; y < GRID; y++) { terrain[y * GRID + 5] = 4; terrain[y * GRID + 6] = 4; terrain[y * GRID + 7] = 4; }
  const changed = floodFill(terrain, elevation, 5, 0, { terrain: 1 });
  assert.equal(changed, 3 * GRID);
  assert.equal(terrain[0 * GRID + 5], 1);
  assert.equal(terrain[16 * GRID + 6], 1);
  assert.equal(terrain[0 * GRID + 8], 0, 'outside stripe untouched');
});

test('floodFill stops at terrain boundaries (island)', () => {
  const { terrain, elevation } = blankArrays();
  // island of Plain surrounded by Deep (3)
  for (let y = 0; y < GRID; y++) for (let x = 0; x < GRID; x++) {
    if (x >= 10 && x <= 14 && y >= 10 && y <= 14) continue;
    terrain[y * GRID + x] = 3;
  }
  const changed = floodFill(terrain, elevation, 12, 12, { terrain: 1 });
  assert.equal(changed, 25);
  assert.equal(terrain[10 * GRID + 10], 1);
  assert.equal(terrain[9 * GRID + 10], 3, 'moat untouched');
});

test('floodFill with identical payload is a no-op (0 changed)', () => {
  const { terrain, elevation } = blankArrays();
  assert.equal(floodFill(terrain, elevation, 0, 0, { terrain: 0 }), 0);
});

test('floodFill elevation only lands on Hill tiles', () => {
  const { terrain, elevation } = blankArrays();
  terrain[0] = 5; terrain[1] = 5; terrain[2] = 0; // H H P
  const changed = floodFill(terrain, elevation, 0, 0, { elev: 2 });
  assert.equal(changed, 2);
  assert.equal(elevation[0], 2);
  assert.equal(elevation[2], 0, 'plain tile untouched');
});

test('floodFill region predicate includes elevation', () => {
  const { terrain, elevation } = blankArrays();
  terrain[0] = 5; terrain[1] = 5;
  elevation[0] = 0; elevation[1] = 1; // same terrain, different elev → two regions
  const changed = floodFill(terrain, elevation, 0, 0, { elev: 2 });
  assert.equal(changed, 1);
  assert.equal(elevation[1], 1, 'different-elevation neighbour not filled');
});

// --- isConnected ------------------------------------------------------------------

test('isConnected: blank map connects the runtime spawns (both profiles)', () => {
  const { terrain } = blankArrays();
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Light));
  assert.ok(isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Heavy));
});

test('isConnected: full wall row between spawns strands both profiles', () => {
  const { terrain } = blankArrays();
  for (let x = 0; x < GRID; x++) terrain[16 * GRID + x] = 8; // Wall
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(!isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Light));
  assert.ok(!isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Heavy));
});

test('isConnected: Shallow is Light-passable but Heavy-impassable', () => {
  const { terrain } = blankArrays();
  // Ring of Shallow around spawn A (16,12): Light crosses (cost 2), Heavy blocked (cost 0)
  for (let y = 10; y <= 14; y++) for (let x = 14; x <= 18; x++) {
    if (y === 10 || y === 14 || x === 14 || x === 18) terrain[y * GRID + x] = 2;
  }
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Light));
  assert.ok(!isConnected(terrain, new Uint8Array(GRID*GRID), a, b, PROFILE_COSTS.Heavy));
});

// --- view transform ----------------------------------------------------------------

test('screenToTile / tileToScreen round-trip at base zoom', () => {
  const view = { px: 16, ox: 0, oy: 0 };
  for (const [tx, ty] of [[0, 0], [5, 9], [31, 31], [16, 12]]) {
    const s = tileToScreen(tx, ty, view);
    const t = screenToTile(s.x, s.y, view);
    assert.deepEqual(t, { x: tx, y: ty });
  }
});

test('round-trip holds under arbitrary zoom + pan', () => {
  const view = { px: 37 - 37 + 32, ox: -123.5, oy: 400.25 };
  for (const [tx, ty] of [[0, 0], [7, 3], [20, 28], [31, 31]]) {
    const s = tileToScreen(tx, ty, view);
    const t = screenToTile(s.x, s.y, view);
    assert.deepEqual(t, { x: tx, y: ty });
  }
});

test('zoomAround keeps the anchor point fixed', () => {
  const view = { px: 16, ox: -40, oy: 60 };
  const sx = 200, sy = 300;
  const before = screenToTile(sx, sy, view);
  const z = zoomAround(sx, sy, 64, view);
  // The sub-tile position is preserved exactly, so the tile under the cursor
  // must be identical before and after.
  assert.deepEqual(screenToTile(sx, sy, z), before);
  assert.equal(z.px, 64);
});

test('zoomAround clamps to the zoom level set', () => {
  const view = { px: 16, ox: 0, oy: 0 };
  assert.equal(zoomAround(0, 0, 100, view).px, MAX_ZOOM);
  assert.equal(zoomAround(0, 0, 2, view).px, MIN_ZOOM);
});

test('clampPan centers a map smaller than the viewport', () => {
  const view = clampPan({ px: 8, ox: -999, oy: 999 }, 512, 512);
  // 32 tiles × 8px = 256px map in a 512px viewport → centered at 128.
  assert.equal(view.ox, 128);
  assert.equal(view.oy, 128);
});

test('clampPan keeps a large map covering the viewport', () => {
  // 32×64px = 2048px map in 512px viewport: origin must be in [-1536, 0].
  const a = clampPan({ px: 64, ox: 500, oy: -3000 }, 512, 512);
  assert.equal(a.ox, 0);
  assert.equal(a.oy, -1536);
  const b = clampPan({ px: 64, ox: -9999, oy: 0 }, 512, 512);
  assert.equal(b.ox, -1536);
  assert.equal(b.oy, 0);
});



// --- ramp grading ------------------------------------------------------------------

test('rampElevationAt: flat surroundings → 0', () => {
  const elevation = new Uint8Array(GRID * GRID);
  assert.equal(rampElevationAt(elevation, 16, 16), 0);
});

test('rampElevationAt: peak neighbour (2) → grades to 1', () => {
  const elevation = new Uint8Array(GRID * GRID);
  elevation[15 * GRID + 16] = 2; // west neighbour is a peak
  assert.equal(rampElevationAt(elevation, 16, 16), 1);
});

test('rampElevationAt: clamped at 0 below low ground', () => {
  const elevation = new Uint8Array(GRID * GRID);
  elevation[15 * GRID + 16] = 0; // only low neighbours
  assert.equal(rampElevationAt(elevation, 16, 16), 0);
});

test('rampElevationAt: uses the highest of all four neighbours', () => {
  const elevation = new Uint8Array(GRID * GRID);
  elevation[16 * GRID + 15] = 1; // west: mid
  elevation[17 * GRID + 16] = 2; // south: peak
  assert.equal(rampElevationAt(elevation, 16, 16), 1);
});

// --- cliff-aware connectivity -------------------------------------------------------

function blankTE() {
  return { terrain: new Uint8Array(GRID * GRID), elevation: new Uint8Array(GRID * GRID) };
}

test('isConnected: Δ2 elevation row between spawns strands both profiles', () => {
  const { terrain, elevation } = blankTE();
  // Full-width row of peaks at y=16 — every step across it is Δ2 (0 → 2), a
  // cliff with no ramp.
  for (let x = 0; x < GRID; x++) elevation[16 * GRID + x] = 2;
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(!isConnected(terrain, elevation, a, b, PROFILE_COSTS.Light));
  assert.ok(!isConnected(terrain, elevation, a, b, PROFILE_COSTS.Heavy));
});

test('isConnected: a Ramp tile in the cliff wall restores the route', () => {
  const { terrain, elevation } = blankTE();
  for (let x = 0; x < GRID; x++) elevation[16 * GRID + x] = 2;
  // Ramp at the spawn column — the server rule (either end is Ramp) unlocks
  // the Δ2 step across it.
  terrain[16 * GRID + 16] = RAMP_TERRAIN;
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(isConnected(terrain, elevation, a, b, PROFILE_COSTS.Light));
  assert.ok(isConnected(terrain, elevation, a, b, PROFILE_COSTS.Heavy));
});

test('isConnected: Δ1 steps are always walkable', () => {
  const { terrain, elevation } = blankTE();
  // Graded staircase 0→1→2 down the spawn column.
  elevation[14 * GRID + 16] = 1;
  elevation[15 * GRID + 16] = 2;
  const [a, b] = RUNTIME_SPAWNS;
  assert.ok(isConnected(terrain, elevation, a, b, PROFILE_COSTS.Light));
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
