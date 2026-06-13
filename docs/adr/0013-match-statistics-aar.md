# 0013 — Match Statistics / After-Action Report (AAR)

Date: 2026-06-13

## Context

When a match ended, the client showed only a winner banner and win reason. Players
had no visibility into how the match played out — kills, losses, recruitment volume,
gold economy. Without an after-action report (AAR), there was no feedback loop for
players to assess their performance or for the design to surface cumulative economy
data that was already being tracked internally (kill bounties, gold deductions,
recruitment counts).

The game already computed every datum needed for an AAR each tick:
- `DeathSystem.GoldBounties` and `LastAttacker` attribution (per-kill bounty)
- `RecruitmentSystem.GoldDeductions` (per-recruit gold spent)
- `HealthComponent` + `CommanderComponent` (commander-death detection)

But none of this was accumulated across the match or surfaced to the client.

## Decision

Add a server-side `MatchStats` accumulator and a `MsgMatchStats` (0x83) wire message
sent immediately after `MsgMatchResult` at match end. The client renders a
Blue-vs-Red AAR table in the match-result overlay.

### 1. MatchStats Accumulator (`pkg/game/stats.go`)

Per-faction (FactionPlayer=0, FactionEnemy=1) cumulative counters:

| Field | Source |
|-------|--------|
| Kills | `DeathSystem.KillEvents[killerFaction].count` |
| Deaths | `DeathSystem.KillEvents[deadFaction].count` |
| CommanderKills | `KillEvents` with `IsCommander=true` |
| UnitsRecruited | `RecruitmentSystem.SuccessfulRecruits` |
| GoldEarned | Sum of `KillEvents.bounty` |
| GoldSpent | Sum of `RecruitmentSystem.GoldDeductions` |

The accumulator is fed once per tick from `GameSession.Tick()` after the death and
recruitment systems run. `NoKiller (0xFF)` is used for deaths without an attacker
(despawn, suicide, map hazard) — only the death is counted, no kill or gold awarded.

### 2. KillEvent Emission (`pkg/combat/death.go`)

`DeathSystem` now emits a `KillEvent` per death with: `KillerFaction`, `DeadFaction`,
`IsCommander`, `Bounty`. The event is built *before* entity components are removed,
so faction/commander lookups succeed. The events are cleared at the start of each
tick (same lifecycle as `Deaths` and `GoldBounties`).

### 3. SuccessfulRecruits Counter (`pkg/combat/recruit.go`)

`RecruitmentSystem` now tracks `{playerID: count}` per tick alongside the existing
`GoldDeductions`. This avoids double-counting by reading from the same successful
path that already deducts gold.

### 4. Wire Protocol (`MsgMatchStats` 0x83)

Fixed 32-byte payload: two `MatchStatsEntry` structs (16 bytes each), encoding
Kills/Deaths/CommanderKills/UnitsRecruited as `uint16` and GoldEarned/GoldSpent as
`int32`. Sent only at match end, immediately after `MsgMatchResult`.

### 5. Client AAR Overlay (`client/src/main.js`)

`showMatchResult` renders a two-column table (Blue vs Red) with rows for Kills,
Losses, Cmdr Kills, Recruited, Gold Earned, Gold Spent. Handles both message
orderings: if stats arrive before result they're cached; if result arrives first
the overlay is re-rendered when stats land. The winner check was corrected to
`winner === (pid - 1)` because `Lifecycle.WinnerFaction` is a faction index (0/1),
not a playerID (1/2).

## Files Changed

**Server (new):**
- `pkg/game/stats.go` — `MatchStats` + `FactionStats` + `RecordKill`/`RecordRecruit`/`AddRecruits`
- `pkg/game/stats_test.go` — 5 unit tests (kill, commander kill, recruit, accumulate, no-killer)
- `pkg/combat/death_killevent_test.go` — 3 tests for `KillEvent` emission
- `pkg/combat/recruit_count_test.go` — 1 test for `SuccessfulRecruits`

**Server (modified):**
- `pkg/combat/death.go` — `KillEvent` struct + emission in `Tick()`
- `pkg/combat/recruit.go` — `SuccessfulRecruits` map + per-tick increment
- `pkg/game/session.go` — `stats *MatchStats` field, per-tick feeding, `factionOfPlayer()`, `GetMatchStats()`
- `pkg/network/server_msg.go` — `MsgMatchStats` (0x83) + `MatchStatsEntry` encode/decode
- `pkg/network/server_msg_test.go` — round-trip encode/decode test
- `cmd/server/main.go` — broadcast `MsgMatchStats` after `MsgMatchResult` to in-game clients

**Client (modified):**
- `src/connection.js` — parse `MsgMatchStats` (0x83), call `onMatchStats` callback
- `src/main.js` — cache stats, render Blue-vs-Red AAR table in match-result overlay, fix winner faction check

## Consequences

- New wire message type 0x83 (no collision: 0x80 gold, 0x81 result, 0x82 roster)
- 32-byte one-shot payload at match end — negligible bandwidth cost
- `DeathSystem` and `RecruitmentSystem` gain one extra `[]KillEvent` / `map[uint32]uint16`
  field each, cleared per tick (no memory growth across a match)
- Stats are accumulated server-side only; client receives final values, not running totals
- Mid-match stats are available via `GameSession.GetMatchStats()` for future features
  (spectator overlays, live AAR, match-history persistence) but not exposed in v1
- All 10 new tests pass; all 18 packages green
