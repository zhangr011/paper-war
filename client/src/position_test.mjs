// position_test.mjs — TDD test for unit/tile position alignment
// Run with: node client/src/position_test.mjs

import assert from 'node:assert/strict';

// ---- Constants matching iso.js ----
const TILE_WIDTH = 64;
const TILE_HEIGHT = 32;
const HALF_W = TILE_WIDTH / 2;  // 32
const HALF_H = TILE_HEIGHT / 2; // 16

// ---- Terrain tile raw world-pixel position (no camera offset) ----
function terrainTileScreen(tx, ty, zoom) {
  const sx = (tx - ty) * HALF_W * zoom;
  const sy = (tx + ty) * HALF_H * zoom;
  return [sx, sy];
}

// ---- Unit raw world-pixel position (same formula as terrain, using float world coords) ----
function unitWorldToRawScreen(wx, wy, zoom) {
  const sx = (wx - wy) * HALF_W * zoom;
  const sy = (wx + wy) * HALF_H * zoom;
  return [sx, sy];
}

// ---- Renderer applies camera offset to raw positions ----
function applyCameraOffset(sx, sy, cameraOffsetX, cameraOffsetY) {
  return [sx - cameraOffsetX, sy - cameraOffsetY];
}

// ---- Tests ----

function testUnitPositionMatchesTilePosition() {
  const zoom = 1.0;
  const viewW = 800;
  const viewH = 600;
  const cameraOffsetX = (32 + 32) * HALF_W * zoom - viewW / 2;
  const cameraOffsetY = (32 + 32) * HALF_H * zoom - viewH / 2;

  // Terrain tile (10, 10)
  const [rawTileX, rawTileY] = terrainTileScreen(10, 10, zoom);
  const [finalTileX, finalTileY] = applyCameraOffset(rawTileX, rawTileY, cameraOffsetX, cameraOffsetY);

  // Unit at world (10.0, 10.0) — uses same raw formula as terrain
  const [rawUnitX, rawUnitY] = unitWorldToRawScreen(10.0, 10.0, zoom);
  const [finalUnitX, finalUnitY] = applyCameraOffset(rawUnitX, rawUnitY, cameraOffsetX, cameraOffsetY);

  assert.strictEqual(finalUnitX, finalTileX,
    `Unit X (${finalUnitX}) should match tile X (${finalTileX})`);
  assert.strictEqual(finalUnitY, finalTileY,
    `Unit Y (${finalUnitY}) should match tile Y (${finalTileY})`);
}

function testUnitPositionMatchesAtDifferentZoom() {
  const zoom = 2.0;
  const viewW = 1024;
  const viewH = 768;
  const cameraOffsetX = 64 * HALF_W * zoom - viewW / 2;
  const cameraOffsetY = 64 * HALF_H * zoom - viewH / 2;

  // Terrain tile (5, 20)
  const [rawTileX, rawTileY] = terrainTileScreen(5, 20, zoom);
  const [finalTileX, finalTileY] = applyCameraOffset(rawTileX, rawTileY, cameraOffsetX, cameraOffsetY);

  // Unit at world (5.0, 20.0)
  const [rawUnitX, rawUnitY] = unitWorldToRawScreen(5.0, 20.0, zoom);
  const [finalUnitX, finalUnitY] = applyCameraOffset(rawUnitX, rawUnitY, cameraOffsetX, cameraOffsetY);

  assert.strictEqual(finalUnitX, finalTileX,
    `At zoom=${zoom}, unit X (${finalUnitX}) should match tile X (${finalTileX})`);
  assert.strictEqual(finalUnitY, finalTileY,
    `At zoom=${zoom}, unit Y (${finalUnitY}) should match tile Y (${finalTileY})`);
}

function testFractionalPositionMatchesBetweenTiles() {
  const zoom = 1.0;
  const viewW = 800;
  const viewH = 600;
  const cameraOffsetX = 0;
  const cameraOffsetY = 0;

  // Unit at fractional position (10.5, 10.5) should be exactly between tile (10,10) and (11,11)
  const [rawUnitX, rawUnitY] = unitWorldToRawScreen(10.5, 10.5, zoom);
  const [finalUnitX, finalUnitY] = applyCameraOffset(rawUnitX, rawUnitY, cameraOffsetX, cameraOffsetY);

  // Expected: same formula as tile (10.5, 10.5)
  const expectedX = (10.5 - 10.5) * HALF_W * zoom;
  const expectedY = (10.5 + 10.5) * HALF_H * zoom;

  assert.strictEqual(finalUnitX, expectedX);
  assert.strictEqual(finalUnitY, expectedY);
}

// ---- Run tests ----
let passed = 0;
let failed = 0;

for (const [name, fn] of Object.entries({
  testUnitPositionMatchesTilePosition,
  testUnitPositionMatchesAtDifferentZoom,
  testFractionalPositionMatchesBetweenTiles,
})) {
  try {
    fn();
    console.log(`PASS: ${name}`);
    passed++;
  } catch (e) {
    console.log(`FAIL: ${name}`);
    console.log(`  ${e.message}`);
    failed++;
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
