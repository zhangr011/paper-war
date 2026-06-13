const { exec } = require('child_process');
const http = require('http');
const path = require('path');

const SERVER_PORT = 9091;
const SERVER_URL = `http://localhost:${SERVER_PORT}`;
const STARTUP_TIMEOUT = 10000;
const POLL_INTERVAL = 200;

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

async function globalSetup() {
  const serverDir = path.join(__dirname, '../../server');
  const serverBin = path.join(serverDir, 'server');

  serverProcess = exec(serverBin, { cwd: serverDir }, (err) => {
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
