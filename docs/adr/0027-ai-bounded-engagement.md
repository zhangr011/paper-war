# 0027 — AI Bounded Engagement (close on out-of-range targets, no kiting)

**Status:** Accepted (2026-08-02)

Restores closing behavior the v3 Guard policy (issue #52) removed, with
explicit anti-kite guards so it does not reintroduce the v2 formation-break
that got range-aware engagement reverted.

## Context — the v2 → v3 → v4 whiplash

- **ADR-0017 (v2)** added range-aware engagement: ranged-dominant squads held
  at `CommitRange` (MaxRange), others at `DefaultEngageRange` (5). An
  `Approach` move chased out-of-range enemies to close the distance.
- **Issue #52 (v3, no ADR)** replaced that with a static **Guard** policy:
  when any enemy is detected, the squad holds ground and fires — it never
  repositions. Commit range became unused for movement. Reason: `Approach`
  chased out-of-range enemies, which broke formation and got the squad kited
  across the map.

The v3 Guard fix solved the kite, but introduced a worse exploit: **if the
detected enemy is beyond weapon range, the squad issues no move and no pursue
— it stands still.** A player with longer range (or who simply backs off)
kills the AI with impunity. The AI also can't close to use its melee units.

## Decision — bounded engagement

Reintroduce closing, but bounded so it cannot kite:

1. **Range-check before Guard.** In the combat branch, recover the enemy
   distance/position that v3 Guard discards (`scanEnemiesScored` returns
   `bestDist, bestEnemyX, bestEnemyY`). Compute squad-commander → target
   distance.
2. **Approach if out of range.** If `dist > CommitRange` → `StateApproach`,
   issue `CmdMove` toward a point **at `CommitRange` from the enemy**, not the
   enemy's tile. Stops the squad from overrunning into melee and lets ranged
   squads hover at max range.
3. **Guard once in range.** If `dist <= CommitRange` → `StateGuard` +
   `CmdAttack` (unchanged v3 behavior).
4. **Pursuit break-off.** Each `AIState` carries an engagement anchor
   (`EngageAnchorX/Y`) = the squad position when it first detected the current
   target. If the squad is pulled beyond `MaxPursuitDist` (~8 tiles) from that
   anchor → break off.
5. **Break-off = return + avoid-cooldown.** Drop the target, move back toward
   the anchor, set `AvoidUnitID + AvoidUntilTick` (~60 ticks / 2 eval cycles)
   so the same enemy is skipped as a target while the cooldown lasts. `Guard`
   at the anchor. After the cooldown, normal re-evaluation.

`CommitRange` is unchanged from v2 (`MaxRange` for ranged-dominant squads via
`assessSquad().IsRangedDominant()`, else `DefaultEngageRange`). The
`EvalInterval` (30-tick) throttle still governs non-emergency combat
decisions, smoothing the chase. Emergency retreat (`CriticallyLowHP`) still
bypasses everything.

## Why not the alternatives

- **Static Guard forever (v3 status quo):** exploitable — AI won't close on
  out-of-range targets.
- **Unbounded Approach (v2 status quo):** kites and breaks formation — the
  exact failure v3 was introduced to fix.
- **Kiting / range-maintenance (back off to maintain max range):** valuable
  but harder to tune without re-triggering formation-break; deferred. This
  ADR's anchor+cooldown mechanism is the prerequisite for safely adding it
  later.

## Consequences

- New `AIState` fields: `EngageAnchorX/Y int64`, `EngageEnemyID uint32`,
  `AvoidUnitID uint32`, `AvoidUntilTick uint32`.
- New constant `MaxPursuitDist` (start ~8 tiles, tunable).
- `scanEnemiesScored` callers recover the previously-discarded distance/position.
- Must coordinate with `CombatSystem` auto-pursue: Guard suppresses it via a
  state lookup; confirm `StateApproach` does not cause a double-move (AI
  issues the closing move; CombatSystem should still only resolve in-range
  attacks, not pursue, during Approach).
- Tests: out-of-range enemy → squad advances to CommitRange then Guards;
  kiting enemy pulling squad beyond `MaxPursuitDist` → break-off + anchor
  return + avoid-cooldown; cooldown expiry → re-engagement allowed.

## Out of scope

- Kiting / active range-maintenance (back-off) — deferred; needs the anchor
  mechanism this ADR adds.
- True flanking (distinct approach vectors) — still deferred from ADR-0017.
- Target-selection (`scoreTarget`) changes — unchanged.

See the plan at `docs/plans/ai-bounded-engagement.md`. Resolved in a
grill-with-docs session (2026-08-02).
