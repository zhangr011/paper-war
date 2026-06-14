const { defineConfig } = require('@playwright/test');
const os = require('os');
const path = require('path');

const home = os.homedir();
const headlessShell = path.join(
  home,
  'Library/Caches/ms-playwright/chromium_headless_shell-1217/chrome-headless-shell-mac-arm64/chrome-headless-shell',
);

module.exports = defineConfig({
  testDir: './tests/e2e',
  timeout: 30000,
  expect: { timeout: 10000 },
  fullyParallel: false,
  retries: 0,
  use: {
    baseURL: 'http://localhost:9091',
    headless: true,
    viewport: { width: 1280, height: 720 },
    launchOptions: {
      executablePath: headlessShell,
    },
  },
  globalSetup: './tests/e2e/global-setup.js',
  globalTimeout: 60000,
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
});
