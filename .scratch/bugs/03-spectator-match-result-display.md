---
Title: Defeat and victory display unexpectedly in clash/spectator mode
Status: open
Severity: bug
Type: frontend
Created: 2026-05-31
---

# Defeat and victory display unexpectedly in clash/spectator mode

## Description

When a match ends (elimination), the client shows "Defeat" or "Victory" based on the player's faction vs the winner. In spectator mode (playerID=0), the client has no faction — so the result message is unexpected/confusing.

## Steps to Reproduce

1. Open client at localhost:9091
2. Log in, click "Clash Test"
3. Watch AI vs AI match until elimination
4. Observe the result display

## Expected Behavior

- Spectator should see a neutral result: "Match Over — Team X Wins" (or similar)
- Solo mode should continue showing Victory/Defeat as before

## Actual Behavior

- Spectator sees an incorrect/unexpected victory/defeat message (or no meaningful message)

## Root Cause

The `MsgMatchResult` handler in the client compares the winner faction against the player's faction. Spectator has playerID=0 and no faction, so the comparison is wrong.

## Fix

In the client-side match result handler, check if `playerID === 0` (spectator) and show a neutral outcome message instead of Victory/Defeat.

## Affected Files

- `client/src/main.js` — match result display logic
- `client/src/app.js` — possible screen transition on match end
