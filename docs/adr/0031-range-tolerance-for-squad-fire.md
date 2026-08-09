# 0031 — Proximity-Gated Range Tolerance for Squad Fire

Date: 2026-08-07

## Context

Each CombatUnit fires autonomously at targets within its own per-type Range
(5–9 tiles; `CombatUnitStats.Range`). In a mixed Squad the long-range units open
fire while shorter-range units keep marching, so the Squad strings out along the
contact axis — long-range shooters hang back as short-range units surge ahead.
That fragments the Squad at the moment of engagement, fighting the cohesion
model (ADR-0025) and collision (ADR-0030), whose entire point is that a Squad
acts as one body.

## Decision

Add a small **Range Tolerance**: a unit may fire up to `RangeTolerance` (1 tile)
past its nominal Range when a nearby squadmate is already engaging — a "spotter."
In `CombatSystem` target acquisition:

- Before the attack loop, compute the **spotter set**: non-garrisoned units whose
  current target is valid and within their base Range (derived from existing
  target-validity state, one tick of latency).
- A unit B acquires at `Range + RangeTolerance` iff a same-Squad spotter lies
  within `SpotterRadius` (~2 tiles) of B; otherwise it acquires at its base Range.
- B still picks its own target via the normal `findTarget` selection (wounded-focus,
  hysteresis). The spotter only *unlocks* the tolerance; it does not dictate B's
  target.

Garrisoned units are excluded from the spotter system entirely — they neither
grant nor receive tolerance (they fire from the Stronghold's position, not the
field formation). Frozen units (mid-swing) may be spotters: they are engaging and
their position is valid.

## Consequences

- Squads fire more coherently at contact: once the long-range units engage, the
  shorter-range units within `SpotterRadius` join in ~1 tile early instead of
  marching forward alone.
- Proximity-gated, so a unit surged out of formation does NOT get the benefit —
  which is the whole reason (B) was chosen over a flat unconditional tolerance.
  (A flat tolerance was rejected as too permissive; a Squad-membership-only gate
  was rejected as ignoring "nearby.")
- Cost: one extra spatial-hash query per unit per tick (spotter-proximity check)
  on top of the existing target acquisition, plus an O(N) pre-pass to build the
  spotter set. Spotters reuse existing target-validity state with one tick of
  latency — acceptable, and consistent with the 1-tick latency already present in
  the aura spatial query.
- `RangeTolerance` and `SpotterRadius` are tuning constants; retune in one place.
