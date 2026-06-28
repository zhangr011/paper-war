# ADR-0019: Leaderboard

**Date:** 2026-06-28
**Status:** Accepted
**Builds on:** ADR-0018 (Player Identity & Career Stats)

## Context

ADR-0018 added per-player career stats accumulation. The natural next
question players ask is "how do I compare to others?" Without a
leaderboard, the career screen shows isolated numbers with no benchmark.

The v1.x match-end pipeline already writes `player_career` rows. A
leaderboard is just a sorted, limited query against that table — the
cheapest possible "meta" feature.

## Decision

### 1. Pull-based: client requests, server responds

The client sends `get_leaderboard` text JSON; the server replies with a
`leaderboard` text JSON message containing an `entries` array. Rationale:

- Push-based (server broadcasts on every match end) would spam every
  lobby client with N entries every match. Most clients don't have the
  leaderboard open.
- Pull-based lets the client refresh on demand (button click, screen
  open) and is naturally throttled by user intent.
- Same pattern as `join_queue` / `get_roster` — text JSON for
  non-tick-critical messages.

### 2. Ranking metric: total kills, descending

Simple and correlates with activity + combat engagement. Tie-breaks:

1. `matches_won` desc (winning is better than losing at equal kills)
2. `player_id` asc (deterministic — same input → same output)

Future versions may switch to a derived score (`3*wins + kills`) or
win-rate with a minimum-games threshold. The wire format is stable
enough to add a `metric` field later without breaking clients; the
current v1.x doesn't need to.

### 3. Players with zero matches excluded

A new account has a `player_career` row (created lazily on first
`AddCareerStats`), but `matches_played = 0`. Including them in the
leaderboard would flood the empty state with "unnamed player — 0
kills". Filter at the SQL layer (`WHERE matches_played > 0`).

### 4. Limit clamped to [1, 100]

`LeaderboardLimit = 10` default. Client may request a custom limit
(`get_leaderboard { limit: 50 }`) — clamped server-side to prevent a
single client from requesting the entire player table. The clamp helper
returns the default (10) for limit ≤ 0.

### 5. Player names: added to `players` table

v1.1 stored tokens but not names — names were only in the in-memory
`Hub.ClientSession.Name`. For the leaderboard to show names, they must
be persisted. Added `name TEXT NOT NULL DEFAULT ''` to the `players`
schema, with an idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
migration for existing databases.

**Last-login-wins**: `FindOrCreatePlayer(token, name)` updates the
stored name on every login. Lets players rename themselves by simply
typing a new name in the login box. No separate rename API needed.

### 6. DB-side sort + JOIN

```sql
SELECT pc.player_id, p.name, pc.matches_played, ...
FROM player_career pc
JOIN players p ON p.id = pc.player_id
WHERE pc.matches_played > 0
ORDER BY pc.total_kills DESC, pc.matches_won DESC, pc.player_id ASC
LIMIT $1
```

Single query, single round-trip. Backed by an index on
`player_career(total_kills DESC)` (added in `ensureSchema()`) so the
sort is index-served as the table grows.

The MockStore mirrors this with an in-memory `sort.Slice` — fine for
tests with single-digit player counts.

## Consequences

- **Pro**: Smallest possible "meta" feature — ~250 lines including
  tests. The career-stats foundation pays off immediately.
- **Pro**: Names now persist across server restarts (previously
  ephemeral). Side benefit for roster display and future features.
- **Pro**: Pull-based means leaderboard cost is paid only when someone
  actually looks at it.
- **Con**: Adding `name` column is a schema change. The idempotent
  `ALTER TABLE` makes it safe but a v1.0→v1.2 upgrade does require a
  DB write on connect.
- **Con**: Total-kills metric favors active players over skilled ones.
  Acceptable for v1.x — the player base is small and activity is the
  primary signal. Future ADR can switch to win-rate or derived score.
- **Con**: No rate limiting on `get_leaderboard`. A malicious client
  could spam requests. Internal limit clamp bounds per-request cost to
  100 rows; a future ADR can add per-client throttling.

## Verification

- `pkg/persist/leaderboard_test.go` — 6 tests: empty store, zero-match
  exclusion, sort order, kills tie-break by wins, limit clamping,
  last-login-wins name update.
- `tests/e2e/_smoke-leaderboard.spec.js` (in `.scratch/`) — manual smoke
  test confirms login → click Leaderboard → server responds with empty
  entries → empty-state UI renders.

## Out of scope (v1.3+)

- **Seasons** — periodic resets with rewards. Needs a season boundary
  column + scheduler.
- **Per-commander leaderboard** — track W/L per commander type.
- **Friends list / social graph** — filter leaderboard to "people I know".
- **Win-rate ranking** — needs a minimum-games threshold (e.g., 10
  matches) to be statistically meaningful.
- **Pagination** — currently capped at 100. If the player base grows
  past that, add `offset` parameter.
