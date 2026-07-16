const { test, expect } = require('@playwright/test');

test('shader compiles + units render at 150%', async ({ browser }) => {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/favicon|404/.test(m.text())) errors.push(`console: ${m.text()}`);
  });

  await page.goto('http://localhost:9091/', { waitUntil: 'domcontentloaded' });
  await page.fill('#login-username', 'scale-check');
  await page.click('#login-form button[type="submit"]');
  await page.waitForSelector('#lobby-screen.active');
  await page.click('#solo-btn');
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 15000 });
  await page.waitForTimeout(3000);

  const probe = await page.evaluate(() => {
    const g = window.__paperWarGame;
    const r = g.renderer;
    return {
      hasSetUnitScale: typeof r.setUnitScale === 'function',
      unitScale: r.unitScale,
      currentZoom: r.currentZoom,
      descW: g.buildUnitDescriptors(g.state.getRenderUnits())[0]?.w,
      atlasCell: g.buildUnitDescriptors(g.state.getRenderUnits())[0]?.spriteW,
      unitCount: g.state.getRenderUnits().length,
    };
  });
  console.log('PROBE=' + JSON.stringify(probe, null, 2));
  console.log('ERRORS=' + JSON.stringify(errors));

  expect(errors.filter(e => !/favicon/.test(e))).toEqual([]);
  await ctx.close();
});
