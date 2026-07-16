const { test } = require('@playwright/test');

test('probe unit shader scaling', async ({ browser }) => {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto('http://localhost:9091/', { waitUntil: 'domcontentloaded' });
  await page.fill('#login-username', 'probe');
  await page.click('#login-form button[type="submit"]');
  await page.waitForSelector('#lobby-screen.active');
  await page.click('#solo-btn');
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 10000 });
  await page.waitForTimeout(2000);

  const probe = async (z) => await page.evaluate((zoom) => {
    const g = window.__paperWarGame;
    g.camera.zoom = zoom;
    g.renderer.setZoom(zoom);
    return new Promise(resolve => {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          const desc = g.buildUnitDescriptors(g.state.getRenderUnits());
          const u = desc[0] || {};
          resolve({
            zoom,
            tilePx: 32 * zoom,
            rendererZoom: g.renderer.currentZoom,
            descW: u.w,
            descSpriteW: u.spriteW,
          });
        });
      });
    });
  }, z);

  console.log('zoom=0.5:', JSON.stringify(await probe(0.5)));
  console.log('zoom=1.0:', JSON.stringify(await probe(1.0)));
  console.log('zoom=2.0:', JSON.stringify(await probe(2.0)));
  await ctx.close();
});
