const { test, expect } = require('@playwright/test');
const BASE = 'http://localhost:9091';
test('undo/redo a paint stroke', async ({ page }) => {
  await page.goto(`${BASE}/editor/map.html`);
  await page.waitForFunction(() => document.querySelectorAll('#snapshot-select option').length > 1);
  await page.click('#clear-btn');
  // Undo/Redo start disabled after clear? Undo has the pre-clear state → enabled.
  await expect(page.locator('#undo-btn')).not.toBeDisabled();
  await expect(page.locator('#redo-btn')).toBeDisabled();

  // Paint a Deep stroke at (5,5).
  await page.locator('#terrain-palette .swatch').nth(3).click(); // Deep
  await page.locator('#canvas').click({ position: { x: 5*16+8, y: 5*16+8 } });
  let src = await readExport(page);
  expect(src).toContain('m.SetTerrain(5, 5, component.TerrainDeep)');

  // Undo → the stroke (and its mirror) should disappear.
  await page.click('#undo-btn');
  src = await readExport(page);
  expect(src).not.toContain('TerrainDeep');

  // Redo → it comes back.
  await page.click('#redo-btn');
  src = await readExport(page);
  expect(src).toContain('m.SetTerrain(5, 5, component.TerrainDeep)');

  // A new edit after redo clears the redo branch.
  await page.locator('#terrain-palette .swatch').nth(8).click(); // Wall
  await page.locator('#canvas').click({ position: { x: 2*16+8, y: 2*16+8 } });
  await expect(page.locator('#redo-btn')).toBeDisabled();
});

test('keyboard shortcut Cmd+Z / Shift+Cmd+Z', async ({ page }) => {
  await page.goto(`${BASE}/editor/map.html`);
  await page.waitForFunction(() => document.querySelectorAll('#snapshot-select option').length > 1);
  await page.click('#clear-btn');
  await page.locator('#terrain-palette .swatch').nth(3).click(); // Deep
  await page.locator('#canvas').click({ position: { x: 6*16+8, y: 6*16+8 } });
  let src = await readExport(page);
  expect(src).toContain('TerrainDeep');
  await page.keyboard.press('Meta+z');
  expect(await readExport(page)).not.toContain('TerrainDeep');
  await page.keyboard.press('Shift+Meta+z');
  expect(await readExport(page)).toContain('TerrainDeep');
});

async function readExport(page) {
  await page.click('#show-go-btn');
  return page.inputValue('#export-text');
}
