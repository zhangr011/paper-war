// Drive a solo match against the running server (:9091) and capture the
// UI-verify driver (repo-resident). Run from the repo root:
//   node scripts/ui-verify/<this-script>
// Browser: set PLAYWRIGHT_CHROMIUM to override the default cached build.
// Output: $TMPDIR/paper-war-ui/
// minimap canvas (2D — composites into element screenshots).
const { chromium } = require(require('path').isAbsolute(process.env.PW_PKG || '') ? process.env.PW_PKG : require('path').join(process.cwd(), 'node_modules', 'playwright'));

(async () => {
  const exe = (process.env.PLAYWRIGHT_CHROMIUM || '/Users/zhangrong/Library/Caches/ms-playwright/chromium-1217/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing');
  const errors = [];
  const browser = await chromium.launch({ executablePath: exe });
  const page = await browser.newPage({ viewport: { width: 520, height: 900 }, deviceScaleFactor: 2 });
  page.on('pageerror', e => errors.push(String(e)));
  await page.goto('http://localhost:9091', { waitUntil: 'domcontentloaded', timeout: 15000 });

  // Login → lobby
  await page.fill('#login-username', 'KitCheck');
  await page.click('.login-btn');
  await page.waitForSelector('#lobby-screen.active', { timeout: 8000 }).catch(() => {});
  // Start a solo match
  await page.click('#solo-btn', { timeout: 8000 }).catch(e => console.log('solo-btn click failed:', e.message));
  await page.waitForSelector('#game-screen.active', { timeout: 15000 }).catch(() => console.log('game screen never activated'));

  // Wait for map data → minimap terrain build + a few frames
  await page.waitForTimeout(5000);

  const mm = await page.$('#minimap-canvas');
  if (mm) {
    await mm.screenshot({ path: require('os').tmpdir() + '/paper-war-ui/minimap.png' });
    console.log('minimap captured');
  } else {
    console.log('NO #minimap-canvas on page');
  }
  await page.screenshot({ path: require('os').tmpdir() + '/paper-war-ui/hud.png' });
  console.log('page errors:', errors.length ? errors.slice(0, 3) : 'none');
  await browser.close();
})().catch(e => { console.error(e); process.exit(1); });
