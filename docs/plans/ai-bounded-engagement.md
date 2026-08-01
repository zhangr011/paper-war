# Plan — AI Bounded Engagement (ADR-0027)

Implements ADR-0027. Restores closing on out-of-range enemies (removed by the
v3 Guard policy, issue #52) with anti-kite guards: move to `CommitRange`, a
pursuit break-off anchored at first-detection position, and a per-enemy
avoid-cooldown.

Server-side Go, fully unit-testable. No client change.

## Scope

1. Recover enemy distance/position in the combat branch (currently discarded).
2. Approach when out of range; Guard when in range (today's behavior).
3. Engagement anchor + pursuit break-off.
4. Avoid-cooldown on break-off.
5. Tests + ADR (ADR written).

## Out of scope

- Kiting / active range-maintenance (back-off) — deferred (ADR-0027).
- True flanking — deferred (ADR-0017).
- `scoreTarget` changes — unchanged.
- Client / wire / balance-tuning beyond the intended AI behavior change.

## Edits — `server/pkg/ai/ai.go`

### 1. `AIState` struct — new fields

Read the current `AIState` struct definition first (the per-squad state; `as.States` is `map[uint32]*AIState`). Add:

- `EngageAnchorX, EngageAnchorY int64` — squad position when it first detected the current target.
- `EngageEnemyID uint32` — the target the anchor applies to (reset when target changes).
- `AvoidUnitID uint32`, `AvoidUntilTick uint32` — break-off cooldown target/expiry.

### 2. New constant

`MaxPursuitDist = 8.0` (tiles). Tunable.

### 3. `Update()` combat branch (`ai.go:300-362`)

The current flow: `scanEnemiesScored` returns `(bestDist, bestEnemyX, bestEnemyY, bestEnemyID, _, enemyStrength)` — recover `bestDist, bestEnemyX, bestEnemyY` (currently `_`). Then in the `if bestEnemyID != 0` block, BEFORE the existing Guard+CmdAttack:

- **Avoid-cooldown gate:** if `state.AvoidUnitID == bestEnemyID && tick < state.AvoidUntilTick`, treat this enemy as non-target (skip combat this eval; fall through to strategic behavior). This prevents instant re-engagement after a break-off.
- **Anchor bookkeeping:** if `state.EngageEnemyID != bestEnemyID`, set `EngageEnemyID = bestEnemyID` and `EngageAnchorX/Y = pos.X/pos.Y` (first detection of this target).
- **Force-ratio retreat:** keep the existing force-ratio check (`enemyStrength/squadStrength > ForceRatioRetreat && hpRatio < 0.60`) as-is — it still wins.
- **Pursuit break-off:** compute current distance from the engagement anchor (`pos` vs `EngageAnchorX/Y`). If beyond `MaxPursuitDist`:
  - set `state.AvoidUnitID = bestEnemyID`, `state.AvoidUntilTick = tick + AvoidCooldownTicks` (~60),
  - clear `state.EngageEnemyID = 0`,
  - issue a `CmdMove` back toward the anchor,
  - set `state.State = StateGuard`,
  - `continue`.
- **Close vs Guard:** compute `distSq` to the enemy (`bestDist` or from `bestEnemyX/Y`). `commitRange := assessment.CommitRange()` (read the method; it's MaxRange for ranged-dominant, else `DefaultEngageRange`). If `distSq > commitRange²` → `StateApproach`, issue `CmdMove` toward a point at `commitRange` from the enemy (interpolate: `enemyPos - dir*commitRange`), NOT the enemy tile. Else → today's `StateGuard` + `CmdAttack`.

Use fixed-point consistently (`fixed.FromFloat`, `fixed.FromFloat(8.0)`, etc.) — positions and ranges are `int64` fixed. Compare squared distances to avoid `ISqrt`.

### 4. CombatSystem auto-pursue coordination

Read `server/pkg/combat/combat.go` and confirm how it treats `StateApproach` vs `StateGuard` (the issue-#52 comment says Guard suppresses auto-pursuit via a state lookup). Verify `StateApproach` does NOT cause CombatSystem to also move the squad (double-move). If CombatSystem pursues whenever the AI target is set regardless of state, gate it so pursuit only resumes once the squad is Guard/in-range — the AI owns the closing move. Report what you find; only change combat.go if a double-move would occur.

## Verification — `server/pkg/ai/ai_test.go`

Add tests (mirror the existing v2 AI test style — construct an `AISystem`, seed squads/enemies in pools, call `Update`, assert states/commands):

1. **Out-of-range → Approach.** Enemy placed beyond the squad's CommitRange, no other enemies. Assert `State == StateApproach` and a `CmdMove` toward (but not onto) the enemy is issued.
2. **In-range → Guard.** Enemy within CommitRange. Assert `State == StateGuard` and `CmdAttack` (today's behavior preserved).
3. **Break-off on kite.** Enemy placed such that the squad would be pulled beyond `MaxPursuitDist` from its anchor (simulate by setting the squad's position far from `EngageAnchor` with the target still out of range). Assert `AvoidUnitID` set, `AvoidUntilTick` set, `EngageEnemyID` cleared, `CmdMove` toward anchor, `State == StateGuard`.
4. **Avoid-cooldown respected.** Same enemy re-appears as best target within `AvoidUntilTick`. Assert the squad skips combat (no Approach/Attack) and falls through to strategic behavior.
5. **Cooldown expiry → re-engage.** Tick advanced past `AvoidUntilTick`. Assert normal combat resumes.
6. **Emergency retreat still wins.** Squad at `CriticallyLowHP` with an out-of-range enemy → `StateRetreat`, not Approach.

Run the full existing AI suite (`go test ./pkg/ai/`) — all 29 must stay green; the v2 range-aware tests may need adjustment IF they asserted the v3 Guard behavior, but prefer adding new tests over rewriting old ones.

## Verify before reporting

- `cd server && go build ./...`
- `cd server && go test ./pkg/ai/ ./pkg/combat/ ./pkg/game/`
- `cd server && go vet ./pkg/ai/`

## Report back

Concise: files changed with line ranges, the exact `AIState` fields added, the `MaxPursuitDist`/`AvoidCooldownTicks` values, what you found about CombatSystem auto-pursue (and whether you changed combat.go), and test results (paste PASS/FAIL lines). Flag any case where the design was ambiguous. Do not commit — leave uncommitted on `terrain-polish-loop` (current branch).

## Pointers

- `server/pkg/ai/ai.go:234` — `Update` (combat branch ~300-362).
- `server/pkg/ai/ai.go:489` — `scanEnemiesScored` (return signature; recover dist/pos).
- `server/pkg/ai/ai.go:420` — `assessSquad` / `CommitRange` (v2, reuse).
- `server/pkg/ai/ai.go:30-38` — state constants (`StateApproach=2`, `StateGuard=10`).
- `server/pkg/ai/ai.go:106` — `AICommand` / `CmdMove` / `CmdAttack`.
- `server/pkg/ai/ai_test.go` — existing v2 tests to mirror + keep green.
- `server/pkg/combat/combat.go` — auto-pursue behavior to coordinate with.
- ADR-0017 (v2), issue #52 (v3 Guard), ADR-0027 (this).
