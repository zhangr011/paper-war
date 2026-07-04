const { defineConfig } = require('@playwright/test');
const path = require('path');

// Resolve the headless-shell binary from common macOS locations.
// We can't rely on os.homedir() because some runtimes (e.g. Hermes profiles)
// override HOME to a sandbox path that doesn't contain the Playwright cache.
const fs = require('fs');
function resolveHeadlessShell() {
  const candidates = [
    process.env.PLAYWRIGHT_CHROMIUM_PATH,
    path.join(process.env.HOME || '/Users/zhangrong',
              'Library/Caches/ms-playwright/chromium_headless_shell-1217/chrome-headless-shell-mac-arm64/chrome-headless-shell'),
    '/Users/zhangrong/Library/Caches/ms-playwright/chromium_headless_shell-1217/chrome-headless-shell-mac-arm64/chrome-headless-shell',
  ].filter(Boolean);
  for (const c of candidates) {
    if (c && fs.existsSync(c)) return c;
  }
  // Fall back to the Playwright-bundled default; the test will fail loudly
  // if it doesn't exist.
  return candidates[0];
}

module.exports = defineConfig({
  testDir: './tests/e2e',
  timeout: 180000,        // 3 min per test — needed for crash-restart cycles
  expect: { timeout: 10000 },
  fullyParallel: false,
  workers: 1,              // CRITICAL: crash-restart kills/restarts the
                          // shared server on :9091. With >1 worker, it'd
                          // kill the server mid-test for other specs
                          // running in parallel. Force serial execution.
  retries: 0,
  use: {
    baseURL: 'http://localhost:9091',
    headless: true,
    viewport: { width: 1280, height: 720 },
    launchOptions: {
      executablePath: resolveHeadlessShell(),
    },
  },
  globalSetup: './tests/e2e/global-setup.js',
  globalTimeout: 900000,  // 15 min — suite-level cap
  projects: [
    // Default project: all tests except the flaky multiplayer-playtest.
    // Kept at retries: 0 so real regressions in fog/map-gen/crash-restart
    // surface immediately.
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
      testIgnore: /multiplayer-playtest/,
    },
    // Issue #47 Fix D: scoped retries for the multiplayer-playtest spec.
    // Despite the deterministic seed (Fix A) and tighter overlay check
    // (Fix C), this end-to-end test involves two browser contexts + a
    // real-time WebSocket server + simulation — some variance is
    // unavoidable (CI load, browser startup, network jitter). Two
    // retries absorbs the residual ~5-10% flakiness without masking
    // real regressions in the other 17 specs.
    {
      name: 'chromium-flaky-e2e',
      testMatch: /multiplayer-playtest/,
      retries: 2,
      use: { browserName: 'chromium' },
    },
  ],
});
