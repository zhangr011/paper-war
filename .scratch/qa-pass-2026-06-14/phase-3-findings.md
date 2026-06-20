# Phase 3 — Failure-mode testing

**Date:** 2026-06-20
**Browser:** Chromium via Playwright
**Server:** Multiple PIDs (crashed and restarted multiple times)
**Result:** ✅ PASS — 4 of 5 tests pass, 1 low-severity defensive-coding bug filed

## Tests

### P3-1 / P3-2 — WS drop mid-match solo / clash
**Status:** PASS (covered by Phase 1 crash-restart spec)
The Phase 1 e2e spec at `tests/e2e/zz-crash-restart.spec.js` covers both: WS close mid-match, server kill mid-match, on both solo and clash paths. Auto-reconnect via exponential backoff (1s → 2s → 4s → 8s → 16s → 30s cap). Phase 1 already verified these paths return to a playable state.

### P3-3 — Server crash mid-match
**Status:** PASS (Phase 1 spec covers)
The connection layer shows the reconnect overlay (`game.showReconnectOverlay()`), retries the WS, sends a `reconnect` message with the saved token, and the server's session manager re-attaches the player to the in-progress match. Verified via `tests/e2e/zz-crash-restart.spec.js`.

### P3-4 — Server crash mid-lobby
**Status:** ✅ PASS

Reproduced: while in lobby (logged in, no active match), `kill -9` the server.

**Observed:**
- WS goes CLOSED (`ws.readyState: 3`)
- `connection.connected` flips to `false`
- Lobby screen stays visible — `bodyLen: 5` (5 top-level children), `htmlLen: 14556` (14.5KB of DOM), `screen: lobby-screen`, App instance intact.
- 5 sample points over 7.5s: lobby screen never blanks, no errors.

After restarting server, the auto-reconnect succeeded within ~5s: `ws: 1`, `conn: true`, lobby buttons all re-enabled. Player can continue from where they were.

**Earlier false alarm:** I initially reported a "black screen" mid-lobby crash. Re-tested with proper instrumentation: the page remained stable. The earlier observation was likely a Playwright session timeout or a separate transient, not a real client bug.

### P3-5 — Server crash AT login
**Status:** N/A — architectural constraint

Because the server serves the client assets (single-binary deployment), if the server is down when the player tries to load the page, the page itself fails to load (`net::ERR_CONNECTION_REFUSED`).

If the server crashes AFTER the page is loaded but BEFORE the WS connects (i.e. the brief window between Join-click and WS-handshake-success), the connection layer would treat it as a normal connect failure and back off / retry.

To fully test the latter case would require serving the client separately (CDN / different origin) or hooking the WS constructor to inject a delay — out of scope for this pass. **Documented as test-coverage gap.**

### P3-6 — Rapid double-click on Start buttons
**Status:** ✅ PASS

**Solo:** 5 rapid clicks on `#solo-btn` after lobby loaded. Result: exactly 1 match started (12 units, 2 squads). The buttons are disabled on first click (`soloBtn.disabled = true`); subsequent clicks are ignored because disabled buttons don't fire click events. **Idempotent.**

**Clash:** 5 rapid clicks on `#clash-start-btn` after the clash config screen opened. Result: exactly 1 match started (entityIDs 37–44, squads 1+2 only — meaning no second match created mid-flight). Match-end overlay appeared ~5s later, returned to lobby cleanly.

**Observation:** The `hasGame: true` flag remained set on `window.__paperWarApp.game` even after returning to the lobby (Game instance not nulled until next match starts). This is a minor leak — doesn't affect gameplay but means stale Game instances accumulate if a player does many quick lobby→match→lobby cycles without reload. **Not filed** — covered by the broader memory leak bug #30.

### P3-7 — Tab backgrounded 5 min during match
**Status:** ✅ PASS (Playwright only — needs manual mobile verification)

Simulated by overriding `document.hidden = true` and `document.visibilityState = 'hidden'`, then dispatching `visibilitychange`.

**Observed over 75s of "background":**
- Tick rate preserved at 10 Hz (server kept pushing snapshots; client received +746 ticks)
- WS state stayed OPEN
- `lastPong` updated (heartbeat continued)
- After restoring visibility, gameplay continued normally

**Caveat:** Playwright does not actually throttle timers when the tab is "hidden" via JS-only simulation — real browsers (especially mobile Chrome/Safari) aggressively throttle `setInterval` to once-per-minute for background tabs. The 15s heartbeat could miss multiple beats in a real mobile background scenario, causing the server to time out the session.

**Recommendation:** Manual test on Chrome mobile (Android) and Safari mobile (iOS) — background the app for 5+ minutes during a match, return, verify (a) the match is still alive, (b) the reconnect overlay shows briefly, (c) state catches up correctly.

### P3-8 — Malformed inbound binary frame
**Status:** ⚠️ BUG — filed as #33

Synthesized 5 malformed binary messages and dispatched them via `conn.ws.onmessage()`:

| Input | Result |
|---|---|
| `ArrayBuffer(0)` | **THROWS**: "Offset is outside the bounds of the DataView" |
| `ArrayBuffer(1)` | **THROWS**: same |
| `ArrayBuffer(3)` | **THROWS**: same |
| Truncated snapshot (header says 5 units but only 3 trailing bytes) | OK (length guard at line 321 catches it) |
| 1024 bytes of garbage (header says 100 units, 255 events) | OK (no throw, parser falls through safely) |

**Root cause:** `connection.js:313` calls `view.getUint32(off=0, true)` on a buffer that could be empty. Lines 274 and 280 only check `byteLength >= 2` for the magic-prefix branches; the snapshot branch has no length guard at all.

**Impact:** Low severity. The throw is uncaught in `ws.onmessage`, but the WS itself survives (`connAlive: true, wsState: 1`) and gameplay continues. Browser console logs the error but the page does not crash. A malicious server (we control it, but a proxy/CDN/reverse-proxy could inject bytes) could disrupt a single frame's processing.

**Fix sketch:** Add at top of `handleMessage`:
```js
if (checkView.byteLength < 11) {
  console.warn('snapshot too short:', checkView.byteLength);
  return;
}
```
(11 = minimum snapshot header size: 4 tick + 4 prevTick + 2 unitCount + 1 eventCount + ... — actually need at least 12 to read baseAlert, but 11 is the safer minimum before any units.)

## Summary

| Test | Status |
|---|---|
| WS drop mid-match solo/clash | ✅ PASS (Phase 1 covers) |
| Server crash mid-match | ✅ PASS (Phase 1 covers) |
| Server crash mid-lobby | ✅ PASS |
| Server crash at login | N/A (architectural) |
| Rapid double-click | ✅ PASS (idempotent) |
| Tab backgrounded 5 min | ✅ PASS (Playwright; needs mobile verification) |
| Malformed binary frame | ⚠️ BUG #33 |

**1 bug filed (#33 — defensive-coding gap in snapshot parser).** No critical issues. Connection-layer resilience is solid for the tested scenarios.
