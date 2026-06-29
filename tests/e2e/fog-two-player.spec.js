const { test, expect } = require('@playwright/test');

// ---------------------------------------------------------------------------
// Two-player fog test — each player gets independent fog vision
// ---------------------------------------------------------------------------

test('two players get separate fog grids with independent visibility', async ({ browser }) => {
  test.setTimeout(60000); // 2-player match needs extra time for matchmaking + snapshots
  const player1 = await browser.newContext();
  const player2 = await browser.newContext();
  const page1 = await player1.newPage();
  const page2 = await player2.newPage();

  try {
    // --- Player 1: login and join queue ---
    await page1.goto('/');
    await page1.fill('#login-username', 'Player1');
    await page1.click('#login-form button[type="submit"]');
    await expect(page1.locator('#lobby-screen.active')).toBeVisible();
    await page1.click('#find-match-btn');

    // --- Player 2: login and join queue ---
    await page2.goto('/');
    await page2.fill('#login-username', 'Player2');
    await page2.click('#login-form button[type="submit"]');
    await expect(page2.locator('#lobby-screen.active')).toBeVisible();
    await page2.click('#find-match-btn');

    // Wait for match on both pages
    await expect(page1.locator('#game-screen.active')).toBeVisible({ timeout: 10000 });
    await expect(page2.locator('#game-screen.active')).toBeVisible({ timeout: 10000 });

    // Wait for fog data on both
    const waitForFog = async (page) => {
      await page.waitForFunction(
        () => {
          const game = window.__paperWarGame;
          return game && game.state && game.state.fogVisible && game.state.fogVisible.length > 0;
        },
        { timeout: 5000 },
      );
    };
    await waitForFog(page1);
    await waitForFog(page2);

    // Get fog state from both players
    const getFogState = async (page) => {
      return page.evaluate(() => {
        const game = window.__paperWarGame;
        const state = game.state;
        const fog = state.fogVisible;
        const fogW = state.fogWidth;
        const fogH = state.fogHeight;
        const playerID = game.playerID;

        const visibleTiles = [];
        for (let y = 0; y < fogH; y++) {
          for (let x = 0; x < fogW; x++) {
            if (fog[y * fogW + x] >= 1) {
              visibleTiles.push({ x, y });
            }
          }
        }

        return { playerID, fogW, fogH, visibleCount: visibleTiles.length, visibleTiles };
      });
    };

    const p1State = await getFogState(page1);
    const p2State = await getFogState(page2);

    // Both players should have valid fog grids matching the map
    expect(p1State.fogW).toBe(30);
    expect(p1State.fogH).toBe(48);
    expect(p2State.fogW).toBe(30);
    expect(p2State.fogH).toBe(48);

    // Both should have visible tiles
    expect(p1State.visibleCount).toBeGreaterThan(0);
    expect(p2State.visibleCount).toBeGreaterThan(0);

    // Players should have different IDs (1 and 2)
    expect(p1State.playerID).not.toBe(p2State.playerID);

    // Vision areas should differ — players spawn at opposite ends of the map
    const p1Center = {
      x: p1State.visibleTiles.reduce((s, t) => s + t.x, 0) / p1State.visibleCount,
      y: p1State.visibleTiles.reduce((s, t) => s + t.y, 0) / p1State.visibleCount,
    };
    const p2Center = {
      x: p2State.visibleTiles.reduce((s, t) => s + t.x, 0) / p2State.visibleCount,
      y: p2State.visibleTiles.reduce((s, t) => s + t.y, 0) / p2State.visibleCount,
    };

    // Centers should be far apart (players spawn at opposite ends of the map)
    const dist = Math.sqrt((p1Center.x - p2Center.x) ** 2 + (p1Center.y - p2Center.y) ** 2);
    expect(dist).toBeGreaterThan(10);

    // Fog grids should be different (different visible tile patterns)
    const p1Fog = await page1.evaluate(() => Array.from(window.__paperWarGame.state.fogVisible));
    const p2Fog = await page2.evaluate(() => Array.from(window.__paperWarGame.state.fogVisible));

    let differences = 0;
    for (let i = 0; i < p1Fog.length; i++) {
      if (p1Fog[i] !== p2Fog[i]) differences++;
    }
    expect(differences).toBeGreaterThan(0);
  } finally {
    await player1.close();
    await player2.close();
  }
});
