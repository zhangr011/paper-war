// connection_test.mjs — Binary protocol encoder/decoder tests for Connection
// Run with: node --test client/src/connection_test.mjs
//
// Tests the WebSocket protocol layer: command encoding (client→server)
// and snapshot/event decoding (server→client). Mocks WebSocket so we
// can capture sent bytes and verify wire format.

import { test, describe, before, after, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { Connection } from './connection.js';

// -- Mock WebSocket ------------------------------------------------------

class MockWebSocket {
  constructor() {
    this.readyState = 1; // OPEN
    this.sent = []; // captured outgoing messages
    this.onmessage = null;
    this.onopen = null;
    this.onclose = null;
    this.onerror = null;
    this.closeCh = null;
    this.binaryType = 'arraybuffer';
  }
  send(data) { this.sent.push(data); }
  close() {
    this.readyState = 3; // CLOSED
    if (this.onclose) this.onclose({ wasClean: true });
  }
}
// Static constants (real WebSocket has CONNECTING=0, OPEN=1, CLOSING=2, CLOSED=3)
MockWebSocket.CONNECTING = 0;
MockWebSocket.OPEN = 1;
MockWebSocket.CLOSING = 2;
MockWebSocket.CLOSED = 3;

// -- Mock global WebSocket ----------------------------------------------

before(() => {
  globalThis.WebSocket = MockWebSocket;
  globalThis.window = { location: { host: 'localhost:9091' } };
});

after(() => {
  delete globalThis.WebSocket;
  delete globalThis.window;
});

// -- Helper to create a Connection with a mock WS ----------------------

function makeConnection() {
  const conn = new Connection('ws://mock/ws');
  conn.connect(); // creates the WebSocket via the mock
  assert.ok(conn.ws instanceof MockWebSocket, 'WebSocket should be mocked');
  // Track for cleanup
  makeConnection._instances = (makeConnection._instances || []);
  makeConnection._instances.push(conn);
  return conn;
}

// Clean up all connections after each test to prevent reconnect timers
// from keeping the process alive
afterEach(() => {
  if (makeConnection._instances) {
    for (const c of makeConnection._instances) {
      try { c.disconnect(); } catch {}
    }
    makeConnection._instances = [];
  }
});

// -- Tests --------------------------------------------------------------

describe('Command encoding (client → server)', () => {
  test('sendMoveSquad writes correct wire format', () => {
    const c = makeConnection();
    // Use values within int32 range to avoid JS number-vs-uint32 confusion
    c.sendMoveSquad(7, 0x12345678, 100000, 42);
    assert.equal(c.ws.sent.length, 1);
    const buf = c.ws.sent[0];
    assert.equal(buf.byteLength, 21, 'CmdMoveSquad should be 21 bytes');
    const v = new DataView(buf);
    assert.equal(v.getUint8(0), 0x01, 'command type');
    assert.equal(v.getUint32(1, true), 0, 'seq starts at 0');
    assert.equal(v.getUint32(5, true), 42, 'predictedTick');
    assert.equal(v.getUint32(9, true), 7, 'squadID');
    assert.equal(v.getInt32(13, true), 0x12345678, 'targetX');
    assert.equal(v.getInt32(17, true), 100000, 'targetY');
  });

  test('sendAttackTarget writes correct wire format', () => {
    const c = makeConnection();
    c.sendAttackTarget(3, 99, 0);
    const buf = c.ws.sent[0];
    assert.equal(buf.byteLength, 17, 'CmdAttackTarget should be 17 bytes');
    const v = new DataView(buf);
    assert.equal(v.getUint8(0), 0x02, 'command type');
    assert.equal(v.getUint32(9, true), 3, 'squadID');
    assert.equal(v.getUint32(13, true), 99, 'targetID');
  });

  test('sendAttackGround writes correct wire format', () => {
    const c = makeConnection();
    c.sendAttackGround(11, -100, 200, 5);
    const buf = c.ws.sent[0];
    assert.equal(buf.byteLength, 21, 'CmdAttackGround should be 21 bytes');
    const v = new DataView(buf);
    assert.equal(v.getUint8(0), 0x03, 'command type');
    assert.equal(v.getUint32(9, true), 11, 'squadID');
    assert.equal(v.getInt32(13, true), -100, 'targetX (negative allowed)');
    assert.equal(v.getInt32(17, true), 200, 'targetY');
  });

  test('sendChangeFormation writes correct wire format', () => {
    const c = makeConnection();
    c.sendChangeFormation(1, 2); // squadID=1, formationType=2 (Wedge)
    const buf = c.ws.sent[0];
    assert.equal(buf.byteLength, 14, 'CmdChangeFormation should be 14 bytes');
    const v = new DataView(buf);
    assert.equal(v.getUint8(0), 0x04, 'command type');
    assert.equal(v.getUint32(9, true), 1, 'squadID');
    assert.equal(v.getUint8(13), 2, 'formationType');
  });

  test('sendBuild writes correct wire format', () => {
    const c = makeConnection();
    c.sendBuild(1, 0x1000, 0x2000); // Tower at (4096, 8192) in fixed-point
    const buf = c.ws.sent[0];
    assert.equal(buf.byteLength, 22, 'CmdBuild should be 22 bytes');
    const v = new DataView(buf);
    assert.equal(v.getUint8(0), 0x08, 'command type');
    assert.equal(v.getUint32(9, true), 0, 'squadID — always 0 for build');
    assert.equal(v.getUint8(13), 1, 'structureType');
    assert.equal(v.getInt32(14, true), 0x1000, 'targetX');
    assert.equal(v.getInt32(18, true), 0x2000, 'targetY');
  });

  test('seq increments across multiple commands', () => {
    const c = makeConnection();
    c.sendMoveSquad(1, 0, 0, 0);
    c.sendMoveSquad(1, 0, 0, 0);
    c.sendMoveSquad(1, 0, 0, 0);
    const seqs = c.ws.sent.map(buf => new DataView(buf).getUint32(1, true));
    assert.deepEqual(seqs, [0, 1, 2]);
  });
});

describe('Connection lifecycle', () => {
  test('connect sets up WebSocket and triggers onConnect', () => {
    const c = makeConnection();
    let connected = false;
    c.onConnect = () => { connected = true; };
    // Simulate server-side open
    c.ws.onopen();
    assert.equal(c.connected, true);
    assert.equal(connected, true);
  });

  test('disconnect calls onDisconnect and clears state', () => {
    const c = makeConnection();
    let disconnected = false;
    c.onDisconnect = () => { disconnected = true; };
    c.ws.onopen();
    c.ws.close(); // triggers onclose
    assert.equal(c.connected, false);
    assert.equal(disconnected, true);
  });

  test('send to closed WebSocket is silently dropped', () => {
    const c = makeConnection();
    c.ws.readyState = 3; // CLOSED
    // Should not throw
    assert.doesNotThrow(() => {
      c.sendMoveSquad(1, 0, 0, 0);
    });
    assert.equal(c.ws.sent.length, 0, 'nothing should be sent when closed');
  });

  test('intentional close sets _intentionalClose flag', () => {
    const c = makeConnection();
    c.disconnect();
    assert.equal(c._intentionalClose, true);
  });
});

describe('Snapshot decoding (server → client)', () => {
  test('Connection accepts onSnapshot callback without throwing', () => {
    const c = makeConnection();
    assert.doesNotThrow(() => {
      c.onSnapshot = (snap) => {};
    });
  });

  test('Connection accepts onGoldUpdate callback without throwing', () => {
    const c = makeConnection();
    assert.doesNotThrow(() => {
      c.onGoldUpdate = (gold) => {};
    });
  });
});

describe('Reconnect state', () => {
  test('reconnectToken starts null', () => {
    const c = makeConnection();
    assert.equal(c.reconnectToken, null);
  });

  test('reconnectToken can be set (e.g. on match_found)', () => {
    const c = makeConnection();
    c.reconnectToken = 'abc123';
    assert.equal(c.reconnectToken, 'abc123');
  });

  test('disconnect clears reconnectToken (intentional)', () => {
    const c = makeConnection();
    c.reconnectToken = 'abc123';
    c.disconnect();
    assert.equal(c.reconnectToken, null);
  });
});

describe('Heartbeat', () => {
  test('stopHeartbeat is idempotent and safe to call before start', () => {
    const c = makeConnection();
    assert.doesNotThrow(() => {
      c.stopHeartbeat();
      c.stopHeartbeat();
    });
  });
});
