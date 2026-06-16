# 0017 — AI v2: Strategic + Tactical Improvements

Date: 2026-06-16

## Context

ADR-0011 added the first strategic layer (exploration, strongholds, adaptive
recruitment, base defense). Post-v1 review of `pkg/ai/ai.go` identified nine
weaknesses that made the AI tactically exploitable and strategically inert:

1. **Hardcoded 5.0 engagement range** — Snipers (range 8) and Missile units
   (range 9) would walk into melee before firing, wasting their range advantage.
2. **No target prioritization** — AI attacked the nearest enemy regardless of
   value, so it would chase a cheap LightInfantry decoy instead of the enemy
   commander at 1.5x the distance.
3. **Passive elimination behavior** — in elimination mode the AI patrolled
   randomly and never advanced toward the enemy base; the human player could
   simply wait.
4. **Fight-to-the-death** — no force-ratio awareness; the AI would suicide a
   lone damaged commander into five enemies instead of retreating.
5. **No squad composition awareness** — every squad was treated identically.
   A ranged-heavy squad was not held at range; a melee-heavy squad was not
   directed to close.
6. **Trickle recruitment** — AI spent gold the moment it had 15, producing a
   dribble of single units that died piecemeal. No tempo.
7. **Intel reset every tick** — `EnemyUnits` was wiped at the start of each
   Update(), so adaptive recruitment ratios were based only on enemies visible
   *right now*. Short sightlines meant no memory.
8. **Binary base defense** — a single enemy near the base recalled every idle
   squad, even when harmless.
9. **No regroup behavior** — a retreating squad kept retreating forever even
   after reaching safety.

Issue #3 (Better AI) requested strategic decision-making, terrain-aware
movement, objective-focused behavior, adaptive unit recruitment, and unit
grouping/flanking.

## Decision

Rewrite the AI decision loop around a new `SquadAssessment` structure that
gives every per-squad decision access to the squad's actual composition,
range, HP, and strength. Layer seven concrete improvements on top.

### 1. SquadAssessment (composition awareness)

`assessSquad()` iterates the squad's boid members once per evaluation cycle
and computes: `UnitCount`, `MaxRange`, `TotalHP/TotalMaxHP`, `HPRatio`,
`RangedCount`, `MeleeCount`, `Strength` (heavy armor = 2, others = 1).

Derived helpers:
- `IsRangedDominant()` — ranged+heavy units outnumber frontline.
- `CommitRange()` — ranged-dominant squads hold at their MaxRange; others use
  `DefaultEngageRange` (5).

`Update()` now receives `boidPool` as a sixth parameter to enable this.

### 2. Range-aware engagement

Combat decisions compare the squared distance to `CommitRange()` instead of a
hardcoded 5.0. A Sniper-heavy squad now holds at range 8 and fires; a
LightInfantry squad closes to 5. The attack command is always issued (units
fire when their individual weapon range is satisfied); the move command is
only issued when the enemy is beyond the squad's commit range.

### 3. Target prioritization (scoreTarget)

Each visible enemy is scored:

```
score = 10 / (distTiles + 1)          // closer is better
if commander:  score *= 3.0           // win condition
if hpRatio<0.3: score *= 1.8          // finish kills
if hpRatio<0.5: score *= 1.3
if ArmorHeavy:  score *= 1.2          // high-threat priority
```

The highest-scoring enemy becomes `TargetUnitID`. A commander at 6 tiles
out-scores a LightInfantry at 4 tiles.

### 4. Force-ratio retreat + regroup

Two-tier retreat logic:

- **Emergency (bypasses cooldown)**: squad HP ratio below `CriticallyLowHP`
  (0.10) → always retreat, regardless of odds.
- **Force-ratio**: `enemyStrength/squadStrength > ForceRatioRetreat` (1.5) AND
  `hpRatio < 0.60` → retreat toward base. Prevents suicide charges.

Retreat auto-clears when HP recovers above `RetreatHPThreshold + 0.15` (0.40),
returning the squad to Idle so it re-enters normal decision-making.

### 5. Offensive push (elimination objective)

`offensivePushCommand()` advances squads toward `EnemyBaseX/Y` (set by
`SetEnemyBasePosition()` in `configureAIStrategy()`). To concentrate force,
squads route through the stronghold nearest the enemy base as a waypoint.
Once within 10 tiles of the enemy base the push releases and other behaviors
(scout/patrol/combat) take over.

This replaces the v1 passive patrol in elimination mode.

### 6. Wave-based recruitment + persistent intel

`recruitDecisions()` accumulates gold and fires in coordinated bursts:

- First wave fires immediately when gold >= 15.
- Subsequent waves respect `RecruitWaveInterval` (60 ticks / ~12s) cooldown.
- Cooldown bypasses if gold >= 3x `RecruitWaveMinGold` (90) — never hoard
  uselessly.
- Max 3 recruits per tick (production spawn rate cap).

**Persistent enemy intel**: `EnemyUnits` map is no longer reset each tick.
Spottings accumulate and decay at `IntelDecayFactor` (0.7) every
`IntelDecayInterval` (100 ticks / ~20s). Adaptive ratios now reflect the
last ~30s of observed enemy composition rather than only what is on screen.

### 7. Scaled base defense

Base-defense recall now requires `BaseDefenseThreshold` (2) enemies near base
instead of 1. A single raider no longer yanks every idle squad home.

## Implementation Details

### Update() Signature

```go
func (as *AISystem) Update(
    tick uint32,
    cmdPool, posPool, ownerPool, healthPool, unitTypePool,
    boidPool *ecs.ComponentPool[component.BoidComponent],
) []AICommand
```

`session.runAI()` extracts `boidPool` from the world and passes it through.

### New AISystem Fields

- `EnemyBaseX, EnemyBaseY int64` — enemy spawn for offensive push
- `lastIntelDecay uint32` — last intel-decay tick
- `lastRecruitWave uint32` — last recruitment-wave tick

### New AI States

- `StatePush` (8) — offensive push toward enemy base
- `StateRegroup` (9) — falling back to rally with reinforcements

### configureAIStrategy() in session.go

Now calls both `SetBasePosition()` and `SetEnemyBasePosition()` using
`AIPlayerID` to select the correct spawn from `Map.Spawns[]`. Works for both
PvAI (AI = player 2) and Clash mode (AI = player 1).

### New Constants

| Constant | Value | Purpose |
|---|---|---|
| `CriticallyLowHP` | 0.10 | emergency-retreat floor |
| `RetreatHPThreshold` | 0.25 | force-ratio retreat gate |
| `ForceRatioRetreat` | 1.5 | enemyStrength/squadStrength threshold |
| `IntelDecayInterval` | 100 ticks | intel halving period |
| `IntelDecayFactor` | 0.7 | 30% reduction per cycle |
| `RecruitWaveInterval` | 60 ticks | recruitment cooldown |
| `RecruitWaveMinGold` | 30 | gold floor for a wave |
| `BaseDefenseThreshold` | 2 | enemies near base to trigger recall |

## Verification

- 29 AI tests pass (13 new for v2 behaviors).
- Full suite: all 18 packages pass (`go test ./...`).
- Range-aware engagement verified: Sniper squad engages at 7 tiles
  (v1 would have closed to 5).
- Target prioritization verified: commander (score 4.29) beats nearby
  LightInfantry (score 2.00) at 6 vs 4 tiles.
- Force-ratio retreat verified: 1 HP-damaged AI unit vs 5 enemies → StateRetreat
  with move command toward base.
- Offensive push verified: elimination objective, tick 200, AI moves toward
  enemy base (x=43) from spawn (x=5).
- Wave timing verified: no recruits during 60-tick cooldown; bypass fires when
  gold piles up.
- Intel persistence verified: sightings survive 1 decay cycle (Sniper 10→7),
  fully decay after ~20 cycles.

## Risks

- **EvalInterval throttle (30 ticks)**: decisions are re-evaluated every 6s,
  not every tick. Between evaluations squads continue their last order. This
  trades reactivity for performance and prevents flip-flopping. Emergency
  retreat (CriticallyLowHP) bypasses the throttle.
- **No true flanking yet**: the offensive push concentrates force via a shared
  stronghold waypoint, which approximates a rally point but does not compute
  distinct approach vectors. True flanking is deferred to a future ADR.
