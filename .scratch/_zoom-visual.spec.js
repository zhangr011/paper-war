const { test } = require('@playwright/test');

test('zoom unit visual check', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 720 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/favicon|404/.test(m.text())) errors.push(`console: ${m.text()}`);
  });

  await page.goto('http://localhost:9091/', { waitUntil: 'domcontentloaded' });
  await page.fill('#login-username', 'zoom-check');
  await page.click('#login-form button[type="submit"]');
  await page.waitForSelector('#lobby-screen.active');
  await page.click('#solo-btn');
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 15000 });
  await page.waitForTimeout(2000); // let units populate

  // Take a screenshot at default zoom
  await page.screenshot({ path: '/tmp/pw-zoom-default.png' });

  // Zoom in via button
  const zoomInBtn = await page.$('#zoom-in-btn');
  if (zoomInBtn) {
    for (let i = 0; i < 3; i++) await zoomInBtn.click();
  }
  await page.waitForTimeout(500);
  await page.screenshot({ path: '/tmp/pw-zoom-in.png' });

  // Zoom out via button
  const zoomOutBtn = await page.$('#zoom-out-btn');
  if (zoomOutBtn) {
    for (let i = 0; i < 5; i++) await zoomOutBtn.click();
  }
  await page.waitForTimeout(500);
  await page.screenshot({ path: '/tmp/pw-zoom-out.png' });

  console.log('ERRORS=' + JSON.stringify(errors));
  await ctx.close();
});
