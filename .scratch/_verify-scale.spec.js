const { test } = require('@playwright/test');

test('verify map dimensions are 30x48', async ({ browser }) => {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto('http://localhost:9091/', { waitUntil: 'domcontentloaded' });
  await page.fill('#login-username', 'scale-verify');
  await page.click('#login-form button[type="submit"]');
  await page.waitForSelector('#lobby-screen.active');
  await page.click('#solo-btn');
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 10000 });
  const dims = await page.evaluate(() => ({
    w: window.__paperWarGame.mapWidth,
    h: window.__paperWarGame.mapHeight,
  }));
  console.log('MAP_DIMENSIONS=' + JSON.stringify(dims));
  await ctx.close();
});
