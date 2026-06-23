# Phase 1 — Regression Guard (crash/restart, bug #17)

**Status:** ✅ Complete — 17/17 e2e specs pass (42.5s)
**Date:** 2026-06-20
**Spec:** `tests/e2e/zz-crash-restart.spec.js` (renamed from `crash-restart.spec.js` to run LAST alphabetically — Playwright runs files in alphabetical order, and this spec kills the shared server mid-cycle)

## Bug #17 — Crash/Restart Regression Guard

The spec covers both modes:
- **Solo mode** (`crash-restart.spec.js:241`): 3 cycles of (login → start solo match → crash server → restart server → verify clean reconnect + new match)
- **Clash mode** (`crash-restart.spec.js:305`): 3 cycles of the same flow with a 2-player clash match

Each cycle verifies:
1. `__paperWarApp.connection.connected === true` (WS reconnect succeeds with the new server)
2. No stale `reconnectToken` left over from the previous server (would block new match)
3. A new match can be started from the lobby after the restart

## Bugs Found & Fixed During Phase 1

### Bug A — `killServer()` race: port not released before restart
**Root cause:** `killServer()` had an un-awaited `sleep(200)` Promise, making its port-release busy-loop a no-op.  `restartServer()` then raced the dying listener; the new `go run` hit `bind: address already in use` and exited silently (stderr was being drained without logging).  `waitForServer` spun forever against a port with no listener.

**Fix:** Make `killServer` async, properly await the sleep, surface stderr via tagged logging.

### Bug B — Playwright worker teardown killed the spawned server
**Root cause:** Even with Bug A fixed, `spawn()` (without `detached: true`) puts the child in the same process group as the Playwright worker.  On any test failure the worker's teardown SIGKILL'd all spawned children, leaving the next test with no server.  Symptom: `login()` failed with `WebSocket error: ... wsState: 3 (CLOSED)` because the server was dead before login ran.

**Fix:** `detached: true` + `proc.unref()` + `stdio: 'ignore'` (stdio pipes would keep the parent attached and defeat detachment).  Server logs go to /tmp instead of pipes; spawn errors are still surfaced via the `'error'` event.

### Bug C — Parallel workers + shared server = chaos
**Root cause:** Playwright's `fullyParallel: false` only affects WITHIN-file test ordering.  Different spec files were running in parallel (5 workers by default).  The crash-restart spec's mid-test `killServer()` killed the server that fog/map-generation specs were depending on simultaneously.

**Fix:** `workers: 1` in `playwright.config.js`.  Documented why with a comment.

### Bug D — Snapshot race in `enemy-units-in-fogged-tiles` test
**Root cause:** `waitForFog` only checked `state.fogVisible` length, not `state.units`.  Sometimes the first unit-bearing snapshot arrived 1-2 ticks after the first fog-bearing snapshot; the test would then read `state.units.size === 0` and fail the assertion.

**Real bug?** No — server behavior is correct (units populate within ~200ms of fog).  Test race only.

**Fix:** Added `waitForFunction(units.size > 0)` with a 3s budget before querying units.  Also added diagnostic context to the assertion message so future failures are debuggable from output instead of opaque zeroes.

## Files Changed

| File | Change |
|------|--------|
| `tests/e2e/zz-crash-restart.spec.js` | NEW — 2 specs (solo + clash), 3 cycles each, kills server mid-match and verifies reconnect |
| `tests/e2e/fog.spec.js` | Fixed snapshot race in `enemy units are not present in fogged tiles` (Bug D) |
| `playwright.config.js` | Added `workers: 1` to prevent cross-spec server collisions (Bug C) |

## Repro

Server: `cd server && PORT=9091 go run ./cmd/server` (started externally before the suite)
Suite:  `npx playwright test` (uses workers=1, runs all 17 specs in alphabetical order)

## Notes for Phase 2

- The `global-setup.js` still spawns a stale prebuilt `./server/server` binary that fails to bind against the external server — harmless noise at suite start (`Server process error: ... listen tcp :9091: bind: address already in use`), but ugly.  Phase 2 cleanup candidate.
- Spec naming convention `zz-*.spec.js` for stateful specs that must run last is now established.
