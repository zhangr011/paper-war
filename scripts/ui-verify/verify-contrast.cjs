// WCAG contrast check on the restyled kit components, computed live.
// UI-verify driver (repo-resident). Run from the repo root:
//   node scripts/ui-verify/<this-script>
// Browser: set PLAYWRIGHT_CHROMIUM to override the default cached build.
// Output: $TMPDIR/paper-war-ui/
const { chromium } = require(require('path').isAbsolute(process.env.PW_PKG || '') ? process.env.PW_PKG : require('path').join(process.cwd(), 'node_modules', 'playwright'));

(async () => {
  const exe = (process.env.PLAYWRIGHT_CHROMIUM || '/Users/zhangrong/Library/Caches/ms-playwright/chromium-1217/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing');
  const browser = await chromium.launch({ executablePath: exe });
  const page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
  await page.goto('file:///Users/zhangrong/repo/paper-war/client/components.html', { waitUntil: 'domcontentloaded', timeout: 15000 });

  const pairs = [
    ['.selection-label', '.panel',            'selection label on tan panel'],
    ['.career-label',    '.career-row',       'career label on paper chip'],
    ['.career-value',    '.career-row',       'career value on paper chip'],
    ['.action-name',     '.action-btn',       'action label on paper tile'],
    ['.panel-title',     null,                'tape text (white on tape blue)'],
    ['.resource-value',  '.resource.card',    'stat numeral on paper card'],
  ];

  const results = await page.evaluate((pairs) => {
    const lum = (r, g, b) => {
      const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); };
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const parse = (c) => c.match(/\d+(\.\d+)?/g).map(Number);
    return pairs.map(([fg, bg, label]) => {
      const el = document.querySelector(fg);
      if (!el) return { label, error: 'fg not found' };
      const fgc = parse(getComputedStyle(el).color);
      let bgc = [255, 255, 255];
      if (bg) {
        const bel = el.closest(bg);
        if (bel) bgc = parse(getComputedStyle(bel).backgroundColor);
      } else if (fg === '.panel-title') {
        bgc = [0x1C, 0x6A, 0xB8]; // tape blue token
      }
      const l1 = lum(...fgc), l2 = lum(...bgc);
      const ratio = (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
      return { label, fg: fgc.join(','), bg: bgc.join(','), ratio: ratio.toFixed(2) };
    });
  }, pairs);

  let fail = 0;
  for (const r of results) {
    if (r.error) { console.log(`?? ${r.label}: ${r.error}`); continue; }
    const ok = parseFloat(r.ratio) >= 4.5;
    if (!ok) fail++;
    console.log(`${ok ? 'PASS' : 'FAIL'}  ${r.label}: ${r.ratio}:1  (fg ${r.fg} / bg ${r.bg})`);
  }
  console.log(fail === 0 ? 'ALL CONTRAST CHECKS PASS' : `${fail} FAILURES`);
  await browser.close();
})().catch(e => { console.error(e); process.exit(1); });
