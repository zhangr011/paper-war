// match_result_test.mjs — Regression test for bug #03
// "Defeat and victory display unexpectedly in clash/spectator mode"
//
// Run with: node client/src/match_result_test.mjs
//
// Tests formatMatchResultHeading(), the pure helper extracted from
// Game.showMatchResult.  Before the fix, the spectator (playerID === 0)
// saw "Victory" or "Defeat" based on a meaningless comparison against
// their non-existent faction.  After the fix, spectators see a neutral
// "Team Blue/Red Wins!" message in team colour.

import assert from 'node:assert/strict';
import { formatMatchResultHeading, FACTION_PLAYER, FACTION_ENEMY } from './match_result.js';

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    console.log(`  ✓ ${name}`);
    passed++;
  } catch (e) {
    console.error(`  ✗ ${name}`);
    console.error(`    ${e.message}`);
    failed++;
  }
}

console.log('Bug #03 — spectator match-result display');

test('spectator (pid=0) sees "Team Blue Wins!" when Blue (faction 0) wins', () => {
  const r = formatMatchResultHeading(0, FACTION_PLAYER);
  assert.equal(r.heading, 'Team Blue Wins!');
  assert.equal(r.color, '#4488FF');
});

test('spectator (pid=0) sees "Team Red Wins!" when Red (faction 1) wins', () => {
  const r = formatMatchResultHeading(0, FACTION_ENEMY);
  assert.equal(r.heading, 'Team Red Wins!');
  assert.equal(r.color, '#FF4444');
});

test('spectator never sees "Victory!" or "Defeat"', () => {
  for (const winner of [FACTION_PLAYER, FACTION_ENEMY]) {
    const r = formatMatchResultHeading(0, winner);
    assert.ok(!r.heading.includes('Victory'),
      `spectator heading must not be Victory (winner=${winner}, got "${r.heading}")`);
    assert.ok(!r.heading.includes('Defeat'),
      `spectator heading must not be Defeat (winner=${winner}, got "${r.heading}")`);
  }
});

test('pid=1 (Blue player) sees Victory! when Blue wins', () => {
  const r = formatMatchResultHeading(1, FACTION_PLAYER);
  assert.equal(r.heading, 'Victory!');
  assert.equal(r.color, '#4CAF50');
});

test('pid=1 (Blue player) sees Defeat when Red wins', () => {
  const r = formatMatchResultHeading(1, FACTION_ENEMY);
  assert.equal(r.heading, 'Defeat');
  assert.equal(r.color, '#FF4444');
});

test('pid=2 (Red player) sees Victory! when Red wins', () => {
  const r = formatMatchResultHeading(2, FACTION_ENEMY);
  assert.equal(r.heading, 'Victory!');
  assert.equal(r.color, '#4CAF50');
});

test('pid=2 (Red player) sees Defeat when Blue wins', () => {
  const r = formatMatchResultHeading(2, FACTION_PLAYER);
  assert.equal(r.heading, 'Defeat');
  assert.equal(r.color, '#FF4444');
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
