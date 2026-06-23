// crash-restart.spec.js — Regression guard for issue #17
//
// Issue #17: After the server crashes mid-match and restarts, the client
// could not start a new match without a full page reload. Three rounds of
// root-cause analysis uncovered that the Game class silently hijacked
// connection callbacks and never restored them.
//
// This spec verifies the fix holds by actually killing the Go server
// mid-match and restarting it, then asserting the client can:
//   1. Detect the disconnect
//   2. Return to the lobby (via reconnect_failed → cleanupGame in solo,
//      or directly via cleanupGame in clash since there's no token)
//   3. Start a NEW match from the lobby
//
// The cycle is repeated 3 times per mode to catch callback-leak regressions
// (each leaked callback compounds across cycles).
//
// IMPORTANT: This spec manages its own server lifecycle. global-setup.js's
// server may already be running, may have failed to start (port in use), or
// may have been started by a prior test — either way, we find the current
// listener PID via `lsof` and kill+restart in place. We always leave a
// server running on exit so subsequent specs work.

const { test, expect } = require('@playwright/test');
const { execSync, spawn } = require('child_process');
const http = require('http');
const path = require('path');

const PORT = 9091;
const SERVER_DIR = path.join(__dirname, '../../server');

// IMPORTANT: spawn `go run ./cmd/server`, NOT a prebuilt binary.  A stale
// `server/server` binary was the cause of the spec failing in earlier runs
// (the binary predated the solo-flow / callback fixes and clicked
// #solo-btn never transitioned to #game-screen).  `go run` always uses the
// current source, with a small first-invocation compile cost.
//
// PATH must include /opt/homebrew/bin so the `go` binary is found inside
// Playwright's spawned shell (the parent shell profile isn't inherited).

// Track the live `go run` parent + the spec's spawned child so we can
// clean them up on teardown.  We re-find the actual listener PID via
// lsof for kill/restart, since `go run`'s child PID isn't known ahead
// of time.
const spawnedProcs = [];

// ---------------------------------------------------------------------------
// Server lifecycle helpers
// ---------------------------------------------------------------------------

/** Find the PID currently listening on PORT, or null if none. */
function findServerPID() {
  try {
    const out = execSync(`lsof -i :${PORT} -sTCP:LISTEN -t 2>/dev/null`, {
      encoding: 'utf8',
    }).trim();
    if (!out) return null;
    // lsof can return multiple PIDs — take the first.
    return parseInt(out.split('\n')[0], 10);
  } catch {
    return null;
  }
}

/** Kill the server on PORT. No-op if none is running. */
async function killServer() {
  const pid = findServerPID();
  if (pid) {
    try { process.kill(pid, 'SIGKILL'); } catch { /* already dead */ }
    // Wait for the port to be released.  The previous implementation
    // busy-spun an un-awaited sleep(200) Promise, making this loop a
    // no-op — `restartServer()` then raced with the dying listener and
    // the new server hit `bind: address already in use` and exited
    // silently.  Await the sleep properly.
    const deadline = Date.now() + 8000;
    while (Date.now() < deadline) {
      if (!findServerPID()) return;
      await sleep(200);
    }
    console.warn(`[crash-restart] killServer: PID ${pid} still listening after 8s`);
  }
  // Note: spawned `go run` parents are detached + unref'd, so we CAN'T
  // rely on proc.kill() to clean them up — we always re-find via lsof
  // and kill by the actual listener PID.  No spawnedProcs cleanup here.
}

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

/** Poll the server URL until it returns 200 or timeout. */
async function waitForServer(timeoutMs = 60000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      await new Promise((resolve, reject) => {
        const req = http.get(`http://localhost:${PORT}/`, (res) => {
          res.resume();
          res.statusCode === 200 ? resolve() : reject(new Error(String(res.statusCode)));
        });
        req.on('error', reject);
        req.setTimeout(1000, () => reject(new Error('timeout')));
      });
      return true;
    } catch {
      await sleep(250);
    }
  }
  throw new Error(`Server on :${PORT} did not become ready within ${timeoutMs}ms`);
}

/** Start a fresh server via `go run ./cmd/server`. Returns the child process. */
async function startServer() {
  // IMPORTANT: spawn detached (in its own process group) and explicitly
  // UNref the child so Playwright's worker teardown doesn't kill it.
  // Earlier runs hit a vicious cycle: test failure → worker teardown →
  // SIGKILL all spawned children → next test starts with no server →
  // login() WS handshake fails → next test fails too.  Detached + unref
  // breaks that chain — the server survives across tests.
  const proc = spawn('go', ['run', './cmd/server'], {
    cwd: SERVER_DIR,
    env: {
      ...process.env,
      PORT: String(PORT),
      // Ensure go toolchain is on PATH even if Playwright's spawned
      // process doesn't inherit the user's interactive shell profile.
      PATH: `/opt/homebrew/bin:${process.env.PATH || ''}`,
    },
    // detached requires ignore stdio — pipes keep the parent attached
    // and let Playwright tear them down with the worker.  Server logs
    // go to /tmp/pw-crash-restart-server.log instead so we can still
    // inspect them post-mortem if a cycle fails mysteriously.
    stdio: 'ignore',
    detached: true,          // new process group — survives parent exit
  });
  proc.unref();              // don't keep Node alive waiting for it
  proc.on('error', (err) => {
    process.stderr.write(`[crash-restart go-run ${proc.pid}] SPAWN ERROR: ${err.message}\n`);
  });
  spawnedProcs.push(proc);
  // 60s budget: first invocation may compile (~3-5s); subsequent ones
  // use the build cache and start in ~1s.
  await waitForServer(60000);
  return proc;
}

/**
 * Kill + restart the server, returning when the new instance is ready.
 * Returns the new child process (for tracking). The caller does NOT need
 * to keep the process — we always re-find by port — but keeping a ref
 * prevents Node from garbage-collecting and signaling exit.
 */
async function restartServer() {
  await killServer();
  return startServer();
}

// ---------------------------------------------------------------------------
// Page helpers
// ---------------------------------------------------------------------------

async function login(page) {
  await page.goto('/');
  await page.fill('#login-username', 'CrashTest');
  await page.click('#login-form button[type="submit"]');
  await expect(page.locator('#lobby-screen.active')).toBeVisible({ timeout: 10000 });
  // Confirm the WS is actually connected before clicking lobby buttons.
  await page.waitForFunction(
    () => {
      const app = window.__paperWarApp;
      return app && app.connection && app.connection.connected;
    },
    { timeout: 10000 },
  );
}

async function startSolo(page) {
  // Ensure connection is live before clicking.
  await page.waitForFunction(
    () => window.__paperWarApp && window.__paperWarApp.connection &&
           window.__paperWarApp.connection.connected,
    { timeout: 10000 },
  );
  await page.click('#solo-btn');
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 15000 });
}

async function startClash(page) {
  await page.waitForFunction(
    () => window.__paperWarApp && window.__paperWarApp.connection &&
           window.__paperWarApp.connection.connected,
    { timeout: 10000 },
  );
  // Clicking #clash-btn shows the clash CONFIG screen, not the game.
  await page.click('#clash-btn');
  await expect(page.locator('#clash-screen.active, #clash-config.active').first()).toBeVisible({ timeout: 5000 });
  // Click "Start Battle" on the config screen to actually start the match.
  await page.click('#clash-start-btn');
  await expect(page.locator('#game-screen.active')).toBeVisible({ timeout: 20000 });
}

async function waitForLobby(page, timeoutMs = 20000) {
  await expect(page.locator('#lobby-screen.active')).toBeVisible({ timeout: timeoutMs });
}

async function getConnectionState(page) {
  return page.evaluate(() => {
    const app = window.__paperWarApp;
    const conn = app && app.connection;
    if (!conn) return { present: false };
    return {
      present: true,
      connected: conn.connected,
      hasWs: !!(conn.ws && conn.ws.readyState === 1), // OPEN
      reconnectToken: conn.reconnectToken,
      hasGame: !!window.__paperWarGame,
    };
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Serialize so server-lifecycle manipulation doesn't collide with other specs.
test.describe.configure({ mode: 'serial' });

test.describe('Issue #17 — crash/restart regression (solo mode)', () => {
  test.beforeAll(async () => {
    // Ensure a server is up before the suite starts (restart if needed).
    if (!findServerPID()) {
      await startServer();
    }
  });

  test.afterAll(async () => {
    // Always leave a server running for subsequent specs.
    if (!findServerPID()) {
      await startServer();
    }
  });

  test('survives 3 cycles of server crash → restart → new solo match', async ({ page }) => {
    test.setTimeout(300000); // 5 min — 3 cycles × (kill/restart + 3s cleanupGame delay)

    await login(page);

    for (let cycle = 1; cycle <= 3; cycle++) {
      // Start a solo match
      await startSolo(page);

      // Confirm the match actually started — snapshot arrives, game object set.
      await page.waitForFunction(
        () => window.__paperWarGame && window.__paperWarGame.state &&
               window.__paperWarGame.state.units && window.__paperWarGame.state.units.size > 0,
        { timeout: 10000 },
      );

      const stateBefore = await getConnectionState(page);
      expect(stateBefore.hasGame, 'game instance should exist mid-match').toBe(true);
      expect(stateBefore.reconnectToken, 'solo match should have reconnect token').toBeTruthy();

      // Kill the server mid-match.
      await restartServer();

      // The client should detect the drop and return to the lobby (via
      // reconnect_failed → cleanupGame, since the new server has no state).
      await waitForLobby(page, 20000);

      // Verify the client is in a clean state to start a new match.
      // NOTE: cleanupGame() runs inside a 3-second setTimeout in the
      // reconnect_failed handler, so we need to wait at least that long.
      // We check app.game (not window.__paperWarGame) because cleanupGame
      // nulls this.game but leaves the global reference set.
      await page.waitForFunction(
        () => {
          const app = window.__paperWarApp;
          const conn = app && app.connection;
          if (!conn || !conn.connected) return false;
          if (app.game) return false;            // game instance cleaned up
          if (conn.reconnectToken) return false; // token cleared
          return true;
        },
        { timeout: 30000 },
      );

      // The lobby buttons should be enabled (not disabled by a stale overlay).
      const soloDisabled = await page.getAttribute('#solo-btn', 'disabled');
      expect(soloDisabled, `cycle ${cycle}: solo-btn must not be disabled`).toBeNull();
    }
  });
});

test.describe('Issue #17 — crash/restart regression (clash mode)', () => {
  test.beforeAll(async () => {
    if (!findServerPID()) {
      await startServer();
    }
  });

  test.afterAll(async () => {
    if (!findServerPID()) {
      await startServer();
    }
  });

  test('survives 3 cycles of server crash → restart → new clash match', async ({ page }) => {
    test.setTimeout(420000); // 7 min — clash spawns 20v20 (slower startup)

    await login(page);

    for (let cycle = 1; cycle <= 3; cycle++) {
      await startClash(page);

      // Wait for the clash match to actually populate (default 5v5 = 10 units).
      await page.waitForFunction(
        () => window.__paperWarGame && window.__paperWarGame.state &&
               window.__paperWarGame.state.units && window.__paperWarGame.state.units.size >= 10,
        { timeout: 15000 },
      );

      const stateBefore = await getConnectionState(page);
      expect(stateBefore.hasGame, `cycle ${cycle}: game should exist mid-clash`).toBe(true);
      // Clash spectator has no reconnect token.
      expect(stateBefore.reconnectToken, `cycle ${cycle}: clash should have NO reconnect token`).toBeNull();

      await restartServer();

      // Clash path: no token → onDisconnect calls cleanupGame immediately.
      // The client should land back in the lobby.
      await waitForLobby(page, 20000);

      await page.waitForFunction(
        () => {
          const app = window.__paperWarApp;
          const conn = app && app.connection;
          if (!conn || !conn.connected) return false;
          if (app.game) return false;
          return true;
        },
        { timeout: 30000 },
      );

      const clashDisabled = await page.getAttribute('#clash-btn', 'disabled');
      expect(clashDisabled, `cycle ${cycle}: clash-btn must not be disabled`).toBeNull();
    }
  });
});
