const { test, expect } = require('@playwright/test');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Login and start a solo game, returning the page with game running. */
async function startSoloGame(page) {
  await page.goto('/');

  // Login
  await page.fill('#login-username', 'TestPlayer');
  await page.click('#login-form button[type="submit"]');

  // Wait for lobby
  await expect(page.locator('#lobby-screen.active')).toBeVisible();

  // Start solo game
  await page.click('#solo-btn');

  // Wait for game screen (match_found received)
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 5000 });
}

/** Poll until fog data is populated in game state. */
async function waitForFog(page, timeoutMs = 5000) {
  await page.waitForFunction(
    () => {
      const game = window.__paperWarGame;
      return game && game.state && game.state.fogVisible && game.state.fogVisible.length > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Get the fog grid from the game state. */
async function getFogState(page) {
  return page.evaluate(() => {
    const game = window.__paperWarGame;
    const state = game.state;
    return {
      width: state.fogWidth,
      height: state.fogHeight,
      visible: Array.from(state.fogVisible),
    };
  });
}

// ---------------------------------------------------------------------------
// Test 1 — Tracer bullet: fog grid received after game start
// ---------------------------------------------------------------------------

test('fog grid is received after starting a solo game', async ({ page }) => {
  await startSoloGame(page);
  await waitForFog(page);

  const fog = await getFogState(page);

  // Fog grid dimensions match the map (48x96)
  expect(fog.width).toBe(48);
  expect(fog.height).toBe(96);
  expect(fog.visible.length).toBe(48 * 96);

  // At least some tiles should be visible (commander's vision radius)
  const visibleCount = fog.visible.filter((v) => v === 1).length;
  expect(visibleCount).toBeGreaterThan(0);

  // Most tiles should be fogged (small map, one commander position)
  const foggedCount = fog.visible.filter((v) => v === 0).length;
  expect(foggedCount).toBeGreaterThan(0);
});

// ---------------------------------------------------------------------------
// Test 2 — Fog overlay renders dark tiles for fogged areas
// ---------------------------------------------------------------------------

test('buildFogTiles returns dark quads for fogged tiles only', async ({ page }) => {
  await startSoloGame(page);
  await waitForFog(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    const visible = game.camera.getVisibleTiles();
    const fogTiles = game.buildFogTiles(visible);

    // Check that fogged tiles get overlay quads
    const hasDarkQuads = fogTiles.every((t) => t.r === 0 && t.g === 0 && t.b === 0 && t.a > 0);

    return {
      fogTileCount: fogTiles.length,
      hasDarkQuads,
      sampleTile: fogTiles[0] || null,
      fogWidth: game.state.fogWidth,
      fogHeight: game.state.fogHeight,
      visibleRange: visible,
    };
  });

  // Fog tiles should exist (some tiles are fogged)
  expect(result.fogTileCount).toBeGreaterThan(0);

  // All fog tiles should be dark semi-transparent quads
  expect(result.hasDarkQuads).toBe(true);

  // Each fog tile should have valid position and dimensions
  expect(result.sampleTile.w).toBeGreaterThan(0);
  expect(result.sampleTile.h).toBeGreaterThan(0);
});

// ---------------------------------------------------------------------------
// Test 3 — Enemy units hidden in fogged tiles
// ---------------------------------------------------------------------------

test('enemy units are not present in fogged tiles', async ({ page }) => {
  await startSoloGame(page);
  await waitForFog(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    const state = game.state;
    const fogW = state.fogWidth;
    const fogH = state.fogHeight;
    const fogVisible = state.fogVisible;
    const playerID = game.playerID;

    // Player 1 owns squads 1,2; player 2 owns squads 3,4
    // (solo game: playerIndex * 2 + 1 base squad ID)
    const ownSquadBase = (playerID - 1) * 2 + 1;
    const ownSquads = new Set([ownSquadBase, ownSquadBase + 1]);

    const ownUnits = [];
    const enemyUnits = [];
    for (const [id, unit] of state.units) {
      if (ownSquads.has(unit.squadID)) {
        ownUnits.push(unit);
      } else if (unit.squadID > 0) {
        enemyUnits.push(unit);
      }
    }

    // Check each enemy unit's tile is visible in the fog grid
    const enemyInFog = enemyUnits.filter((u) => {
      const tx = Math.floor(u.currX);
      const ty = Math.floor(u.currY);
      if (tx < 0 || tx >= fogW || ty < 0 || ty >= fogH) return true;
      return fogVisible[ty * fogW + tx] === 0;
    });

    return {
      ownUnitCount: ownUnits.length,
      enemyUnitCount: enemyUnits.length,
      enemyInFogCount: enemyInFog.length,
      playerID,
      totalUnits: state.units.size,
    };
  });

  // Game should have units
  expect(result.totalUnits).toBeGreaterThan(0);

  // Player should have own units
  expect(result.ownUnitCount).toBeGreaterThan(0);

  // All enemy units in the snapshot must be on visible (non-fogged) tiles.
  // Enemy units on fogged tiles should have been filtered out server-side.
  expect(result.enemyInFogCount).toBe(0);
});

// ---------------------------------------------------------------------------
// Test 4 — Vision circle reveals correct tile pattern
// ---------------------------------------------------------------------------

test('vision around commander forms a circular pattern, not square', async ({ page }) => {
  await startSoloGame(page);
  await waitForFog(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    const state = game.state;
    const fogW = state.fogWidth;
    const fogVisible = state.fogVisible;

    // Find the center of the visible area (commander position)
    // Look for the highest-density cluster of visible tiles
    let visibleTiles = [];
    for (let y = 0; y < state.fogHeight; y++) {
      for (let x = 0; x < fogW; x++) {
        if (fogVisible[y * fogW + x] === 1) {
          visibleTiles.push({ x, y });
        }
      }
    }

    if (visibleTiles.length === 0) {
      return { error: 'no visible tiles', visibleCount: 0 };
    }

    // Find centroid of visible tiles
    const cx = visibleTiles.reduce((s, t) => s + t.x, 0) / visibleTiles.length;
    const cy = visibleTiles.reduce((s, t) => s + t.y, 0) / visibleTiles.length;

    // Compute max distance of visible tiles from centroid
    const distances = visibleTiles.map((t) =>
      Math.sqrt((t.x - cx) ** 2 + (t.y - cy) ** 2),
    );
    const maxDist = Math.max(...distances);

    // Check that corners of bounding box are NOT visible (proves circular, not square)
    const minX = Math.min(...visibleTiles.map((t) => t.x));
    const maxX = Math.max(...visibleTiles.map((t) => t.x));
    const minY = Math.min(...visibleTiles.map((t) => t.y));
    const maxY = Math.max(...visibleTiles.map((t) => t.y));

    const cornerTL = fogVisible[Math.round(minY) * fogW + Math.round(minX)] === 1;
    const cornerBR = fogVisible[Math.round(maxY) * fogW + Math.round(maxX)] === 1;

    // Count tiles near the edge (within 1 tile of maxDist)
    const edgeTiles = visibleTiles.filter((t) => {
      const d = Math.sqrt((t.x - cx) ** 2 + (t.y - cy) ** 2);
      return d >= maxDist - 1.5;
    });

    return {
      visibleCount: visibleTiles.length,
      centroid: { x: Math.round(cx * 10) / 10, y: Math.round(cy * 10) / 10 },
      maxDist: Math.round(maxDist * 10) / 10,
      boundingBox: { minX, maxX, minY, maxY },
      cornerTLVisible: cornerTL,
      cornerBRVisible: cornerBR,
      edgeTileCount: edgeTiles.length,
    };
  });

  expect(result.error).toBeUndefined();
  expect(result.visibleCount).toBeGreaterThan(0);

  // Vision radius should be ~12 tiles per squad, but with 2 squads
  // the total visible area can span wider (up to ~16 with separation)
  expect(result.maxDist).toBeGreaterThanOrEqual(10);
  expect(result.maxDist).toBeLessThanOrEqual(20);

  // At least one corner of the bounding box should NOT be visible
  // (proves the shape is circular, not square)
  expect(result.cornerTLVisible && result.cornerBRVisible).toBe(false);
});
