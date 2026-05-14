const { test, expect } = require('@playwright/test');

async function startSoloGame(page) {
  await page.goto('http://localhost:8090');
  await page.fill('#login-username', 'FogAlign');
  await page.click('#login-form button[type="submit"]');
  await expect(page.locator('#lobby-screen.active')).toBeVisible();
  await page.click('#solo-btn');
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 5000 });
  await page.waitForFunction(
    () => {
      const game = window.__paperWarGame;
      return game && game.state && game.state.fogVisible && game.state.fogVisible.length > 0;
    },
    { timeout: 5000 },
  );
}

test('fog overlay aligns with terrain tiles', async ({ page }) => {
  await startSoloGame(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    const visible = game.camera.getVisibleTiles();

    const terrainTiles = game.buildTerrainTiles(visible);
    const fogTiles = game.buildFogTiles(visible);

    // Build a lookup of terrain tile positions by (tx, ty)
    // Terrain uses: sx = tx * TILE_WIDTH * zoom, sy = ty * TILE_HEIGHT * zoom
    const zoom = game.camera.zoom;
    const terrainPositions = {};
    for (const t of terrainTiles) {
      const key = `${Math.round(t.x)},${Math.round(t.y)}`;
      terrainPositions[key] = t;
    }

    // Check each fog tile position matches a corresponding terrain tile position
    let mismatchCount = 0;
    let matchedCount = 0;
    const mismatches = [];

    for (const ft of fogTiles) {
      const key = `${Math.round(ft.x)},${Math.round(ft.y)}`;
      if (terrainPositions[key]) {
        matchedCount++;
      } else {
        mismatchCount++;
        if (mismatches.length < 5) {
          mismatches.push({ x: Math.round(ft.x), y: Math.round(ft.y), w: Math.round(ft.w), h: Math.round(ft.h) });
        }
      }
    }

    // Sample some terrain positions for comparison
    const sampleTerrain = terrainTiles.slice(0, 3).map((t) => ({
      x: Math.round(t.x), y: Math.round(t.y), w: Math.round(t.w), h: Math.round(t.h),
    }));
    const sampleFog = fogTiles.slice(0, 3).map((t) => ({
      x: Math.round(t.x), y: Math.round(t.y), w: Math.round(t.w), h: Math.round(t.h),
    }));

    return {
      terrainCount: terrainTiles.length,
      fogCount: fogTiles.length,
      matchedCount,
      mismatchCount,
      mismatches,
      sampleTerrain,
      sampleFog,
      zoom,
      tileWidth: 32,
      tileHeight: 32,
    };
  });

  // Every fog tile should sit exactly on top of a terrain tile
  expect(result.mismatchCount).toBe(0);
  expect(result.matchedCount).toBe(result.fogCount);
});

test('fog grid covers the full map height', async ({ page }) => {
  await startSoloGame(page);

  const result = await page.evaluate(() => {
    const game = window.__paperWarGame;
    return {
      fogWidth: game.state.fogWidth,
      fogHeight: game.state.fogHeight,
      mapWidth: game.mapWidth,
      mapHeight: game.mapHeight,
    };
  });

  // Fog grid must cover the full map
  expect(result.fogWidth).toBe(result.mapWidth);
  expect(result.fogHeight).toBe(result.mapHeight);
});
