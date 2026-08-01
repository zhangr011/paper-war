# Plan — AI Hunt/Seek (find enemies after losing contact)

Connectivity is fixed (PR #61 + the connectivity-guarantee branch), but the
solo-match stalemate rate only dropped from 28/30 to 25/30. The residual cause
is an **AI hunt/seek gap**: when no enemy is currently in vision, the AI moves
only to *predictable* locations — enemy base (push), strongholds, or random
patrol — never to where enemies actually are. Enemy survivors that slipped out
of vision aren't at their base, so contact never resumes and elimination never
completes.

Concrete (seed 1, tick 5000): P2 and P1 clashed earlier (P1 went 12→3), but
once P1's survivors slipped out of vision (~21 tiles away at y≈25), P2 reverted
to base-push and never went looking — 9 AI units sit at y≈46, 3 player units at
y≈25, neither converges.

## Step 0 — Instrument first (insurance)

Before implementing, confirm P2's AI state in the seed-1 residual so the fix
targets the real behavior (is it "pushing to the wrong place" or "not pushing
at all"?). Throwaway test on the deterministic setup (`gs.AISys.SetRNG(seed 1)`,
`SetSessionRNG(seed 1)`, map seed 42 via `NewGameSession`): run to tick 5000,
log each P2 AI squad's `State`, `TargetUnitID`, and the squad commander
position. Delete the throwaway after. If P2 is *not* in StatePush (e.g., stuck
in Defend/Retreat/Idle), note it — the fix may need to also un-stick that.

## Goal

When the AI has lost contact with enemies, actively **seek** them: hunt the
most recent last-known position (LKP) if one exists, else sweep the enemy
half of the map. Eliminate the "lost contact → never re-engage" stalemate class.

## Scope

1. LKP memory on `AISystem` (position + tick of the most recent enemy sighting).
2. Update the LKP whenever enemies are seen (reuse `bestEnemyX/bestEnemyY`
   the combat branch already recovers from `scanEnemiesScored`).
3. A `huntCommand` in the no-enemy-visible branch, ordered before push.
4. A sweep fallback in `offensivePushCommand` (or patrol) toward the enemy
   half when no recent LKP.
5. Tests + the seed-sweep stalemate rate as the headline metric.

## Out of scope

- A full systematic map-sweep (spiral/sector search) — defer unless LKP+sweep
  still leaves stalemates.
- Multi-squad coordinated search (flanking/pincer) — one squad hunts the LKP
  is enough for v1; others push.
- Changes to vision/fog or the flow field (connectivity is already guaranteed).
- AI behavior when enemies ARE visible (Approach/Guard/break-off unchanged).

## Design

### 1. LKP memory

Add to `AISystem` (`server/pkg/ai/ai.go`):

```go
// lastEnemySighting is the most recent position an enemy was seen, with the
// tick it was seen. Drives hunt/seek when no enemy is currently visible.
lastEnemySightingX, lastEnemySightingY int64
lastEnemySightingTick                 uint32
```

Single AI-wide LKP (intel is shared across the AI's squads). Updated in the
combat branch right after `scanEnemiesScored` returns a target — reuse
`bestEnemyX, bestEnemyY` (already recovered) and the current `tick`.

### 2. Hunt command

`func (as *AISystem) huntCommand(squadID uint32, state *AIState, pos component.PositionComponent, tick uint32) *AICommand`:
- Returns nil if no LKP, or if `tick - lastEnemySightingTick > HuntMemoryTicks` (stale — enemy likely long gone).
- Otherwise issues `CmdMove` to the LKP, sets `state.State = StateScout` (reuse the scout state — it's "searching for enemies"). On arrival the squad re-scans from closer range and either re-detects (→ combat branch, LKP refreshes) or finds nothing (LKP eventually goes stale → falls through to push/sweep).

### 3. Wire into the no-enemy branch (`ai.go:479+`)

Order becomes: `explore (early) → hunt (recent LKP) → push (elimination) → stronghold → patrol`.
Hunt takes priority over push because the LKP is *where enemies actually
are*, whereas push goes to the enemy base (where they may not be).

### 4. Sweep fallback

When hunt returns nil (no recent LKP), the push/patrol should bias toward the
enemy half of the map rather than a single base point or a fully random tile.
Minimal change: if `offensivePushCommand` returns nil (already "at" enemy base
or no enemy base), and there's no LKP, send the squad to a sweep point on the
enemy half — e.g., a deterministic rotating sector point biased toward enemy
territory and currently-fogged tiles (reuse the `exploreCommand` fog check).
Keep this lightweight; the LKP hunt is the primary mechanism.

### 5. Constants

`HuntMemoryTicks uint32 = 300` (~30s at 10Hz — an enemy seen within the last
30s is worth hunting; older is stale). Tunable.

## Interaction with ADR-0027 (bounded engagement)

If the hunted enemy kites and the squad breaks off (AvoidCooldownTicks=60),
the LKP updates on the next sighting, so the squad re-hunts the updated
position and converges rather than oscillating. Add a regression test for:
see enemy → lose it → hunt LKP → re-acquire.

## Verification — `server/pkg/ai/ai_test.go`

- `TestAI_HuntsLastKnownPosition`: spawn an enemy in AI vision for a few
  ticks (LKP recorded), then move it out of vision. Assert the AI issues a
  CmdMove toward the LKP within `EvalInterval` after losing sight.
- `TestAI_LKPStaleFallsThrough`: LKP older than `HuntMemoryTicks` → hunt
  returns nil → falls through to push/patrol.
- `TestAI_LKPRefreshesOnReSight`: after hunting to the LKP area, if the enemy
  is re-seen the LKP updates to the new position.
- (Regression) the existing ADR-0027 break-off tests stay green.

## Headline metric — seed sweep

`TestSoloMatchRunsToCompletion`-style sweep over seeds 1–30 (AI RNG seeds on
the seed-42 map, matching the connectivity branch's methodology). Before this
fix: 5/30 end. **Target: most of 30 end.** Report the before/after. If a
residual seed still stalemates, capture seed + the no-enemy AI state + unit
positions for the next investigation (don't mask; don't weaken tests).

## Verify before reporting

- `cd server && go build ./...`
- `cd server && go test ./pkg/ai/ -v` — new tests pass, existing suite green.
- `cd server && go test ./pkg/game/ -run TestSoloMatchRunsToCompletion` green.
- `cd server && go test ./...` (note any pre-existing `TestStatsResetBetweenMatches` flake — unrelated.)
- `cd server && go vet ./pkg/ai/ ./pkg/game/`

## Report back

Concise: Step-0 finding (P2's residual AI state — was it pushing?), files
changed, the LKP wiring + hunt order, the **before/after stalemate rate over
seeds 1–30**, and whether `TestSoloMatchRunsToCompletion` can be un-crutched.
Branch `ai-hunt-seek` off master; one commit; don't push.

## Pointers

- `server/pkg/ai/ai.go:479+` — no-enemy-visible strategic branch (insertion point).
- `server/pkg/ai/ai.go:304,311` — `bestEnemyX/bestEnemyY` already recovered from `scanEnemiesScored` (reuse for LKP).
- `server/pkg/ai/ai.go:823` (`exploreCommand`), `offensivePushCommand`, `strongholdCommand` — patterns to mirror for `huntCommand`.
- `server/pkg/ai/ai.go:92` — `AIState` struct; `AISystem` struct ~155.
- `server/pkg/game/solo_match_integration_test.go` — stalemate test + sweep methodology.
- ADR-0017 (intel/retreat), ADR-0027 (bounded engagement break-off) — interactions.
