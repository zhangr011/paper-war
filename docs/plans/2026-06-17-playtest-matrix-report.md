# Playtest Report — 2026-06-17

**Method:** Headless AI-vs-AI matrix. 20 matches per scenario. Plains clash map, MoveDisabled=true, 10 units per side, level=10.
**Harness:** `server/pkg/game/playtest_matrix_test.go`
**Baseline:** All existing `go test ./...` green (19 packages).

## Correction (2026-06-17 — same day, after systematic debugging)

Initial findings F1, F3 were **false alarms** caused by misdiagnosis. F2 is real but a balance tuning question, not a bug.

| Original finding | Verdict | Evidence |
|---|---|---|
| F1 — Red-side faction bias in asymmetric matchups | **INVALID** | Symmetry check: swapping which side fields the stronger unit produces a perfect mirror. Side with stronger unit wins 20/20 regardless of faction. |
| F3 — Damage matrix counters not functioning | **INVALID** | `combat_test.go` already verifies matrix is applied. Outcomes were correct; my expectations were wrong (close-range squad combat favors high-DPS/high-HP units, not the design-favored unit). |
| F2 — Mirror matches end in mutual annihilation | **REAL but DESIGN** | Symmetric squad combat with simultaneous damage produces low survivor HP by Lanchester linear law. Tuning question, not a bug. |

## What actually happened

My first test had a fatal flaw: it always assigned the **design-favored** unit to Blue and the **counter** unit to Red. At 2-tile spawn distance with MoveDisabled=true:

- **Range advantage is nullified** — both sides are in each other's range from tick 1
- **DPS dominates per-shot damage** — Sniper (20 dmg / 12 cd = 1.67 DPS) loses to LightInfantry (15 dmg / 3 cd = 5.0 DPS) at close range
- **HP pool dominates damage multiplier** — AA's 150% Missile bonus (52 dmg/shot) cannot overcome MotorGun's 3× fire rate (15 dmg/2 cd) before AA's low 60 HP gives out

So "Red wins everything" wasn't a faction bias — Red was just always fielding the actually-stronger close-range unit.

## Symmetry check (the correct test)

| Matchup | Normal (A=Blue,B=Red) | Swapped (A=Red,B=Blue) | Winner |
|---|---|---|---|
| LightInf vs MotorGun | Red 20/20 | Blue 20/20 | Motor wins regardless of faction |
| Sniper vs LightInf | Red 20/20 | Blue 20/20 | LightInf wins regardless of faction |
| AA vs MotorGun | Red 20/20 | Blue 20/20 | Motor wins regardless of faction |
| HeavyInf vs MotorGun | Red 20/20 | Blue 20/20 | Motor wins regardless of faction |

Perfect mirror in every case. Engine is symmetric.

## Why MotorGun dominates at close range

MotorGun: HP=120, Damage=15, Cooldown=2, Range=5, Gun/Heavy. DPS=7.5, HP×DPS=900.
- vs LightInf (HP=100, DPS=5.0, HP×DPS=500) — Motor 1.8× tankier-and-faster
- vs AA (HP=60, vs-Gun-effective-DPS=15×100/6=2.5*100%=2.5, HP×DPS=150) — Motor 6× advantage
- vs HeavyInf (HP=60, Cannon/Light, Gun×Light=100% so DPS=5.0, HP×DPS=300) — Motor 3× advantage
- vs Sniper (HP=30, Sniper/Light, Gun×Light=100% DPS=7.5, HP×DPS=225) — Motor 4× advantage

This isn't a bug. It reveals MotorGun is overtuned for close-range clash OR the test environment (MoveDisabled + tiny spawn gap) is unrealistic.

## Real implications

1. **Engine is correct.** No work needed on combat/damage/faction systems.
2. **Clash test mode doesn't represent real gameplay.** MoveDisabled + 2-tile spawn gap creates conditions where short-range high-DPS units always win. For meaningful balance data, either enable movement or spawn at larger distance.
3. **MotorGun may need tuning** for cost 2 (same as HeavyInf, AA, MotorGun) — it outperforms peers in close combat. Worth a balance pass but separate from this debugging session.

## Files

- `server/pkg/game/playtest_matrix_test.go` — kept; mirrors informational, symmetry check is the canonical bias regression test
- Issues #23, #25 — closed as invalid
- Issue #24 — kept open but reframed as a balance design discussion, not a bug

## Process lessons

- **Run the symmetry test before claiming faction bias.** A single-direction test cannot distinguish "faction bias" from "one unit is just better."
- **Read existing tests before claiming a system doesn't work.** `combat_test.go` already had Gun-vs-Light, Gun-vs-Heavy, Sniper-vs-Light, Missile-vs-Light tests verifying the damage matrix is applied.
- **Math-check your expectations.** Cooldown matters more than per-shot damage. Range only matters if you can keep distance. HP pool scales effectiveness.
