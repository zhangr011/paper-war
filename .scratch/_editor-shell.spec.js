const { test, expect } = require('@playwright/test');
test('editor shell switches tabs and preserves state', async ({ page }) => {
  await page.goto('http://localhost:9091/editor/');
  // Map tab active by default → its iframe loads the canvas.
  await page.waitForSelector('iframe[data-tab="map"].active');
  await expect(page.frameLocator('iframe[data-tab="map"]').locator('#canvas')).toBeVisible();
  // Switch to Units → units editor iframe mounts.
  await page.click('.tab[data-tab="units"]');
  await expect(page.frameLocator('iframe[data-tab="units"]').locator('h1')).toContainText('Combat Unit Editor');
  // Switch to Animation.
  await page.click('.tab[data-tab="animation"]');
  await expect(page.frameLocator('iframe[data-tab="animation"]').locator('h1')).toContainText('Animation');
  // Back to Map — iframe persisted (still exactly one map frame, state kept).
  await page.click('.tab[data-tab="map"]');
  await expect(page.locator('iframe[data-tab="map"]')).toHaveClass(/active/);
  await expect(page.locator('iframe[data-tab="units"]')).not.toHaveClass(/active/);
  expect(await page.locator('main iframe').count()).toBe(3); // all 3 mounted, kept alive
});
