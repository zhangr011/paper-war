# ADR-0020: Map Scale Rebalance

**Date:** 2026-06-30
**Status:** Accepted
**Issue:** [#45 — Map scale and combat-unit pace are mismatched](https://github.com/zhangr011/paper-war/issues/45)
**Supersedes:** implicit tuning in ADR-0001 (10 Hz tick rate, which referenced "500-1000+ units at full scale")

## Context

Issue #45 identified that the map dimensions (48×96), movement speed formula,
vision radii, and starter roster size were each tuned independently and no
longer formed a coherent pacing experience:

- **`defaultCombatUnitSpeed(mapWidth)`** computed speed against the short
  axis (48), but PvP traversal happens on the long axis (spawns at
  `(w/2, 3)` and `(w/2, h-4)` — up to 89 tiles apart on a 96-tall map).
- Effective PvP first-contact: **~9 minutes** (target was 5).
- Starter roster of 6 units occupied **0.13%** of the 4 608-tile map —
  invisible, not "large-scale."
- Commander vision (radius 12) covered only **12.5%** of the long axis —
  enemies were in firing range the moment they appeared.

## Decision

### 1. Shrink the default map from 48×96 to 30×48

```
DefaultMapWidth  = 30  (was 48)
DefaultMapHeight = 48  (was 96)
```

Area reduced by **69%** (1 440 tiles vs 4 608). The starter roster of
6 units now fills **0.42%** of the map (was 0.13%) — a visible formation
rather than scattered dots.

### 2. Fix `defaultCombatUnitSpeed` to use the long axis

```go
// Before: func defaultCombatUnitSpeed(mapWidth int32) int64
// After:  func defaultCombatUnitSpeed(mapWidth, mapHeight int32) int64
//         — internally uses max(width, height) as the traversal axis
```

This was a real bug, not just a tuning preference. The function's contract
says "speed that crosses the map in `combatUnitCrossMapSeconds`" but it
was using the wrong dimension. PvP spawns are on the long axis; computing
against the short axis produced a speed 40% too slow for the actual
traversal distance.

### 3. Reduce `combatUnitCrossMapSeconds` from 300 to 240

```
combatUnitCrossMapSeconds = 240  (was 300)
```

Combined with the long-axis fix, units now move at **0.20 tiles/sec**
(was 0.16). Cross-map traversal on the 48-tile long axis takes **240 s**
(4 min). PvP first-contact takes **~102 s** (1.7 min) — well under the
120 s ceiling.

### 4. Keep vision radii and starter roster unchanged

- `fog.VisionRadiusTiles = 12` (commander) — now covers **25%** of the
  long axis (was 12.5%). Scouting matters.
- `fog.UnitVisionRadiusTiles = 6` (combat unit) — covers **12.5%**.
- `InitialTeamCombatUnits = 5` — unchanged. On the smaller map, 6 units
  in formation look like a squad, not a scouting party.

## Pacing invariants (verified by `pkg/game/scale_test.go`)

| Invariant | Target | Actual (30×48) | Was (48×96) |
|-----------|--------|----------------|-------------|
| Cross-map traversal (long axis) | ≤ 240 s | 239.8 s | ~298 s (width) / ~596 s (height) |
| PvP first-contact | ≤ 120 s | 102.4 s | ~472 s |
| Commander vision coverage | ≥ 25% | 25.0% | 12.5% |
| Starter roster fill | ≥ 0.4% | 0.42% | 0.13% |

## Consequences

- **Pro**: Multiplayer playtest (`tests/e2e/multiplayer-playtest.spec.js`)
  now concludes within 2 minutes (was timing out at 4 min). Match
  completion verified end-to-end: queue → match_found → combat → AAR.
- **Pro**: All four pacing invariants locked down by `scale_test.go`.
  Future constant changes will fail loudly if they break the targets.
- **Pro**: The `defaultCombatUnitSpeed` axis bug is fixed — any future
  map orientation change (portrait ↔ landscape) automatically computes
  the correct speed.
- **Con**: Clash mode maps (`pkg/tilemap/clash_maps.go`) remain at
  48×96 — they're separate sandbox presets, not derived from the
  default constants. If clash mode should also shrink, that's a separate
  change.
- **Con**: Existing tests with hardcoded spawn coordinates (y=85, y=86,
  y=93) had to be updated to use `DefaultMapWidth`/`DefaultMapHeight`
  expressions. Future tests should derive coordinates from constants,
  not hardcode literals.
- **Con**: `TestClashModeBalance` is slightly more flaky — the faster
  units alter the RNG-dependent clash simulation's win distribution.
  Acceptable: the test checks for approximate balance (40-60 split),
  not exact numbers.

## Verification

- `pkg/game/scale_test.go` — two new tests:
  `TestMapScalePacingTargets` (4 invariants, pure-constant calculation)
  `TestDefaultCombatUnitSpeedUsesLongAxis` (axis-aware formula regression)
- `pkg/game/session_test.go` — updated
  `TestDefaultCombatUnitSpeedCrossesLongAxisInConfiguredTime` (was
  `TestDefaultCombatUnitSpeedUsesFiveTimesMovement`) to assert against
  the long axis.
- `tests/e2e/multiplayer-playtest.spec.js` — the v1.0 polish-pass
  playtest (previously in `.scratch/`, timing out at 4 min) now passes
  in 1.8 min. Promoted to the permanent e2e suite.
- Full Go test suite: 18/18 packages pass.
- Playwright e2e: 17/17 existing + 1 new = 18/18 pass.
