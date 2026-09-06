// Screenshot client/components.html with the repo's playwright 1.56,
// UI-verify driver (repo-resident). Run from the repo root:
//   node scripts/ui-verify/<this-script>
// Browser: set PLAYWRIGHT_CHROMIUM to override the default cached build.
// Output: $TMPDIR/paper-war-ui/
// pointing at the already-downloaded chromium-1217 build via executablePath
// (repo playwright wants 1194; the cache has 1217 — works fine headless).
const { chromium } = require(require('path').isAbsolute(process.env.PW_PKG || '') ? process.env.PW_PKG : require('path').join(process.cwd(), 'node_modules', 'playwright'));

(async () => {
  const exe = (process.env.PLAYWRIGHT_CHROMIUM || '/Users/zhangrong/Library/Caches/ms-playwright/chromium-1217/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing');
  const errors = [];
  const browser = await chromium.launch({ executablePath: exe });
  const page = await browser.newPage({ viewport: { width: 1100, height: 900 }, deviceScaleFactor: 2 });
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  page.on('pageerror', e => errors.push(String(e)));
  await page.goto('file:///Users/zhangrong/repo/paper-war/client/components.html', { waitUntil: 'domcontentloaded', timeout: 15000 });
  // fonts.ready can hang forever if the Google Fonts link stalls — race it.
  await page.evaluate(() => Promise.race([
    document.fonts.ready,
    new Promise(res => setTimeout(res, 3000)),
  ]));
  await page.waitForTimeout(600); // font swap + texture paint
  await page.screenshot({ path: require('os').tmpdir() + '/paper-war-ui/gallery.png', fullPage: true });
  const missing = await page.evaluate(() =>
    [...document.querySelectorAll('.cg-swatch.missing, .cg-item code')].map(el => el.textContent).filter(t => t.includes('MISSING')));
  console.log('console errors:', errors.length ? errors : 'none');
  console.log('missing tokens:', missing.length ? missing : 'none');
  await browser.close();
})().catch(e => { console.error(e); process.exit(1); });
