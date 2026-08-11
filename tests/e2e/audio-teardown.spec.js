const { test, expect } = require('@playwright/test');

// Regression: combat SFX must stop once the match ends.
//
// During the server's PhaseEnded flush window the connection layer keeps
// delivering event-bearing snapshots (the last tick's events, replayed).
// Without a match-phase gate on processEvents, gunfire/cannon/explosion
// SFX keep firing after the result overlay is shown — the "sound is not
// vanished when clash finished" bug. The fix gates processEvents (and the
// base-alert siren) on `matchWinner === undefined`.
//
// We can't capture the waveform headless, but we CAN observe voice
// allocation: after match end + a grace period for in-flight SFX to decay,
// audioEngine.activeVoices must be 0 and stay 0 (no new combat SFX).

async function login(page) {
  await page.goto('/');
  await page.fill('#login-username', 'AudioDiag');
  await page.click('#login-form button[type="submit"]');
  await expect(page.locator('#lobby-screen.active')).toBeVisible();
}

async function startClash(page) {
  await page.click('#clash-btn');
  await expect(page.locator('#clash-screen.active, #clash-config.active').first()).toBeVisible({ timeout: 5000 });
  await page.click('#clash-start-btn');
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 20000 });
}

test('clash: no combat SFX after match ends', async ({ page }) => {
  await login(page);
  await startClash(page);

  await page.waitForFunction(
    () => window.__paperWarGame && window.__paperWarGame.state && window.__paperWarGame.state.units && window.__paperWarGame.state.units.size > 0,
    { timeout: 10000 },
  );
  // Spectator gesture to start audio (ambient + SFX armed).
  await page.keyboard.press('Space');
  await page.mouse.click(640, 360);

  // Sanity: audio actually started.
  await page.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.audioStarted, { timeout: 5000 });

  // Wait for the match to end (matchWinner set by onMatchResult).
  await page.waitForFunction(
    () => window.__paperWarGame && window.__paperWarGame.matchWinner !== undefined,
    { timeout: 150000 },
  );

  // Grace period: let any SFX from the final pre-end tick decay
  // (longest envelope is explosion at ~0.6s).
  await page.waitForTimeout(2000);

  // Sample activeVoices twice, 800ms apart. Both must be 0 — no new combat
  // SFX are being allocated from the post-end event snapshots.
  const sample = () => page.evaluate(() => window.__paperWarGame && window.__paperWarGame.audioEngine
    ? window.__paperWarGame.audioEngine.activeVoices : -1);
  const v1 = await sample();
  await page.waitForTimeout(800);
  const v2 = await sample();

  expect(v1, 'activeVoices should be 0 after match end (no post-end combat SFX)').toBe(0);
  expect(v2, 'activeVoices should stay 0 — combat SFX must not resume from flush-window snapshots').toBe(0);
});
