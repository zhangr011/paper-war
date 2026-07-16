const { test } = require('@playwright/test');

// Verify clash spawns are at opposite ends (top-center + bottom-center),
// not the old center+center behavior.
test('clash spawns at opposite ends with 1 path', async ({ browser }) => {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));

  await page.goto('http://localhost:9091/', { waitUntil: 'domcontentloaded' });
  await page.fill('#login-username', 'clash-probe');
  await page.click('#login-form button[type="submit"]');
  await page.waitForSelector('#lobby-screen.active');
  await page.click('#clash-btn');
  await page.waitForSelector('#clash-screen.active');

  // Pick plains terrain and start
  await page.click('#clash-start-btn, .clash-start-btn, [data-action="start-clash"]', { timeout: 5000 }).catch(() => {});
  // If no explicit start button, the clash screen may auto-start
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 15000 });

  const probe = await page.evaluate(() => {
    const g = window.__paperWarGame;
    const spawns = g.mapData?.spawns || [];
    const units = [...g.state.units.values()];
    const t1 = units.filter(u => u.team === 1).map(u => ({ x: u.pos?.x / 4096, y: u.pos?.y / 4096 }));
    const t2 = units.filter(u => u.team === 2).map(u => ({ x: u.pos?.x / 4096, y: u.pos?.y / 4096 }));
    const t1avgY = t1.length ? t1.reduce((s, u) => s + u.y, 0) / t1.length : -1;
    const t2avgY = t2.length ? t2.reduce((s, u) => s + u.y, 0) / t2.length : -1;
    return {
      mapW: g.mapWidth,
      mapH: g.mapHeight,
      spawns,
      t1Count: t1.length,
      t2Count: t2.length,
      t1avgY,
      t2avgY,
      initDistance: Math.abs(t1avgY - t2avgY),
    };
  });
  console.log('PROBE=' + JSON.stringify(probe, null, 2));
  console.log('ERRORS=' + JSON.stringify(errors));
  await ctx.close();
});
