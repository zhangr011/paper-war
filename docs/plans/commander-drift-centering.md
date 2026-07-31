# Plan — Drift-Based Commander Centering (ADR-0025)

Implements ADR-0025. Keeps the Commander at the Squad centroid by suppressing
CombatUnit flow when the Commander drifts beyond 0.5 tile, releasing at 0.2.

## Edits

### 1. `server/pkg/component/commander.go` — add flag

Add `Suppressing bool` to `CommanderComponent`. Per-Squad, computed fresh each
tick (stateless across ticks — safe across Commander death/promotion).

### 2. `server/pkg/commander/commander.go` — compute drift, set flag

In `CommanderSystem.Tick`, before the morale-aura loop:

1. Single O(N) pass over `boidPool` accumulating per-squad
   `sumX, sumY, count` for alive, non-`RoleCommander` members (skip
   `GarrisonedIn != 0`). Build `map[uint32]centroid`.
2. In the existing `cmdPool.Each` loop (after the death check, before/around the
   aura pass), for each alive Commander:
   - `count := centroidCount[squadID]`. If `count < 2` → `cmd.Suppressing = false`
     (guard: empty/lone Squad — can't be "behind" a team that isn't there).
   - Else compute `distSq(commanderPos, centroid)`.
   - Hysteresis: set `Suppressing = true` if `distSq > FromFloat(0.5)²`;
     set `false` if `distSq < FromFloat(0.2)²`; otherwise leave unchanged.
   - Compare `distSq` against squared thresholds (no `ISqrt`).
3. Do **not** reuse the `AuraRadius` spatial query — it reaches only 3 tiles and
   misses surged units.

`CommanderSystem` already has `posPool`, `cmdPool`, `boidPool` and runs at
Priority 50 (before Movement 60). No new pools needed beyond `boidPool` (already
held) for the centroid pass.

### 3. `server/pkg/movement/movement.go` — consume flag

1. Where `commanderPos` is built (`movement.go:62-72`), also build
   `suppressing map[uint32]bool` from `cmd.Suppressing`.
2. At the flow-field application (`movement.go:132-133`), for non-Commander units
   only, zero the force when `suppressing[bc.SquadID]`:
   ```go
   if bc.Role != component.RoleCommander && suppressing[bc.SquadID] {
       flowFX, flowFY = 0, 0
   }
   ```
   Commander keeps its flow force. Separation and attraction-to-Commander are
   untouched, so units collapse back toward the Commander rather than piling on
   it.

## Guards (required)

- `count < 2` alive CombatUnits → never suppress (centroid undefined / noisy).
- Gate on `cmd.IsAlive` (already the loop's domain; promoted Commander inherits).
- Distance-squared compares only; no per-tick `ISqrt`.

## Verification

- Unit test (`commander_test.go`): Commander placed >0.5 tile from a clustered
  Squad → `Suppressing` goes true; moved back within 0.2 → goes false; lone-Squad
  case stays false.
- Movement test: a suppressing Squad's non-Commander units get zero flow force;
  Commander's flow force unchanged.
- Playtest harness (`server/pkg/game/playtest_matrix_test.go`, see memory
  *MotorGun balance baseline*): run the realistic movement harness and check the
  core claim — Squads whose Commander would have trailed now stay centered, and
  the "rear-Commander → lose" outcome rate drops. Top/bottom spawn is randomized
  per run to cancel harness directional bias (memory *Playtest harness
  directional bias*).
- Crash-restart spec (`tests/e2e/zz-crash-restart.spec.js`) is unaffected — it
  asserts reconnect/lobby behavior, not positioning — but it remains the
  observation instrument (20v20 clash matches) for a manual eyeball check.

## Tuning knobs

`D_high = 0.5`, `D_low = 0.2` (tiles). Re-check the non-oscillation invariant
(per-tick motion 0.02 tile << 0.3 band) if `combatUnitCrossMapSeconds` or map
long axis changes.

## Out of scope

- Wiring up real formation offsets / `CmdChangeFormation` (still dead; separate
  effort).
- Adding any new force on the Commander (rejected option (a)).
- Touching the combat system / `StateGuard` / attack-freeze (rejected option (c)).
- Network/snapshot changes — `Suppressing` is server-internal, not replicated.
