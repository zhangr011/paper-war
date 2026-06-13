# 0011 — AI Strategic Behavior

Date: 2026-06-13

## Context

The AI opponent had tactical competence (approach, attack, retreat, recruit) but
no strategic layer. In campaign mode (elimination objective), it would sit at spawn
until an enemy wandered into view, then charge forward. It ignored strongholds,
never scouted fogged areas, recruited a fixed role ratio regardless of enemy
composition, and never defended its base when attacked.

Issue #9 requested four strategic behaviors: exploration, stronghold capture,
adaptive recruitment, and base defense.

## Decision

Extend the AISystem with map awareness and intel-gathering capabilities:

### 1. Early-Game Exploration (first 30 seconds)
- First registered squad is assigned as "scout" during the ExploreDuration (150 ticks)
- Scout probes fogged sectors biased toward the enemy side of the map
- Uses FogGrid.IsVisible() to identify unexplored tiles
- Scout avoids combat — returns to normal behavior if engaged

### 2. Stronghold Awareness
- Session scans the map for TerrainStronghold1-5 tiles on startup
- Positions passed to AISystem via SetStrongholds()
- Idle squads (no enemy visible) are sent to nearest unvisited stronghold
- visitedSH map prevents re-assigning the same stronghold repeatedly

### 3. Adaptive Recruitment
- AI tracks enemy unit types as it encounters them (via UnitTypeComponent pool)
- adaptiveRoleRatio() adjusts target composition based on intel:
  - Enemy many Ranged (>40%) → boost Frontline +15% (close distance fast)
  - Enemy many Heavy (>40%) → boost Ranged +15% (Anti-Armor counter)
  - Enemy many Frontline (>50%) → boost Ranged +10% (Snipers pick off infantry)
- Ratios clamped to 10%-60% range, normalized to sum=1.0
- Falls back to default 40/30/30 when no intel

### 4. Base Defense
- Session passes AI spawn position via SetBasePosition()
- countEnemiesNearBase() scans for enemies within BaseDefenseRadius (12 tiles)
- If >=1 enemy near base, squads recalled to defend (StateDefend)
- Combat takes priority over defense — squads already attacking keep attacking

## Implementation Details

### Update() Signature Change
Added `unitTypePool *ecs.ComponentPool[component.UnitTypeComponent]` parameter
to enable enemy composition tracking. The pool is available in session.runAI()
and passed through to Update().

### New AISystem Fields
- `BaseX, BaseY int64` — home base position (fixed-point)
- `Strongholds [][2]int32` — stronghold tile coordinates
- `EnemyUnits map[CombatUnitType]int` — per-tick enemy composition intel
- `visitedSH map[int]bool` — stronghold visit tracking

### configureAIStrategy() in session.go
Called after AISystem creation to inject:
- Base position from Map.Spawns[1]
- Stronghold positions from map tile scan
- Objective reference from Map.Objective

### New AI States
- StateScout (6) — exploring fogged areas
- StateCapture (7) — moving to stronghold

## Verification

- 21 AI tests (8 new for strategic behaviors)
- All 18 packages pass
- Adaptive ratio verified: vs Snipers frontline→55%, vs Heavy ranged→45%
- Stronghold capture verified: AI moves toward nearest stronghold
- Base defense verified: AI responds when enemy near base
- Enemy tracking verified: AI records UnitSniper sightings
