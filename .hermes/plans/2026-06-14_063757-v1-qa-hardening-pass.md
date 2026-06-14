# Plan: v1 QA & Hardening Pass

**Status**: Draft (auto-generated via `/plan` after `/plan` was invoked with no explicit task)
**Date**: 2026-06-14
**Owner**: agent (autonomous, per user's self-directed work style)
**Rationale**: v1 was declared done, but issue #17 (crash-restart fails) required **three** rounds of root-cause analysis to fix — the last fix uncovered that the Game class silently hijacks connection callbacks. That kind of latent bug suggests there are more. Before truly shipping v1, do a disciplined exploratory + regression QA pass to surface critical defects, then re-declare v1 done with evidence.

---

## Goal

Find and fix all remaining critical bugs in Paper War v1 (crashes, stuck states, desyncs, broken core flows) so v1 can be shipped with confidence. Critical = blocks a normal player from completing a match or starting a new one. Cosmetic/edge issues get filed, not necessarily fixed in this pass.

**Definition of Done for this pass**:
- All critical flows below verified working end-to-end (automated where possible)
- Zero known crash/stuck-state bugs remain open
- Every newly found bug filed as a GitHub issue with the `needs-triage` label
- A summary comment appended to this plan listing what was found and what was fixed

## Current context / assumptions

- **Repo**: `/Users/zhangrong/repo/paper-war` (GitHub: `zhangr011/paper-war`, branch `master`, direct-commit workflow)
- **Server**: Go, run with `cd server && PORT=9091 go run ./cmd/server`. Dev mode = in-memory MockStore (all state wiped on restart).
- **Client**: raw JS, no build step. Served by Go server at `:9091`. Browser global `window.__paperWarApp` (NOT `window.app`).
- **Existing tests**:
  - Server: 51 `*_test.go` files across `pkg/*` — solid unit coverage
  - Client: 3 unit suites (`camera_test.mjs`, `position_test.mjs`, `state_test.mjs`) — 23 tests
  - Playwright e2e: 4 specs (`fog`, `fog-alignment`, `fog-two-player`, `map-generation`) — **narrow, fog/map only**
  - **No e2e coverage for**: matchmaking, solo/clash lifecycle, match-end → new match, reconnect, AI, audio, roster persistence, combat outcomes
- **Issue tracker**: GitHub Issues (`gh` CLI). Triage labels configured: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix` (see `docs/agents/triage-labels.md`).
- **Known-fragile area**: the connection lifecycle. Three consecutive bugs were just fixed here (commits `f63cb12`, `8fff54c`, `5e0d467`). Treat it as suspect.
- **Test conventions**: client tests run with `node client/src/*_test.mjs`. Server: `cd server && go test ./...`. E2E: `npx playwright test` (requires server running on 9091 first; see `tests/e2e/global-setup.js`).

## Proposed approach

Five phases, in priority order. Stop after each phase, file what was found, fix criticals inline, then continue. Phases 3–5 only run if phases 1–2 don't uncover a showstopper that demands full attention.

**Phase 1 — Regression guard (verify recent fixes hold).** Lock in the three crash-restart fixes as an automated test so they can't silently break again. Fast, high value.

**Phase 2 — Critical-path manual exploration.** Walk every player-visible flow end-to-end via the browser, looking for crashes, stuck screens, and broken transitions. This is where the issue-#17-class bugs live.

**Phase 3 — Failure-mode & resilience testing.** Deliberately break things: kill server mid-match, drop WS, corrupt messages, double-click buttons, etc.

**Phase 4 — Gameplay correctness spot-checks.** Verify combat matrix, commander promotion, AI behavior, objectives, roster persistence do what ADRs/CONTEXT say.

**Phase 5 — Scale & performance sanity.** Does a 20v20 clash actually run at acceptable FPS/tick? Are there obvious leaks over a long session?

Each phase writes its findings into `.scratch/qa-pass-2026-06-14/` as evidence (screenshots, console logs, HAR if useful), and files bugs to GitHub with `needs-triage`.

---

## Step-by-step plan

### Phase 1 — Regression guard (verify recent fixes) [~30 min]

**Goal**: Convert the just-fixed crash-restart bugs into a permanent automated guard.

1. **Read the current Playwright setup** to understand the harness:
   - `tests/e2e/global-setup.js`
   - `tests/e2e/fog.spec.js` (template for new specs)
   - `playwright.config.js`

2. **Add `tests/e2e/crash-restart.spec.js`** — a single spec that:
   - Logs in, starts a solo match, waits for first snapshot
   - Tracks the WebSocket via `window.__paperWarApp.connection.ws`
   - Has the test harness `SIGKILL` the Go server (find PID via `lsof -i :9091 -sTCP:LISTEN -t`), wait for client `onDisconnect`
   - Restarts the server (`PORT=9091 go run ./cmd/server &`), waits for WS reconnect
   - Asserts: lobby buttons are re-enabled, login is re-sent, a new solo match can be started
   - Repeats the cycle **3 times** (matches the manual verification we did)
   - Run the same spec against clash mode in a second test case (clash has no reconnect token — exercises the `cleanupGame()` path)

3. **Run it**: `npx playwright test crash-restart` — must pass.

4. **Files likely to change**:
   - `tests/e2e/crash-restart.spec.js` (new)
   - `tests/e2e/global-setup.js` (maybe — if it needs to expose server PID)

5. **Verification**: `npx playwright test` runs all 5 specs green.

### Phase 2 — Critical-path manual exploration [~60 min]

**Goal**: Walk every core flow looking for stuck screens, console errors, and broken transitions.

For each flow below, navigate via `browser_*` tools, capture `browser_console` output (errors/warnings), take a `browser_vision` snapshot at each screen state, and note anything broken.

**Flows to walk** (each starts from a fresh page load at `http://localhost:9091`):

1. **Login flow**
   - Empty username → submit (should reject)
   - Valid username → lands in lobby
   - Network down at login → reconnect → login auto-resent?

2. **Solo match lifecycle** (the core loop)
   - Lobby → click Solo → match starts → first snapshot rendered
   - Move camera (pan/zoom) — no jank, no NaN coordinates
   - Issue a move order — units respond
   - Wait for AI vs player combat → one side eliminated → match-end overlay
   - Click OK on match-end → returns to lobby → **start another solo match** (this is the #17 regression — verify it works)
   - Repeat the full match cycle 3× without reloading the page

3. **Clash match lifecycle** (the mode #17 was actually reported in)
   - Lobby → Clash → 20v20 spawns
   - Verify both teams render with correct colors
   - Play to completion (or accelerate by issuing suicidal orders)
   - Match-end → lobby → start another clash — **3 cycles, no reload**

4. **Mode switching**
   - Solo → match-end → Clash (no reload between)
   - Clash → match-end → Solo (no reload between)
   - This catches callback/state leak bugs like #17

5. **AI-only spectator**
   - Start a solo match, issue no orders, let AI play itself
   - Watch for: stuck units, AI never attacks, AI softlocks, runaway memory in devtools

6. **Roster persistence** (only testable if `DATABASE_URL` is set — if dev mode, document the gap and skip)
   - Win a match → roster updates (XP, kills)
   - Lose a match → permadeath applied to fielded units
   - Reload page → roster persists

**For each broken thing found**: capture evidence to `.scratch/qa-pass-2026-06-14/<flow>-<symptom>.{png,log}`, then file a GitHub issue with label `needs-triage` and the AI-triage disclaimer:
> *This was generated by AI during triage.*

**Files likely to change**: none directly (this phase finds bugs, doesn't fix them). Fixes happen inline only for critical blockers.

### Phase 3 — Failure-mode & resilience testing [~45 min]

**Goal**: Break things on purpose. The crash-restart bugs came from an ungraceful failure path — find the others.

1. **WS drop mid-match (solo)** — kill just the WS (not the server) via `window.__paperWarApp.connection.ws.close()` in `browser_console`. Verify reconnect fires and login re-sent.

2. **WS drop mid-match (clash)** — same, but expect the `cleanupGame()` path (no reconnect token) → return to lobby, not silent hang.

3. **Server crash mid-match (solo)** — Phase 1 covers this; just confirm the spec still passes after any Phase-2 fixes.

4. **Server crash mid-lobby** — between matches, before clicking Start. Does the lobby survive a server restart?

5. **Server crash at login** — during the login round-trip.

6. **Rapid double-click on Start buttons** — does it create duplicate matches or corrupt state?

7. **Tab in background for 5 min during a match** — does the WS time out? Does the snapshot catch up sanely on refocus?

8. **Malformed inbound binary frame** — if feasible, inject a bogus snapshot prefix via a test-only code path and confirm the client doesn't throw an uncaught exception. (May require a test hook; if too invasive, skip and file as a follow-up.)

### Phase 4 — Gameplay correctness spot-checks [~45 min]

**Goal**: Verify the simulation matches its spec. These are quick to check via `browser_console` + targeted Playwright assertions.

1. **Damage matrix** — for each of 4 damage types × 3 armor types, spawn a controlled matchup and verify damage matches `CONTEXT.md`/ADRs. Likely needs a test-only server mode or a dev console command. If no test hook exists, document and file a follow-up issue (low priority for v1).

2. **Commander promotion** — kill a commander in a controlled scenario, verify a CombatUnit promotes and the formation re-centers.

3. **Fog of war** — already covered by existing Playwright specs; just confirm they still pass.

4. **AI behaviors** (per ADR-0011):
   - Explores the map (not stuck in base)
   - Captures strongholds when available
   - Recruits a role-balanced army
   - Defends base when attacked
   - Retreats when outmatched

5. **Objectives** — elimination mode ends when one side is wiped; capture mode ends on point threshold.

6. **Leveling** — units gain levels at the documented kill-point thresholds (2, 4, 8, 16, 32, 64).

### Phase 5 — Scale & performance sanity [~30 min]

**Goal**: Confirm v1 is playable at advertised scale, not just technically running.

1. **20v20 clash FPS** — open devtools Performance monitor, run a clash for 60s, record avg FPS. Red flag: <30 FPS on a modern machine.

2. **Memory over a long session** — start a solo match, let it run 10 min, sample `performance.memory.usedJSHeapSize` every 60s. Red flag: monotonic unbounded growth.

3. **Server tick timing** — `go test -run TestGameLoop ./pkg/game/...` (if it exists) or add a one-shot log of p99 tick duration. Red flag: p99 > 100ms (the tick budget at 10 Hz).

4. **Snapshot size** — log the byte length of inbound snapshots over a clash match. Red flag: > 50 KB/sustained snapshot (would suggest culling isn't working).

---

## Files likely to change (summary)

| Phase | File | Change |
|-------|------|--------|
| 1 | `tests/e2e/crash-restart.spec.js` | NEW — regression test for #17 |
| 1 | `tests/e2e/global-setup.js` | Maybe — expose server PID |
| 2–5 | `client/src/*.js`, `server/pkg/**/*.go` | Only if a found bug is critical and fixed inline |
| 2–5 | `.scratch/qa-pass-2026-06-14/*` | NEW — evidence artifacts |
| 2–5 | GitHub issues | NEW — one per found bug, `needs-triage` label |

## Tests / validation

- **Per fix**: run `cd server && go test ./...` and `node client/src/*_test.mjs` (23 tests must stay green).
- **Per fix**: run `npx playwright test` (grows from 4 → 5 specs after Phase 1).
- **Phase exit gate**: no red tests; no unfixed critical bugs in scope of that phase.
- **Final gate**: re-run the full Phase-2 critical-path walkthrough one more time after all fixes — every flow green from a cold page load.

## Risks, tradeoffs, open questions

- **Risk — scope creep**: exploratory QA can spiral. Mitigation: critical-only fixes inline; everything else gets filed, not fixed, in this pass.
- **Risk — fragile test harness**: the Phase-1 spec kills and restarts the Go server from Playwright; on flaky CI this could be a maintenance burden. Tradeoff accepted — this exact flow is what #17 broke, so it must be covered.
- **Risk — dev mode vs prod divergence**: dev uses MockStore (in-memory); roster persistence (Phase 2.6) can't be truly tested without Postgres. Open question: do we spin up `docker-compose.yml` for that one test, or accept the gap and document it?
- **Risk — browser tool limitations**: `browser_vision` fails on the current provider (zai/glm-5.1 — 429). Fallback: `browser_console` with canvas pixel-sampling IIFEs (proven workflow from past sessions). Pixel-perfect visual checks will be slower.
- **Open question**: should this pass include the **design/audio polish** items from `.scratch/issues/` (e.g. `02-show-combat-unit-hp.md`)? Default: **no** — those are enhancements, not bugs. Keep this pass focused on critical correctness.
- **Open question**: do we ship v1 to a real deployment after this pass, or just re-declare "v1 done" locally? Not in scope of this plan; flag to user at end.

## Out of scope (explicit)

- New features (anything in `.scratch/issues/` or `.scratch/v1-design-overhaul/`)
- Performance optimization beyond sanity checks
- CI/CD setup
- v2 planning
- Anything cosmetic (typo, color tuning, animation easing) — file, don't fix

---

## Execution order

1. Phase 1 (regression spec) — do this first, it's the cheapest insurance
2. Phase 2 (critical-path walk) — most likely to find real bugs
3. If Phase 2 finds ≥1 critical: fix inline, then re-run Phase 2 clean before continuing
4. Phase 3 (failure modes) — second-highest yield
5. Phase 4 (gameplay correctness) — only if Phases 2–3 are clean or low-yield
6. Phase 5 (performance) — final sanity, only if all above green
7. Final summary: append findings + fixes list to this plan file, ping user with the result

---

*This plan was generated in plan mode. No code or project files were modified in its creation (only this plan file was written). Execution requires exiting plan mode.*
