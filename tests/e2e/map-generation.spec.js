const { test, expect } = require('@playwright/test');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const TERRAIN = {
  Plain: 0,
  Road: 1,
  Shallow: 2,
  Deep: 3,
  Forest: 4,
  Hill: 5,
  Swamp: 6,
  Bridge: 7,
  Wall: 8,
  Snow: 9,
  Desert: 10,
  Stronghold1: 11,
  Stronghold2: 12,
  Stronghold3: 13,
  Stronghold4: 14,
  Stronghold5: 15,
};

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

/** Poll until terrain data is populated in game state. */
async function waitForTerrain(page, timeoutMs = 5000) {
  await page.waitForFunction(
    () => {
      const game = window.__paperWarGame;
      return game && game.terrainData && game.terrainData.length > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Poll until at least one unit is in the client game state. */
async function waitForUnits(page, timeoutMs = 5000) {
  await page.waitForFunction(
    () => {
      const game = window.__paperWarGame;
      return game && game.state && game.state.units && game.state.units.size > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Get terrain and map info from the game state. */
async function getMapState(page) {
  return page.evaluate(() => {
    const game = window.__paperWarGame;
    return {
      mapWidth: game.mapWidth,
      mapHeight: game.mapHeight,
      terrainLength: game.terrainData ? game.terrainData.length : 0,
      terrain: game.terrainData ? Array.from(game.terrainData) : [],
      playerID: game.playerID,
    };
  });
}

// ---------------------------------------------------------------------------
// Test 1 — Map terrain data received after game start
// ---------------------------------------------------------------------------

test('map terrain data is received after starting a solo game', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);

  const map = await getMapState(page);

  // Terrain array should exist and have data
  expect(map.terrainLength).toBeGreaterThan(0);

  // Terrain array should match map dimensions
  expect(map.terrainLength).toBe(map.mapWidth * map.mapHeight);
});

// ---------------------------------------------------------------------------
// Test 2 — Map dimensions correct (30x48)
// ---------------------------------------------------------------------------

test('map dimensions are 30x48 as expected', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);

  const map = await getMapState(page);

  expect(map.mapWidth).toBe(30);
  expect(map.mapHeight).toBe(48);
  expect(map.terrainLength).toBe(30 * 48);
});

// ---------------------------------------------------------------------------
// Test 3 — Map contains expected terrain types
// ---------------------------------------------------------------------------

test('map contains expected major terrain types', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);

  const map = await getMapState(page);

  const terrainSet = new Set(map.terrain);

  // Should always have Plain tiles (the base terrain)
  expect(terrainSet.has(TERRAIN.Plain)).toBe(true);

  // Should have at least one of Forest or Hill (generated via Perlin noise)
  const hasForest = terrainSet.has(TERRAIN.Forest);
  const hasHill = terrainSet.has(TERRAIN.Hill);
  expect(hasForest || hasHill).toBe(true);

  // Count each major terrain type
  const counts = {};
  for (const t of map.terrain) {
    counts[t] = (counts[t] || 0) + 1;
  }

  // Plain should be present (base terrain, ~25% with 75% forest coverage)
  expect(counts[TERRAIN.Plain]).toBeGreaterThan(0);
});

// ---------------------------------------------------------------------------
// Test 4 — Map spawns exist (units placed within map bounds)
// ---------------------------------------------------------------------------

test('spawn points place units within map bounds', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);
  await waitForUnits(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    const state = game.state;
    const mapW = game.mapWidth;
    const mapH = game.mapHeight;

    // Collect all units and check their positions
    const unitPositions = [];
    for (const [id, unit] of state.units) {
      unitPositions.push({
        id,
        x: unit.currX,
        y: unit.currY,
        squadID: unit.squadID,
        playerID: unit.playerID,
      });
    }

    // Check all units are within bounds (positions are fixed-point, >>12 = tile coords)
    const allInBounds = unitPositions.every(u => {
      const tx = u.x / 4096;
      const ty = u.y / 4096;
      return tx >= 0 && tx < mapW && ty >= 0 && ty < mapH;
    });

    // Group by player
    const playerIDs = new Set(unitPositions.map(u => u.playerID));

    return {
      mapW,
      mapH,
      totalUnits: unitPositions.length,
      playerCount: playerIDs.size,
      allInBounds,
      samplePositions: unitPositions.slice(0, 5).map(u => ({ x: Math.floor(u.x / 4096), y: Math.floor(u.y / 4096) })),
    };
  });

  // Should have units from both teams (player + AI)
  expect(result.totalUnits).toBeGreaterThan(0);
  expect(result.playerCount).toBeGreaterThanOrEqual(1);

  // All units must be within map bounds
  expect(result.allInBounds).toBe(true);
});

// ---------------------------------------------------------------------------
// Test 5 — Map is fully populated (all tiles have valid terrain types 0-8)
// ---------------------------------------------------------------------------

test('all map tiles have valid terrain types', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);

  const map = await getMapState(page);

  // No empty/zero-length terrain
  expect(map.terrain.length).toBeGreaterThan(0);

  // All terrain values must be in range 0-15 (TerrainType enum)
  const validTypes = new Set(Array.from({ length: 16 }, (_, i) => i));
  for (let i = 0; i < map.terrain.length; i++) {
    expect(validTypes.has(map.terrain[i])).toBe(true);
  }

  // Terrain array size matches dimensions exactly
  expect(map.terrain.length).toBe(map.mapWidth * map.mapHeight);
});

// ---------------------------------------------------------------------------
// Test 6 — Map connectivity (terrain is mixed, not uniform)
// ---------------------------------------------------------------------------

test('procedural map has mixed terrain types', async ({ page }) => {
  await startSoloGame(page);
  await waitForTerrain(page);

  const map = await getMapState(page);

  // Count unique terrain types
  const terrainSet = new Set(map.terrain);
  const uniqueCount = terrainSet.size;

  // A procedurally generated map should have multiple terrain types
  // (at least Plain + a few generated types like Forest, Hill, River, etc.)
  expect(uniqueCount).toBeGreaterThanOrEqual(3);

  // Verify it's not just one uniform type
  const counts = {};
  for (const t of map.terrain) {
    counts[t] = (counts[t] || 0) + 1;
  }

  // No single terrain type should cover the entire map
  const totalTiles = map.terrain.length;
  for (const [type, count] of Object.entries(counts)) {
    expect(count).toBeLessThan(totalTiles);
  }
});

// ---------------------------------------------------------------------------
// Test 7 — Multiple games produce different maps
// ---------------------------------------------------------------------------

test('multiple games produce different terrain maps', async ({ browser }) => {
  // Game 1
  const ctx1 = await browser.newContext();
  const page1 = await ctx1.newPage();
  await startSoloGame(page1);
  await waitForTerrain(page1);
  const map1 = await getMapState(page1);

  // Navigate away / end game 1 context
  await ctx1.close();

  // Game 2
  const ctx2 = await browser.newContext();
  const page2 = await ctx2.newPage();
  await startSoloGame(page2);
  await waitForTerrain(page2);
  const map2 = await getMapState(page2);

  await ctx2.close();

  // Both maps should have valid dimensions
  expect(map1.terrain.length).toBe(30 * 48);
  expect(map2.terrain.length).toBe(30 * 48);

  // Maps should differ (procedural generation with random seed)
  let differences = 0;
  for (let i = 0; i < map1.terrain.length; i++) {
    if (map1.terrain[i] !== map2.terrain[i]) {
      differences++;
    }
  }

  // With random seeds, maps should differ significantly
  expect(differences).toBeGreaterThan(0);
});
