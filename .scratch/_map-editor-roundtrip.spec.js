// Round-trip verification for the Clash Map editor (#53).
// Loads each snapshot, generates Go source, parses the SetTerrain/Elevation
// calls back into a grid, and asserts it reproduces the /editor/clash-maps
// snapshot byte-for-byte. Proves export ↔ snapshot is lossless.
const { test, expect } = require('@playwright/test');

const BASE = 'http://localhost:9091';

// Parse the editor's exported Go source into {terrain, elevation} grids.
function parseExportedGo(src, n) {
  const terrain = new Array(n).fill(0);
  const elevation = new Array(n).fill(0);
  const goTerrain = [
    'TerrainPlain','TerrainRoad','TerrainShallow','TerrainDeep','TerrainForest',
    'TerrainHill','TerrainSwamp','TerrainBridge','TerrainWall','TerrainSnow',
    'TerrainDesert','TerrainStronghold1','TerrainStronghold2','TerrainStronghold3',
    'TerrainStronghold4','TerrainStronghold5',
  ];
  for (const line of src.split('\n')) {
    let m = line.match(/SetTerrain\((\d+),\s*(\d+),\s*component\.(\w+)\)/);
    if (m) {
      const x = +m[1], y = +m[2], idx = goTerrain.indexOf(m[3]);
      terrain[y * 32 + x] = idx;
      continue;
    }
    m = line.match(/TileAt\((\d+),\s*(\d+)\)\.Elevation\s*=\s*(\d+)/);
    if (m) {
      const x = +m[1], y = +m[2];
      elevation[y * 32 + x] = +m[3];
    }
  }
  return { terrain, elevation };
}

test('editor export round-trips every clash map snapshot', async ({ page }) => {
  // Fetch snapshots directly.
  const snaps = await (await fetch(`${BASE}/editor/clash-maps`)).json();

  await page.goto(`${BASE}/editor/map.html`);
  // Wait for the snapshot dropdown to be populated (async fetch from server).
  await page.waitForFunction(() => document.querySelectorAll('#snapshot-select option').length > 1);

  for (const name of Object.keys(snaps)) {
    const snap = snaps[name];
    const n = snap.terrain.length;

    // Drive the editor: pick snapshot, load, generate Go.
    await page.selectOption('#snapshot-select', name);
    await page.click('#load-btn');
    await page.click('#show-go-btn');
    const src = await page.inputValue('#export-text');

    const got = parseExportedGo(src, n);

    // Terrain must match exactly.
    for (let i = 0; i < n; i++) {
      expect(got.terrain[i], `${name} terrain @${i}`).toBe(snap.terrain[i]);
    }
    // Elevation: editor exports only hill tiles with elev>0; snapshot has 0
    // elsewhere too, so exact match holds.
    for (let i = 0; i < n; i++) {
      expect(got.elevation[i], `${name} elevation @${i}`).toBe(snap.elevation[i]);
    }
  }
});

test('blank canvas + painting writes expected SetTerrain calls', async ({ page }) => {
  await page.goto(`${BASE}/editor/map.html`);
  await page.waitForFunction(() => document.querySelectorAll('#snapshot-select option').length > 1);

  // Clear, then paint Deep (terrain id 3) at tile (5,5).
  await page.click('#clear-btn');
  const swatches = page.locator('#terrain-palette .swatch');
  await swatches.nth(3).click(); // index 3 = Deep
  // Click tile (5,5): tile size 16 → center at (5*16+8, 5*16+8) = (88,88),
  // relative to the canvas element.
  await page.locator('#canvas').click({ position: { x: 88, y: 88 } });

  await page.click('#show-go-btn');
  const src = await page.inputValue('#export-text');
  expect(src).toContain('m.SetTerrain(5, 5, component.TerrainDeep)');
  // Mirror on by default → (31-5=26, 5) should also be set.
  expect(src).toContain('m.SetTerrain(26, 5, component.TerrainDeep)');
  // Func name from default "Custom".
  expect(src).toMatch(/func ClashCustom\(\) \*GameMap/);
});
