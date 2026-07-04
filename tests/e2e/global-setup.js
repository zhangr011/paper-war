const { exec } = require('child_process');
const http = require('http');
const path = require('path');

const SERVER_PORT = 9091;
const SERVER_URL = `http://localhost:${SERVER_PORT}`;
const STARTUP_TIMEOUT = 10000;
const POLL_INTERVAL = 200;

// Issue #47 Fix A: pin the map seed in test mode so the multiplayer
// playtest doesn't randomly produce slow-resolution maps. The seed is
// read by server/pkg/game/session.go seedFromEnvOrTime() and by the
// global RNG seeding in cmd/server/main.go.
//
// Override at the command line:
//   PAPER_WAR_TEST_SEED=1234 npx playwright test
const DEFAULT_TEST_SEED = '42';
const testSeed = process.env.PAPER_WAR_TEST_SEED || DEFAULT_TEST_SEED;

let serverProcess = null;

function waitForServer(url, timeout) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeout;
    const poll = () => {
      if (Date.now() > deadline) {
        return reject(new Error(`Server did not start within ${timeout}ms`));
      }
      const req = http.get(url, (res) => {
        res.resume();
        resolve();
      });
      req.on('error', () => setTimeout(poll, POLL_INTERVAL));
    };
    poll();
  });
}

// Issue #47 follow-up: between consecutive `playwright test` invocations
// (e.g. a for-loop in the shell), the previous server's SIGTERM may leave
// port 9091 in TIME_WAIT. The next spawn hits EADDRINUSE and the test
// times out against a missing server. waitForPortFree polls until the
// port is free (or no server responds) before global-setup spawns a new
// one. No-op on the first run of a fresh shell.
function waitForPortFree(port, timeout) {
  return new Promise((resolve) => {
    const deadline = Date.now() + timeout;
    const check = () => {
      const req = http.get(`http://localhost:${port}/`, () => {
        // Server still responding — wait and retry.
        if (Date.now() > deadline) return resolve();
        setTimeout(check, POLL_INTERVAL);
      });
      req.on('error', () => resolve()); // ECONNREFUSED = port free
      req.setTimeout(500, () => {
        req.destroy();
        if (Date.now() > deadline) return resolve();
        setTimeout(check, POLL_INTERVAL);
      });
    };
    check();
  });
}

async function globalSetup() {
  const serverDir = path.join(__dirname, '../../server');
  const serverBin = path.join(serverDir, 'server');

  // Wait for any stale server from a previous run to release the port.
  // Issue #47 follow-up: prevents EADDRINUSE on the new spawn.
  await waitForPortFree(SERVER_PORT, 3000);

  // Issue #47 Fix A: pass PAPER_WAR_TEST_SEED to the server process so
  // map generation + global RNG are deterministic in test mode.
  const serverEnv = {
    ...process.env,
    PAPER_WAR_TEST_SEED: testSeed,
  };
  console.log(`[test mode] PAPER_WAR_TEST_SEED=${testSeed}`);

  serverProcess = exec(serverBin, { cwd: serverDir, env: serverEnv }, (err) => {
    if (err && !err.killed) {
      console.error('Server process error:', err.message);
    }
  });

  serverProcess.stdout?.on('data', (d) => process.stdout.write(d));
  serverProcess.stderr?.on('data', (d) => process.stderr.write(d));

  try {
    await waitForServer(SERVER_URL, STARTUP_TIMEOUT);
    console.log(`Server ready at ${SERVER_URL}`);
  } catch (e) {
    throw new Error(`Failed to start server: ${e.message}`);
  }

  return async () => {
    if (serverProcess) {
      serverProcess.kill('SIGTERM');
      serverProcess = null;
    }
  };
}

module.exports = globalSetup;
