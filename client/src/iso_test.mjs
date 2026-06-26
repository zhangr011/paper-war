// iso_test.mjs — Tests for coordinate conversion utilities
// Run with: node --test client/src/iso_test.mjs

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  worldToScreen,
  screenToWorld,
  worldToScreenCentered,
  depthKey,
  TILE_WIDTH,
  TILE_HEIGHT,
} from './iso.js';

describe('worldToScreen', () => {
  test('converts origin with zero offset', () => {
    assert.deepEqual(worldToScreen(0, 0, 0, 0), [0, 0]);
  });

  test('converts tile (1, 1) at zero offset', () => {
    assert.deepEqual(worldToScreen(1, 1, 0, 0), [TILE_WIDTH, TILE_HEIGHT]);
  });

  test('applies camera offset', () => {
    assert.deepEqual(worldToScreen(2, 3, 100, 200), [
      2 * TILE_WIDTH + 100,
      3 * TILE_HEIGHT + 200,
    ]);
  });

  test('handles negative world coords (for off-map culling)', () => {
    assert.deepEqual(worldToScreen(-1, -1, 0, 0), [-TILE_WIDTH, -TILE_HEIGHT]);
  });

  test('handles fractional world coords', () => {
    const [sx, sy] = worldToScreen(0.5, 0.25, 0, 0);
    assert.equal(sx, TILE_WIDTH / 2);
    assert.equal(sy, TILE_HEIGHT / 4);
  });
});

describe('screenToWorld (inverse)', () => {
  test('round-trips through worldToScreen', () => {
    for (const [wx, wy] of [[0, 0], [1, 1], [5.5, 3.2], [-2, -4]]) {
      const [sx, sy] = worldToScreen(wx, wy, 100, 200);
      const [bx, by] = screenToWorld(sx, sy, 100, 200);
      assert.ok(Math.abs(bx - wx) < 1e-9, `wx round-trip failed: ${wx} → ${bx}`);
      assert.ok(Math.abs(by - wy) < 1e-9, `wy round-trip failed: ${wy} → ${by}`);
    }
  });

  test('origin screen point with zero offset maps to world origin', () => {
    assert.deepEqual(screenToWorld(0, 0, 0, 0), [0, 0]);
  });
});

describe('worldToScreenCentered', () => {
  test('centers on origin when camera at origin and viewport 0', () => {
    // With viewW=viewH=0, the world origin maps to -camX,-camY
    const [sx, sy] = worldToScreenCentered(0, 0, 0, 0, 0, 0);
    assert.equal(sx, 0);
    assert.equal(sy, 0);
  });

  test('world point at camera center maps to viewport center', () => {
    // If camera is centered on world tile (5, 5), that tile should
    // appear at the center of the viewport.
    const camX = 5 * TILE_WIDTH;
    const camY = 5 * TILE_HEIGHT;
    const viewW = 800, viewH = 600;
    const [sx, sy] = worldToScreenCentered(5, 5, camX, camY, viewW, viewH);
    assert.equal(sx, viewW / 2);
    assert.equal(sy, viewH / 2);
  });
});

describe('depthKey', () => {
  test('higher Y → higher depth key (drawn later, in front)', () => {
    const behind = depthKey(0, 1);
    const front = depthKey(0, 2);
    assert.ok(front > behind, 'unit at y=2 should have greater depth than y=1');
  });

  test('same Y → sorts by X', () => {
    const left = depthKey(1, 5);
    const right = depthKey(2, 5);
    assert.ok(right > left, 'at same y, x=2 should sort after x=1');
  });

  test('returns unique keys for distinct positions (within reasonable range)', () => {
    const keys = new Set();
    for (let x = 0; x < 100; x++) {
      for (let y = 0; y < 100; y++) {
        keys.add(depthKey(x, y));
      }
    }
    assert.equal(keys.size, 10000, 'all (x,y) pairs in 100×100 should have unique keys');
  });

  test('X-axis collisions only beyond x=10000 (documented limit)', () => {
    // depthKey = y * 10000 + x — collisions happen when x ≥ 10000
    // For Paper War (48×96 map) this is fine.
    assert.equal(depthKey(0, 1), depthKey(10000, 0));
  });
});

describe('Constants', () => {
  test('TILE_WIDTH and TILE_HEIGHT are 32', () => {
    assert.equal(TILE_WIDTH, 32);
    assert.equal(TILE_HEIGHT, 32);
  });
});
