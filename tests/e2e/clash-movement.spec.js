const { test, expect } = require('@playwright/test');

// Regression test: in a clash match, the two armies must ADVANCE on each
// other (close distance), not sit at spawn and only shoot. The close-range
// static fixture (MoveDisabled) is for the balance harness, not the live
// spectator experience. Measures the inter-team centroid distance at t1 and t2
// and asserts it shrinks.

async function login(page) {
  await page.goto('/');
  await page.fill('#login-username', 'ClashMove');
  await page.click('#login-form button[type="submit"]');
  await expect(page.locator('#lobby-screen.active')).toBeVisible();
}

async function startClash(page) {
  await page.click('#clash-btn');
  await expect(page.locator('#clash-screen.active, #clash-config.active').first()).toBeVisible({ timeout: 5000 });
  await page.click('#clash-start-btn');
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 20000 });
}

async function waitForUnits(page, timeoutMs = 8000) {
  await page.waitForFunction(
    () => window.__paperWarGame && window.__paperWarGame.state && window.__paperWarGame.state.units && window.__paperWarGame.state.units.size > 0,
    { timeout: timeoutMs },
  );
}

// Returns { teams: {0:[{x,y}], 1:[{x,y}]}, dist, vy }.
async function teamGeometry(page) {
  return page.evaluate(() => {
    const units = window.__paperWarGame.state.units;
    const teams = { 0: [], 1: [] };
    let maxSpeed = 0;
    for (const u of units.values()) {
      const t = u.team in teams ? u.team : (u.team === 1 ? 1 : 0);
      if (teams[t]) teams[t].push({ x: u.currX, y: u.currY });
      const sp = Math.hypot(u.currVx || 0, u.currVy || 0);
      if (sp > maxSpeed) maxSpeed = sp;
    }
    const centroid = (pts) => {
      if (!pts.length) return null;
      return { x: pts.reduce((a, p) => a + p.x, 0) / pts.length, y: pts.reduce((a, p) => a + p.y, 0) / pts.length };
    };
    const c0 = centroid(teams[0]), c1 = centroid(teams[1]);
    const meanY = (t) => (teams[t].length ? teams[t].reduce((a, p) => a + p.y, 0) / teams[t].length : null);
    return {
      counts: [teams[0].length, teams[1].length],
      dist: c0 && c1 ? Math.hypot(c0.x - c1.x, c0.y - c1.y) : null,
      meanY0: meanY(0), meanY1: meanY(1), maxSpeed,
    };
  });
}

test('clash armies advance toward each other', async ({ page }) => {
  await login(page);
  await startClash(page);
  await waitForUnits(page);

  const t1 = await teamGeometry(page);
  expect(t1.dist, 'both teams must have units').not.toBeNull();

  // Let the match run ~9s (~90 ticks at 10Hz).
  await page.waitForTimeout(9000);

  const t2 = await teamGeometry(page);
  console.log(`clash t1: dist=${t1.dist.toFixed(2)} Y0=${t1.meanY0.toFixed(2)} Y1=${t1.meanY1.toFixed(2)} maxSpd=${t1.maxSpeed.toFixed(3)}`);
  console.log(`clash t2: dist=${t2.dist.toFixed(2)} Y0=${t2.meanY0.toFixed(2)} Y1=${t2.meanY1.toFixed(2)} maxSpd=${t2.maxSpeed.toFixed(3)}`);

  // Armies must close >1 tile — strategic advance, not the ~0.1-tile jitter
  // of the MoveDisabled static fixture (which this regression guards against).
  expect(t2.dist, 'armies did not advance — clash movement broken').toBeLessThan(t1.dist - 1);
});
