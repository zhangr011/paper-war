// input_test.mjs — Tests for InputHandler keyboard dispatch (Issue #35)
// Run with: node --test client/src/input_test.mjs
//
// Regression test for #35: _onKeyDown used to handle tactical hotkeys
// inline (with a desynced keymap sending to a dead server path) and never
// invoked this.onKeyDown. Result: every binding in main.js's onKeyDown
// (A=attack-ground, Escape=cancel, Space=jump-to-base)
// was dead code. Q/W/E/R "worked" but only via the stale duplicate.

import assert from 'node:assert/strict';

// --- Mocks -------------------------------------------------------------

class MockCamera {
  constructor() { this.x = 0; this.y = 0; this.zoom = 1; }
  pan(dx, dy) { this.x += dx; this.y += dy; }
  screenToWorld(sx, sy) { return [sx, sy]; }
  zoomAt() {}
}

class MockConnection {
  constructor() { this.sent = []; }
  sendTacticalOrder(squadID, orderType) {
    this.sent.push({ type: 'tactical', squadID, orderType });
  }
  sendMoveSquad() {}
}

// Minimal fake canvas/input target. InputHandler only uses addEventListener
// and getBoundingClientRect on canvas.
function fakeCanvas() {
  const handlers = {};
  return {
    addEventListener: (ev, fn) => { handlers[ev] = fn; },
    removeEventListener: () => {},
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 600 }),
    _handlers: handlers,
  };
}

// Instantiate InputHandler WITHOUT attaching to a real DOM, by stubbing
// window.addEventListener. We use jsdom-less approach: import the module
// and let it call window.addEventListener (which we mock globally).

import { InputHandler } from './input.js';

let passed = 0, failed = 0;
function test(name, fn) {
  try { fn(); console.log(`  ✓ ${name}`); passed++; }
  catch (e) { console.error(`  ✗ ${name}\n    ${e.message}`); failed++; }
}

// Build a handler instance. attach() registers listeners on window+canvas.
// We mock window.addEventListener to swallow the calls (we'll invoke
// _onKeyDown directly via the bound reference).
function makeHandler() {
  const canvas = fakeCanvas();
  const camera = new MockCamera();
  const conn = new MockConnection();
  // Stub global window event registration
  const origAdd = globalThis.window?.addEventListener;
  const origRemove = globalThis.window?.removeEventListener;
  globalThis.window = globalThis.window || {};
  globalThis.window.addEventListener = () => {};
  globalThis.window.removeEventListener = () => {};
  const h = new InputHandler(canvas, camera, conn);
  if (origAdd) globalThis.window.addEventListener = origAdd;
  if (origRemove) globalThis.window.removeEventListener = origRemove;
  return { h, canvas, camera, conn };
}

// Helper: synthesize a keydown event
function keydown(key, opts = {}) {
  return { key, repeat: !!opts.repeat, preventDefault: () => {} };
}

// -----------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------

console.log('InputHandler keyboard dispatch tests (Issue #35)\n');

test('onKeyDown callback is invoked with the pressed key', () => {
  const { h } = makeHandler();
  const seen = [];
  h.onKeyDown = (k) => seen.push(k);
  h._onKeyDown(keydown('a'));
  assert.deepEqual(seen, ['a'], `expected ['a'], got ${JSON.stringify(seen)}`);
});

test('onKeyDown fires for all gameplay hotkeys', () => {
  const { h } = makeHandler();
  const seen = [];
  h.onKeyDown = (k) => seen.push(k);
  for (const k of ['a', 'A', 'q', 'w', 'e', 'r', '1', '2', '3', '4', 'Escape', ' ']) {
    h._onKeyDown(keydown(k));
  }
  assert.equal(seen.length, 12, `expected 12 callbacks, got ${seen.length}`);
});

test('keys Set is still updated (WASD pan continues to work)', () => {
  const { h } = makeHandler();
  h.onKeyDown = () => {};
  assert.equal(h.keys.has('a'), false, 'a not in set before press');
  h._onKeyDown(keydown('a'));
  assert.equal(h.keys.has('a'), true, 'a in set after press');
  h._onKeyUp({ key: 'a' });
  assert.equal(h.keys.has('a'), false, 'a removed on keyup');
});

test('OS auto-repeat does NOT re-invoke onKeyDown (toggle safety)', () => {
  // Without this guard, holding 'A' to pan would rapidly flip attack-ground
  // mode on/off, and holding 'q' would flood the server with tactic commands.
  const { h } = makeHandler();
  const seen = [];
  h.onKeyDown = (k) => seen.push(k);
  // Initial press (not a repeat)
  h._onKeyDown(keydown('a'));
  // Auto-repeat events while key is held
  h._onKeyDown(keydown('a', { repeat: true }));
  h._onKeyDown(keydown('a', { repeat: true }));
  h._onKeyDown(keydown('a', { repeat: true }));
  assert.equal(seen.length, 1, `expected 1 callback (initial only), got ${seen.length}`);
});

test('keys Set is still updated on auto-repeat (pan keeps working while held)', () => {
  const { h } = makeHandler();
  h.onKeyDown = () => {};
  h._onKeyDown(keydown('w'));
  h._onKeyDown(keydown('w', { repeat: true }));
  assert.equal(h.keys.has('w'), true, 'w remains in set during auto-repeat');
});

test('missing onKeyDown callback does not throw', () => {
  const { h } = makeHandler();
  h.onKeyDown = null; // explicit
  assert.doesNotThrow(() => h._onKeyDown(keydown('a')));
});

test('stale sendTacticalOrder path is no longer triggered by input.js (q/w/e/r)', () => {
  // Issue #35: input.js used to handle q/w/e/r inline via
  // connection.sendTacticalOrder, which is a desynced dead path — the
  // server stores TacticalState but no system reads it, and the keymap
  // (q→0=Follow) didn't match main.js (q→'charge'). main.js's handleTactic
  // is the real implementation. Verify input.js no longer sends the
  // stale command on tactical hotkeys.
  const { h, conn } = makeHandler();
  h.selectedSquads.add(1);
  h.onKeyDown = () => {}; // main.js would handle these
  for (const k of ['q', 'w', 'e', 'r']) {
    h._onKeyDown(keydown(k));
  }
  assert.equal(conn.sent.length, 0,
    `input.js must not send tactical orders directly, got ${JSON.stringify(conn.sent)}`);
});

// -----------------------------------------------------------------------
console.log(`\n${'─'.repeat(50)}`);
console.log(`${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
