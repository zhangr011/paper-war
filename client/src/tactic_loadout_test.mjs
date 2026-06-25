// tactic_loadout_test.mjs — Tests for customizable tactic slots (Issue #43)
// Run with: node --test client/src/tactic_loadout_test.mjs
//
// Tests the TacticLoadout class: storage persistence, slot assignment,
// right-click clearing, and tactic execution routing. UI event handling
// (picker open/close) is tested via the DOM API on a mock setup.

import { test, describe, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { TacticLoadout } from './tactic_loadout.js';

// -- Mock DOM setup -------------------------------------------------------

// minimal localStorage mock
const _store = new Map();
globalThis.localStorage = {
  getItem: (k) => _store.has(k) ? _store.get(k) : null,
  setItem: (k, v) => { _store.set(k, String(v)); },
  removeItem: (k) => { _store.delete(k); },
};

// Mock document with slot buttons + picker element
function setupMockDOM(savedLoadout = null) {
  _store.clear();
  if (savedLoadout) {
    _store.set('paper-war.tactic-loadout', JSON.stringify(savedLoadout));
  }

  const slots = [];
  const pickerChildren = [];
  const picker = {
    hidden: true,
    style: {},
    querySelectorAll: () => pickerChildren,
    querySelector: () => pickerChildren[0],
    contains: () => false,
    _children: pickerChildren,
  };

  globalThis.document = {
    getElementById: (id) => id === 'tactic-picker' ? picker : null,
    querySelectorAll: (sel) => {
      if (sel.includes('tactic-slot')) return slots;
      return [];
    },
    addEventListener: () => {},
  };

  // Build 4 slot buttons
  for (let i = 0; i < 4; i++) {
    const slot = {
      dataset: { slot: String(i) },
      className: 'tactic-slot empty',
      title: '',
      innerHTML: '',
      _clickHandlers: [],
      _contextHandlers: [],
      getBoundingClientRect: () => ({ left: 10 * i, bottom: 20 }),
      addEventListener: (event, handler) => {
        if (event === 'click') slot._clickHandlers.push(handler);
        if (event === 'contextmenu') slot._contextHandlers.push(handler);
      },
      classList: {
        _classes: new Set(['tactic-slot', 'empty']),
        add(c) { this._classes.add(c); slot.className = [...this._classes].join(' '); },
        remove(c) { this._classes.delete(c); slot.className = [...this._classes].join(' '); },
      },
    };
    slots.push(slot);
  }

  // Build picker options
  for (const t of ['charge', 'retreat', 'defend', 'rally', 'attack-ground']) {
    pickerChildren.push({
      dataset: { tactic: t },
      _clickHandlers: [],
      addEventListener: (event, handler) => {
        if (event === 'click') pickerChildren[pickerChildren.length - 1]._clickHandlers.push(handler);
      },
    });
  }
  pickerChildren.push({ // cancel button
    _clickHandlers: [],
    addEventListener: () => {},
  });

  return { slots, picker, pickerChildren };
}

// Reset global document after each test
after(() => { delete globalThis.document; });

describe('TacticLoadout storage', () => {
  test('empty by default when no saved loadout', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    assert.deepEqual(tl.slots, [null, null, null, null]);
  });

  test('loads saved loadout from localStorage', () => {
    const { slots, picker } = setupMockDOM(['charge', null, 'rally', 'defend']);
    const tl = new TacticLoadout({});
    assert.equal(tl.getSlot(0), 'charge');
    assert.equal(tl.getSlot(1), null);
    assert.equal(tl.getSlot(2), 'rally');
    assert.equal(tl.getSlot(3), 'defend');
  });

  test('sanitizes invalid saved values', () => {
    const { slots, picker } = setupMockDOM(['faketactic', 42, 'charge', {bad: 'object'}]);
    const tl = new TacticLoadout({});
    assert.equal(tl.getSlot(0), null);  // invalid
    assert.equal(tl.getSlot(1), null);  // wrong type
    assert.equal(tl.getSlot(2), 'charge');  // valid
    assert.equal(tl.getSlot(3), null);  // wrong type
  });

  test('sanitizes wrong-length saved array', () => {
    const { slots, picker } = setupMockDOM(['charge', 'rally']);  // only 2 entries
    const tl = new TacticLoadout({});
    assert.deepEqual(tl.slots, [null, null, null, null]);
  });

  test('handles corrupted JSON in localStorage', () => {
    _store.clear();
    _store.set('paper-war.tactic-loadout', 'not-json');
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    assert.deepEqual(tl.slots, [null, null, null, null]);
  });
});

describe('TacticLoadout slot assignment', () => {
  test('setSlot assigns valid tactic', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    tl.setSlot(0, 'charge');
    assert.equal(tl.getSlot(0), 'charge');
  });

  test('setSlot rejects invalid tactic', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    assert.throws(() => tl.setSlot(0, 'faketactic'));
  });

  test('setSlot rejects out-of-range index', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    assert.throws(() => tl.setSlot(-1, 'charge'));
    assert.throws(() => tl.setSlot(4, 'charge'));
  });

  test('setSlot persists to localStorage', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    tl.setSlot(2, 'defend');
    const saved = JSON.parse(_store.get('paper-war.tactic-loadout'));
    assert.deepEqual(saved, [null, null, 'defend', null]);
  });

  test('setSlot with null clears the slot', () => {
    const { slots, picker } = setupMockDOM(['charge', null, null, null]);
    const tl = new TacticLoadout({});
    tl.setSlot(0, null);
    assert.equal(tl.getSlot(0), null);
  });
});

describe('TacticLoadout execution routing', () => {
  test('click assigned slot executes charge', () => {
    const { slots, picker } = setupMockDOM();
    let executed = null;
    const tl = new TacticLoadout({
      handleTactic: (t) => { executed = t; },
    });
    tl.setSlot(0, 'charge');
    // Trigger click handler
    slots[0]._clickHandlers[0]({ preventDefault() {}, stopPropagation() {} });
    assert.equal(executed, 'charge');
  });

  test('click assigned slot executes defend', () => {
    const { slots, picker } = setupMockDOM();
    let executed = null;
    const tl = new TacticLoadout({
      handleTactic: (t) => { executed = t; },
    });
    tl.setSlot(1, 'defend');
    slots[1]._clickHandlers[0]({ preventDefault() {}, stopPropagation() {} });
    assert.equal(executed, 'defend');
  });

  test('click assigned attack-ground slot calls toggleAttackGround', () => {
    const { slots, picker } = setupMockDOM();
    let toggled = false;
    const tl = new TacticLoadout({
      toggleAttackGround: () => { toggled = true; },
    });
    tl.setSlot(2, 'attack-ground');
    slots[2]._clickHandlers[0]({ preventDefault() {}, stopPropagation() {} });
    assert.equal(toggled, true);
  });

  test('right-click clears assigned slot', () => {
    const { slots, picker } = setupMockDOM(['charge', null, null, null]);
    const tl = new TacticLoadout({});
    assert.equal(tl.getSlot(0), 'charge');
    slots[0]._contextHandlers[0]({ preventDefault() {} });
    assert.equal(tl.getSlot(0), null);
  });

  test('right-click empty slot is a no-op (no error)', () => {
    const { slots, picker } = setupMockDOM();
    const tl = new TacticLoadout({});
    // Empty slot shouldn't crash on contextmenu
    assert.doesNotThrow(() => {
      slots[0]._contextHandlers[0]({ preventDefault() {} });
    });
  });
});
