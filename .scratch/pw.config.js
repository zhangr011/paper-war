const { defineConfig } = require('@playwright/test');
const path = require('path');
const fs = require('fs');
const shell = path.join(process.env.HOME || '/Users/zhangrong',
  'Library/Caches/ms-playwright/chromium_headless_shell-1217/chrome-headless-shell-mac-arm64/chrome-headless-shell');
module.exports = defineConfig({
  testDir: __dirname,
  timeout: 60000,
  workers: 1,
  use: {
    baseURL: 'http://localhost:9091',
    headless: true,
    launchOptions: { executablePath: fs.existsSync(shell) ? shell : undefined },
  },
});
