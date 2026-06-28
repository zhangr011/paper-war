# ADR-0018: Player Identity & Career Stats

**Date:** 2026-06-28
**Status:** Accepted
**Supersedes:** extends ADR-0008 (Postgres Roster Persistence)

## Context

v1.0 shipped roster persistence (ADR-0008), per-match `MatchStats`, and an
AAR overlay. But these were disconnected in production:

- The `Player.Token` field existed in the schema but was never used. Login
  was name-only — every client shared `playerID=1` in solo/clash, so the
  roster was effectively shared across all connections.
- `MatchStats` was broadcast at match end and immediately discarded —
  nothing was written back to a player account.
- The client's roster UI was hardcoded; `roster_update` messages were
  handled but never arrived.

Meta / campaign (seasons, leaderboards, commander history) is impossible
until **player identity** + **career aggregation** work. This ADR covers
the foundation: how players are identified, and how match stats accumulate
across matches.

## Decision

### 1. Token-based identity (no real auth)

The client generates a 16-byte opaque hex token on first login
(`crypto.getRandomValues`) and persists it in `localStorage` under
`paper-war:player-token`. Every `login` message includes both `name` and
`token`. The server resolves the token to a DB `player_id` via
`Store.FindOrCreatePlayer`, which is idempotent on the token column.

**Threat model**: anyone with the token can act as that player. There is
no real authentication, no password, no rate limiting. This is acceptable
for v1.x because:
- The game is private (friends-only) — not published.
- Tokens never leave the browser that generated them (no central leak).
- Loss of a token = starting over with a fresh roster; no
  payment/identity tied to accounts.

For v2 (public release), this will be replaced with proper OAuth/email
auth. The token abstraction is preserved — only the verification step
changes.

### 2. Separate `player_career` table

```sql
CREATE TABLE player_career (
  player_id          INT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
  matches_played     INT NOT NULL DEFAULT 0,
  matches_won        INT NOT NULL DEFAULT 0,
  ...
  last_played_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

A separate table from `commanders` because:
- **Roster is mutable** (commanders gain/lose units every match).
- **Career is append-mostly** (totals only go up — W/L counters are
  monotonic).
- Different access patterns: roster is loaded on login and at match
  start, career is updated once at match end.
- Keeps the schema readable — JSONB blob for roster, typed columns for
  totals.

### 3. Atomic accumulation via `ON CONFLICT DO UPDATE`

```sql
INSERT INTO player_career (player_id, matches_played, ...)
VALUES ($1, $2, ...)
ON CONFLICT (player_id) DO UPDATE SET
  matches_played = player_career.matches_played + EXCLUDED.matches_played,
  ...
```

Single statement, atomic, idempotent schema. Works for both first-match
and subsequent matches without a separate `INSERT IF NOT EXISTS`.

### 4. Faction resolution at match end

A player's career delta is built from `MatchStats.Factions[faction]`,
where faction is resolved via `FactionOfPlayer(matchPlayerID)`:

- Solo mode: human player = `matchPlayerID 1` = `FactionPlayer (0)`.
  AI = `matchPlayerID 2` = `FactionEnemy (1)`.
- PvP: queue-ordered — first player joined gets `matchPlayerID 1`
  (blue), second gets `matchPlayerID 2` (red).
- Clash spectator: `matchPlayerID 0` → skipped (no career impact).

This mapping is exact because `SetClientPlayerID` is only called once per
match (at match-found / start-solo / start-clash) and not overwritten
until the next match begins. The `PhaseEnded` block can safely read it.

### 5. Career stats delivery

The server pushes `career_stats` JSON text messages at two points:
1. **Right after `login_ok`** — initial state (may be all zeros for new
   players). Lets the client render the lobby summary line immediately.
2. **Right after match-end AAR** — updated totals. Refreshes the UI
   without requiring a re-login.

JSON text (not binary wire message) for consistency with `roster_update`,
`match_found`, etc. — non-tick-critical messages use JSON.

### 6. Hub token tracking

Added `Hub.SetClientToken` / `GetClientToken` and `ClientSession.Token`
field. The token is preserved across the match (not overwritten by
`SetClientPlayerID`), so the match-end career-stats writer can always
look up the DB `player_id` via `Store.FindOrCreatePlayer(token)`.

## Consequences

- **Pro**: Roster persistence works in production — each browser gets its
  own roster, surviving server restarts.
- **Pro**: Career stats enable future leaderboards, seasons, and commander
  history without further server-side changes.
- **Pro**: Foundation for meta-progression is in place; v1.2 can add
  leaderboard queries + UI on top.
- **Con**: Token theft = account takeover. Acceptable for v1.x private
  game; must fix before public release.
- **Con**: `player_career` is another table to migrate. Idempotent
  `CREATE TABLE IF NOT EXISTS` in `ensureSchema()` handles this.
- **Con**: MockStore and PostgresStore must both implement the new
  `GetCareerStats` / `AddCareerStats` methods. Stub
  (`postgres_nopgx.go`) returns errors so default builds can't
  accidentally use it.

## Verification

- `pkg/persist/career_test.go` — 4 tests: zero-stats for new player,
  accumulation across 3 adds, returns-copy invariant, unique IDs for
  distinct tokens.
- `pkg/game/career_integration_test.go` — drives close-quarters 1v1 to
  completion (0.10s), verifies winner gets MatchesWon=1, loser gets
  MatchesLost=1 + CommandersLost=1.
- `tests/e2e/_smoke-career.spec.js` (in `.scratch/`) — manual smoke test
  confirms login → career_stats arrives → Career screen renders.

## Out of scope (v1.2+)

- Leaderboards — aggregation query + UI, separate screen.
- Seasons — periodic resets, requires scheduler.
- Per-commander match history — `commander_matches` table.
- Real authentication (OAuth/email).
