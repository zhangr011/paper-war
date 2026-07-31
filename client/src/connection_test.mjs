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

// Stronghold state message shape — the stronghold_state JSON message was
// widened (terrain-polish plan) to carry hp, max_hp, and garrison alongside
// the existing x/y/level/faction. The Connection layer is pass-through for
// JSON messages (the handler lives in app.js), so this test pins the wire
// shape: a server sending the widened payload round-trips through JSON.parse
// unchanged and the new fields survive with the expected types/values.
describe('Stronghold state wire shape', () => {
  test('stronghold_state payload carries hp / max_hp / garrison alongside x/y/level/faction', () => {
    const payload = {
      type: 'stronghold_state',
      strongholds: [
        { x: 12, y: 20, level: 3, faction: 0, hp: 410, max_hp: 650, garrison: 2 },
        { x: 40, y: 8,  level: 5, faction: 0xFF, hp: 950, max_hp: 950, garrison: 0 },
      ],
    };
    // Simulate the wire: JSON.stringify on the server, JSON.parse on the client.
    const round = JSON.parse(JSON.stringify(payload));
    assert.equal(round.type, 'stronghold_state');
    assert.equal(round.strongholds.length, 2);
    const s = round.strongholds[0];
    assert.equal(s.x, 12);
    assert.equal(s.y, 20);
    assert.equal(s.level, 3);
    assert.equal(s.faction, 0);
    assert.equal(s.hp, 410, 'hp field present');
    assert.equal(s.max_hp, 650, 'max_hp field present');
    assert.equal(s.garrison, 2, 'garrison field present');
    // Neutral stronghold (pre-siege) — garrison 0 is a valid state and must
    // survive the round-trip so client-side pip rendering can degrade to
    // "empty pips" rather than NaN/undefined.
    const neutral = round.strongholds[1];
    assert.equal(neutral.faction, 0xFF);
    assert.equal(neutral.garrison, 0);
    assert.equal(neutral.hp, neutral.max_hp, 'full HP at neutral spawn');
  });

  test('older stronghold_state payloads (no hp/max_hp/garrison) still parse', () => {
    // Backward compat: an older server that only emits x/y/level/faction
    // must keep parsing. The client falls back to strongholdMaxHP(level)
    // for the HP-bar denominator and treats missing garrison as 0.
    const legacy = {
      type: 'stronghold_state',
      strongholds: [{ x: 5, y: 6, level: 1, faction: 0 }],
    };
    const round = JSON.parse(JSON.stringify(legacy));
    const s = round.strongholds[0];
    assert.equal(s.hp, undefined);
    assert.equal(s.max_hp, undefined);
    assert.equal(s.garrison, undefined);
    assert.equal(s.level, 1);
  });
});
