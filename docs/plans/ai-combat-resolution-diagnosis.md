# Plan — Diagnose AI Combat-Resolution Failure

The solo-match stalemate is down to 25/30. The dominant residual, pinned by
the hunt/seek Step-0 instrumentation (PR #62): **combat-resolution failure**.
P2 squads are in `StateApproach` / `StateRetreat` — they *see* the survivors —
but can't close the kill. Example (seed 1, tick 5000): P2 has 9 units, P1 has
3, neither side finishes the other for thousands of ticks.

This is a **diagnosis** plan, not a fix plan. Goal: determine which of the two
prime suspects is the actual cause, with evidence, so the subsequent fix
targets the real behavior. Do NOT ship a speculative fix — instrument, find
the mechanism, report.

## Step 0 — Reproduce on a pinned seed

Throwaway test on the deterministic setup (`NewGameSession` → map seed 42;
`gs.AISys.SetRNG(rand.New(rand.NewSource(1)))`; `gs.SetSessionRNG(...)`) run to
tick 5000. Confirms the residual reproduces. Delete after.

## Step 1 — Instrument the combat-resolution period

Throwaway test that runs seed 1 to the freeze and, for the late game (e.g.,
ticks 2500–3500, sampled every 30 ticks = each eval cycle), logs per P2 squad:
- `State`, `TargetUnitID`, commander position, squad HP ratio.
- Distance to the target (the survivor).
- Whether the squad fired this interval (CombatSystem: did any squad member
  have `TargetID` set and land a hit? Or simpler: did the target's HP change?).

And for the P1 survivor (the target entity): HP over time (is it taking any
damage at all? Is it full HP / healing?).

This distinguishes the suspects:

### Suspect A — Approach↔Retreat oscillation
The squad flips between `StateApproach` and `StateRetreat` every eval cycle
(force-ratio or emergency-retreat trigger) — it advances a little, retreats a
little, net-zero progress. **Diagnostic signature:** `State` alternates
Approach/Retreat across consecutive samples; target HP roughly constant.

### Suspect B — CommitRange hover (no-fire boundary)
The squad sits in `StateApproach` (or Guard) at ~commit range, never closing
to actually fire. **Diagnostic signature:** `State` stable (Approach/Guard),
distance stable at ~commit range, target HP constant, **no hits landed**.

### Suspect C — fires but can't DPS (tanky survivor / low damage)
The squad IS firing (target HP slowly declining) but can't finish — e.g., the
survivor is a 6×-HP commander and the attackers' DPS is too low, or the
survivor keeps recovering via retreat+time. **Diagnostic signature:** target
HP slowly trends down (or oscillates without hitting 0), hits ARE landing.

## Step 2 — Pinpoint the trigger

Whichever suspect the data points to, pinpoint the exact code path:
- For A: which retreat trigger fires (CriticallyLowHP < 0.10 emergency, or
  ForceRatioRetreat > 1.5 with hpRatio < 0.60)? Why does it flip BACK to
  Approach next cycle? (Likely: retreat raises HP ratio via... no healing
  exists, so it must be the force-ratio recomputing, or the break-off
  avoid-cooldown expiring.)
- For B: is `distSq > commitRange²` every cycle (never closes)? Why — is the
  target moving away at the same speed (kiting), or is the Approach move
  target computed wrong, or does the formation lag put the nearest unit just
  outside `distSq <= rangeSq`? Check the actual `commitRange` value for the
  squad composition and the weapon range.
- For C: what's the survivor's HP/MaxHP and the attackers' per-tick DPS?
  Is promotion/level scaling making the survivor too tanky?

## Deliverable

A **diagnosis report** (not code): which suspect (A/B/C), the evidence
(state trace + distance + target-HP samples from the instrumentation), the
exact trigger code path, and a concrete fix recommendation. If the fix is
small and unambiguous (e.g., a single clearly-wrong constant or condition),
note it but do NOT implement — a separate plan will implement against the
diagnosis. If the instrumentation reveals a DIFFERENT root cause than A/B/C,
report that instead.

## Verification

This is instrumentation only — no production changes. After the throwaway
tests are deleted:
- `cd server && go build ./... && go test ./pkg/ai/ ./pkg/game/` — clean/green
  (no leftover test files, no behavior change).

## Report back

The diagnosis: which of A/B/C (or other), with the sampled evidence (paste a
few representative log lines — squad state transitions + distance + target HP
over the freeze), the pinpointed trigger (code path + line), and the
recommended fix. Note the seed and setup so it reproduces.

## Pointers

- `server/pkg/ai/ai.go:377-477` — combat branch (Approach / Guard /
  force-ratio retreat / emergency retreat / break-off).
- `server/pkg/ai/ai.go:44-47` — retreat thresholds (`CriticallyLowHP`,
  `RetreatHPThreshold`, `ForceRatioRetreat`).
- `server/pkg/ai/ai.go:147` — `CommitRange()`; `server/pkg/combat/combat.go:147`
  — `distSq <= rangeSq` fire check (inclusive boundary).
- `server/pkg/component/unit_type.go` — LightInfantry stats (Range=5, Dmg=15,
  HP=100) and commander HP scaling (6× base, `session.go:888/983`).
- `server/pkg/game/solo_match_integration_test.go` — deterministic setup to copy.
