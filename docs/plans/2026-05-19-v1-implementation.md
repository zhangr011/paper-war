# Paper War v1 Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Refactor the existing Paper War prototype to match the v1 design from grilling session 2 (107 questions resolved).

**Architecture:** ECS-based Go server with PostgreSQL persistence, WebGL client. Replace the single-type system with 7 CombatUnitTypes, 4x3 damage matrix, Commander Formation Template, persistent roster with permadeath, and map-driven objectives.

**Tech Stack:** Go 1.22+, PostgreSQL, lib/pq or pgx, WebGL/JS client

**Reference:** Load skill `paper-war-v1-design` before implementing any task. Read `CONTEXT.md` and `docs/adr/` at repo root.

---

## Phase 1: Component & Type Definitions

### Task 1: Define CombatUnitType constants and stats table

**Objective:** Create the 7 CombatUnitType definitions with all stats.

**Files:**
- Create: `server/pkg/component/unittype.go`

**Step 1: Write the unit type definitions**

```go
package component

type WeaponType uint8

const (
    WeaponGun     WeaponType = 0
    WeaponCannon  WeaponType = 1
    WeaponSniper  WeaponType = 2
    WeaponMissile WeaponType = 3
)

type ArmorType uint8

const (
    ArmorLight    ArmorType = 0
    ArmorHeavy    ArmorType = 1
    ArmorBuilding ArmorType = 2
)

type CombatUnitType uint8

const (
    TypeLightInfantry   CombatUnitType = 0
    TypeHeavyInfantry   CombatUnitType = 1
    TypeSniper          CombatUnitType = 2
    TypeAntiArmorInf    CombatUnitType = 3
    TypeMotorGun        CombatUnitType = 4
    TypeMotorArtillery  CombatUnitType = 5
    TypeMotorMissile    CombatUnitType = 6
)

type CombatUnitStats struct {
    Weapon    WeaponType
    Armor     ArmorType
    Cost      uint8
    HP        int32
    Damage    int32
    Range     int64 // in fixed-point tiles
    Cooldown  uint8 // in ticks
    Speed     int64 // base speed (unused if Heavy uses profile)
    RecruitCost int32 // Gold cost
    Bounty     int32  // Kill bounty (80% of RecruitCost)
}

// CombatUnitTypeTable maps each CombatUnitType to its stats.
var CombatUnitTypeTable = map[CombatUnitType]CombatUnitStats{
    TypeLightInfantry: {
        Weapon: WeaponGun, Armor: ArmorLight, Cost: 1,
        HP: 80, Damage: 15, Range: 5, Cooldown: 3,
        RecruitCost: 15, Bounty: 12,
    },
    TypeHeavyInfantry: {
        Weapon: WeaponCannon, Armor: ArmorLight, Cost: 2,
        HP: 60, Damage: 25, Range: 7, Cooldown: 5,
        RecruitCost: 25, Bounty: 20,
    },
    TypeSniper: {
        Weapon: WeaponSniper, Armor: ArmorLight, Cost: 1,
        HP: 40, Damage: 45, Range: 10, Cooldown: 8,
        RecruitCost: 50, Bounty: 40,
    },
    TypeAntiArmorInf: {
        Weapon: WeaponMissile, Armor: ArmorLight, Cost: 2,
        HP: 60, Damage: 35, Range: 8, Cooldown: 6,
        RecruitCost: 30, Bounty: 24,
    },
    TypeMotorGun: {
        Weapon: WeaponGun, Armor: ArmorHeavy, Cost: 2,
        HP: 120, Damage: 15, Range: 5, Cooldown: 2,
        RecruitCost: 25, Bounty: 20,
    },
    TypeMotorArtillery: {
        Weapon: WeaponCannon, Armor: ArmorHeavy, Cost: 4,
        HP: 150, Damage: 40, Range: 7, Cooldown: 5,
        RecruitCost: 50, Bounty: 40,
    },
    TypeMotorMissile: {
        Weapon: WeaponMissile, Armor: ArmorHeavy, Cost: 4,
        HP: 130, Damage: 50, Range: 9, Cooldown: 7,
        RecruitCost: 60, Bounty: 48,
    },
}
```

**Step 2: Write tests for type table completeness**

Create: `server/pkg/component/unittype_test.go`

```go
package component

import "testing"

func TestAllTypesHaveStats(t *testing.T) {
    types := []CombatUnitType{
        TypeLightInfantry, TypeHeavyInfantry, TypeSniper,
        TypeAntiArmorInf, TypeMotorGun, TypeMotorArtillery, TypeMotorMissile,
    }
    for _, ut := range types {
        stats, ok := CombatUnitTypeTable[ut]
        if !ok {
            t.Errorf("CombatUnitType %d missing from table", ut)
            continue
        }
        if stats.HP <= 0 || stats.Damage <= 0 || stats.Range <= 0 {
            t.Errorf("CombatUnitType %d has invalid stats", ut)
        }
        if stats.RecruitCost <= 0 || stats.Bounty <= 0 {
            t.Errorf("CombatUnitType %d has invalid economy", ut)
        }
    }
}

func TestBountyIs80Percent(t *testing.T) {
    for ut, stats := range CombatUnitTypeTable {
        expected := (stats.RecruitCost * 80) / 100
        if stats.Bounty != expected {
            t.Errorf("CombatUnitType %d: bounty %d != 80%% of %d = %d",
                ut, stats.Bounty, stats.RecruitCost, expected)
        }
    }
}
```

**Step 3: Run tests**

Run: `cd /Users/zhangrong/repo/paper-war/server && go test ./pkg/component/ -v -run TestAll`
Expected: PASS

**Step 4: Commit**

```bash
git add server/pkg/component/unittype.go server/pkg/component/unittype_test.go
git commit -m "feat: define 7 CombatUnitTypes with stats, cost, and economy"
```

---

### Task 2: Define the 4x3 Damage Matrix

**Objective:** Create the damage matrix lookup.

**Files:**
- Create: `server/pkg/component/damagematrix.go`
- Create: `server/pkg/component/damagematrix_test.go`

**Step 1: Write the damage matrix**

```go
package component

// DamageMultiplier returns the damage multiplier for a weapon vs an armor type.
// Values: 0.0 (immune), 0.25, 0.50, 1.0, 1.50, 1.50
// Stored as fixed-point with 2 decimal places (e.g., 150 = 1.50x)
func DamageMultiplier(weapon WeaponType, armor ArmorType) int32 {
    // Rows: Gun, Cannon, Sniper, Missile
    // Cols: Light, Heavy, Building
    matrix := [4][3]int32{
        {100, 50, 0},   // Gun
        {50, 100, 25},  // Cannon
        {150, 25, 0},   // Sniper
        {25, 150, 25},  // Missile
    }
    return matrix[weapon][armor]
}

// CanDamageTerrain returns true if the weapon can damage Building armor.
func CanDamageTerrain(weapon WeaponType) bool {
    return DamageMultiplier(weapon, ArmorBuilding) > 0
}
```

**Step 2: Write tests**

```go
package component

import "testing"

func TestDamageMatrixDimensions(t *testing.T) {
    for w := WeaponType(0); w <= 3; w++ {
        for a := ArmorType(0); a <= 2; a++ {
            mult := DamageMultiplier(w, a)
            if mult < 0 || mult > 200 {
                t.Errorf("DamageMultiplier(%d,%d) = %d, out of range", w, a, mult)
            }
        }
    }
}

func TestSniperDevastatesLight(t *testing.T) {
    if DamageMultiplier(WeaponSniper, ArmorLight) != 150 {
        t.Error("Sniper should deal 150% to Light")
    }
    if DamageMultiplier(WeaponSniper, ArmorHeavy) != 25 {
        t.Error("Sniper should deal 25% to Heavy")
    }
}

func TestMissileShredsHeavy(t *testing.T) {
    if DamageMultiplier(WeaponMissile, ArmorHeavy) != 150 {
        t.Error("Missile should deal 150% to Heavy")
    }
}

func TestTerrainDamage(t *testing.T) {
    if !CanDamageTerrain(WeaponCannon) {
        t.Error("Cannon should damage terrain")
    }
    if !CanDamageTerrain(WeaponMissile) {
        t.Error("Missile should damage terrain")
    }
    if CanDamageTerrain(WeaponGun) {
        t.Error("Gun should not damage terrain")
    }
    if CanDamageTerrain(WeaponSniper) {
        t.Error("Sniper should not damage terrain")
    }
}
```

**Step 3: Run tests**

Run: `cd /Users/zhangrong/repo/paper-war/server && go test ./pkg/component/ -v -run TestDamage`
Expected: PASS

**Step 4: Commit**

```bash
git add server/pkg/component/damagematrix.go server/pkg/component/damagematrix_test.go
git commit -m "feat: add 4x3 damage matrix (Gun/Cannon/Sniper/Missile vs Light/Heavy/Building)"
```

---

### Task 3: Add UnitType component field to entities

**Objective:** Extend the existing component system to track CombatUnitType on each entity.

**Files:**
- Modify: `server/pkg/component/health.go` — add UnitType field
- Modify: `server/pkg/component/commander.go` — add Formation Template fields, Leading Skill, Level, KillPoints, Gold
- Modify: `server/pkg/component/owner.go` — add IsCommander flag

**Step 1: Add UnitType to HealthComponent** (or create a new UnitTypeComponent)

The cleanest approach is a new component:

Create: `server/pkg/component/unittype_component.go`

```go
package component

// UnitTypeComponent stores the combat unit type and leveling info.
type UnitTypeComponent struct {
    Type       CombatUnitType
    Level      uint8   // max 6 for CombatUnit, max 10 for Commander
    KillPoints uint32  // cumulative
    Gold       int32   // only meaningful for Commander (Squad gold pool)
}
```

**Step 2: Update CommanderComponent**

Modify: `server/pkg/component/commander.go`

Add these fields to CommanderComponent:
```go
LeadingSkill    uint8   // max 50, career-persistent
IsCommander     bool    // true = this is a Commander entity
```

Formation Template is stored in the DB as JSONB, not as an ECS component. The session loads it at match start.

**Step 3: Commit**

```bash
git add server/pkg/component/
git commit -m "feat: add UnitTypeComponent and extend CommanderComponent with LeadingSkill"
```

---

### Task 4: Define two MovementProfiles (Light and Heavy)

**Objective:** Replace the single default movement profile with Light and Heavy profiles matching the terrain cost table.

**Files:**
- Modify: `server/pkg/component/movement.go` — add profile definitions
- Modify: `server/pkg/game/session.go` — register both profiles

**Step 1: Add profile constants and constructors**

In `server/pkg/component/movement.go`, add:

```go
const (
    ProfileLight uint8 = 0
    ProfileHeavy uint8 = 1
)

func LightMovementProfile() *MovementProfile {
    return &MovementProfile{
        ID: ProfileLight,
        TerrainCosts: [16]uint8{
            TerrainPlain:       1,
            TerrainRoad:        1,
            TerrainShallow:     2,
            TerrainDeep:        0,
            TerrainForest:      2,
            TerrainHill:        2,
            TerrainSwamp:       2,
            TerrainBridge:      1,
            TerrainWall:        0,
            TerrainSnow:        2,
            TerrainDesert:      2,
            TerrainStronghold1: 1,
            TerrainStronghold2: 1,
            TerrainStronghold3: 1,
            TerrainStronghold4: 1,
            TerrainStronghold5: 1,
        },
    }
}

func HeavyMovementProfile() *MovementProfile {
    return &MovementProfile{
        ID: ProfileHeavy,
        TerrainCosts: [16]uint8{
            TerrainPlain:       2,
            TerrainRoad:        1,
            TerrainShallow:     0,
            TerrainDeep:        0,
            TerrainForest:      4,
            TerrainHill:        4,
            TerrainSwamp:       0,
            TerrainBridge:      1,
            TerrainWall:        0,
            TerrainSnow:        4,
            TerrainDesert:      4,
            TerrainStronghold1: 1,
            TerrainStronghold2: 1,
            TerrainStronghold3: 1,
            TerrainStronghold4: 1,
            TerrainStronghold5: 1,
        },
    }
}
```

**Step 2: Write test verifying profiles**

```go
func TestLightCanCrossShallow(t *testing.T) {
    p := LightMovementProfile()
    if p.TerrainCosts[TerrainShallow] == 0 {
        t.Error("Light should be able to cross shallow water")
    }
}

func TestHeavyCannotCrossShallow(t *testing.T) {
    p := HeavyMovementProfile()
    if p.TerrainCosts[TerrainShallow] != 0 {
        t.Error("Heavy should not be able to cross shallow water")
    }
}

func TestBothSameSpeedOnRoad(t *testing.T) {
    l := LightMovementProfile()
    h := HeavyMovementProfile()
    if l.TerrainCosts[TerrainRoad] != h.TerrainCosts[TerrainRoad] {
        t.Error("Light and Heavy should have same speed on road")
    }
}
```

**Step 3: Commit**

```bash
git add server/pkg/component/movement.go
git commit -m "feat: add Light and Heavy movement profiles per terrain cost table"
```

---

## Phase 2: Persistence Layer

### Task 5: PostgreSQL schema migration

**Objective:** Create the database tables for players, commanders, and match state.

**Files:**
- Create: `server/pkg/persistence/schema.sql`
- Create: `server/pkg/persistence/migrate.go`

**Step 1: Write the schema**

```sql
-- server/pkg/persistence/schema.sql

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE players (
    id          BIGSERIAL PRIMARY KEY,
    token       VARCHAR(64) UNIQUE NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE commanders (
    id              BIGSERIAL PRIMARY KEY,
    player_id       BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    weapon          SMALLINT NOT NULL,  -- WeaponType enum
    armor           SMALLINT NOT NULL,  -- ArmorType enum
    unit_type       SMALLINT NOT NULL,  -- CombatUnitType enum
    leading_skill   SMALLINT NOT NULL DEFAULT 2,
    kill_points     INT NOT NULL DEFAULT 0,
    level           SMALLINT NOT NULL DEFAULT 1,
    formation       JSONB NOT NULL,     -- Formation Template slots
    combat_units    JSONB NOT NULL,     -- array of attached CombatUnits
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Step 2: Write migration helper**

```go
package persistence

import "database/sql"

func Migrate(db *sql.DB) error {
    _, err := db.Exec(Schema)
    return err
}
```

**Step 3: Commit**

```bash
git add server/pkg/persistence/
git commit -m "feat: add PostgreSQL schema for players and commanders"
```

---

### Task 6: Persistence CRUD operations

**Objective:** Write the Go functions to load/save rosters.

**Files:**
- Create: `server/pkg/persistence/roster.go`
- Create: `server/pkg/persistence/roster_test.go`

Key functions:
- `FindOrCreatePlayer(db, token) (playerID int64, err)`
- `LoadRoster(db, playerID) ([]CommanderRow, error)`
- `SaveCommander(db, commanderRow) error`
- `DeleteCommander(db, commanderID) error`
- `CreateStarterRoster(db, playerID) error`

Each CommanderRow contains: ID, PlayerID, Weapon, Armor, UnitType, LeadingSkill, KillPoints, Level, Formation (JSONB), CombatUnits (JSONB).

**Step 1-5:** TDD cycle for each function.

**Step 6: Commit**

```bash
git add server/pkg/persistence/
git commit -m "feat: add roster CRUD operations with PostgreSQL"
```

---

## Phase 3: Combat System Rewrite

### Task 7: Rewrite CombatSystem with damage matrix and smart targeting

**Objective:** Replace the current combat system with type-aware damage, splash, and priority targeting.

**Files:**
- Modify: `server/pkg/combat/combat.go`
- Modify: `server/pkg/combat/combat_test.go`

Key changes:
- Look up attacker weapon from UnitTypeComponent
- Look up target armor from UnitTypeComponent
- Apply DamageMultiplier from the matrix
- Cannon splash: 2-tile radius with falloff
- Smart targeting: iterate enemies in range, pick by priority (150% > 100% > 50% > 25%)
- Award Gold bounties on kill (store on Commander's UnitTypeComponent)
- Award kill points on kill

**Commit:**

```bash
git commit -am "feat: rewrite CombatSystem with 4x3 damage matrix, splash, smart targeting, gold bounties"
```

---

### Task 8: Rewrite DeathSystem with promotion and permadeath tracking

**Objective:** Handle Commander death (promotion), track dead entities for roster persistence.

**Files:**
- Modify: `server/pkg/combat/death.go`
- Modify: `server/pkg/combat/death_test.go`

Key changes:
- On Commander death: find highest-level CombatUnit in same Squad, promote to Commander
- Promoted unit keeps its own CombatUnitType
- Track dead entity IDs in a DeathLog for the persistence flush
- Award kill points to the killer's Commander

**Commit:**

```bash
git commit -am "feat: rewrite DeathSystem with Commander promotion and death logging"
```

---

## Phase 4: Movement System

### Task 9: Simplify Boid to attraction + separation only

**Objective:** Remove cohesion and alignment forces. Keep attraction to formation offset and separation.

**Files:**
- Modify: `server/pkg/boid/forces.go`
- Modify: `server/pkg/boid/forces_test.go`

Key changes:
- Remove CohesionW and AlignmentW from BoidComponent (or set to 0)
- Keep SeparationW and FormationW
- FormationW pulls unit toward its offset from Commander

**Commit:**

```bash
git commit -am "feat: simplify Boid to attraction + separation only"
```

---

### Task 10: Assign MovementProfile based on armor type

**Objective:** Light armor units use ProfileLight, Heavy armor units use ProfileHeavy.

**Files:**
- Modify: `server/pkg/game/session.go` — SpawnSquad assigns profile by CombatUnitType

Key change: When spawning a unit, set `MovementComponent{ProfileID}` based on `CombatUnitTypeTable[type].Armor`:
- ArmorLight → ProfileLight (0)
- ArmorHeavy → ProfileHeavy (1)

**Commit:**

```bash
git commit -am "feat: assign Light/Heavy movement profile based on CombatUnitType armor"
```

---

## Phase 5: Recruitment & Economy

### Task 11: Create RecruitmentSystem

**Objective:** New system that handles Gold-based recruitment respecting Formation Template.

**Files:**
- Create: `server/pkg/recruit/recruit.go`
- Create: `server/pkg/recruit/recruit_test.go`

Logic:
- Player sends Recruit command with desired CombatUnitType
- Check: Gold >= type's RecruitCost
- Check: Formation Template has available slot for that type (scaled by Leading Skill)
- Check: Total cost (existing units + new unit's cost) <= Leading Skill
- If all pass: deduct Gold, spawn unit at Commander position with correct type and profile

**Commit:**

```bash
git commit -am "feat: add RecruitmentSystem with Formation Template and Gold economy"
```

---

### Task 12: Create LevelingSystem

**Objective:** Track kill points, handle level-ups for CombatUnits and Commanders.

**Files:**
- Create: `server/pkg/leveling/leveling.go`
- Create: `server/pkg/leveling/leveling_test.go`

Logic:
- After each kill, add kill point to the killer's UnitTypeComponent
- Level thresholds: 2, 6, 14, 30, 62 (cumulative) for CombatUnits (max 6)
- Level thresholds: 2, 6, 14, 30, 62, 126, 254, 510, 1022 (cumulative) for Commanders (max 10)
- On level-up: recalculate stats (CombatUnit: +10% HP/Dmg per level, Commander: lookup multiplier table)
- On Commander kill point: check Leading Skill growth (every 3 kills = +1 for v1)

**Commit:**

```bash
git commit -am "feat: add LevelingSystem with kill points and level-up"
```

---

## Phase 6: Map & Objectives

### Task 13: Add Objective field to GameMap

**Objective:** GameMap carries an explicit objective type and data.

**Files:**
- Modify: `server/pkg/tilemap/tilemap.go` — add Objective fields
- Modify: `server/pkg/tilemap/generate.go` — set objective based on map generation

```go
type ObjectiveType uint8

const (
    ObjectiveElimination ObjectiveType = 0
    ObjectiveCapture     ObjectiveType = 1
    ObjectiveSurvival    ObjectiveType = 2
)

type ObjectiveData struct {
    Type     ObjectiveType
    TargetX  int32   // for Capture
    TargetY  int32   // for Capture
    HoldTime uint32  // ticks for Capture
    Duration uint32  // ticks for Survival
}
```

Add `Objective ObjectiveData` field to GameMap struct.

**Commit:**

```bash
git commit -am "feat: add Objective field to GameMap with Elimination/Capture/Survival types"
```

---

### Task 14: Update map generator constraints

**Objective:** Road-connected spawns, minimum 3 bridges, shallow fords, indestructible strongholds.

**Files:**
- Modify: `server/pkg/tilemap/generate.go`

Key changes:
- Bridge count: `3 + r.Intn(1)` (always 3)
- After bridge placement: ensure road from each spawn area to nearest bridge
- Add 2-3 shallow water fords placed far from bridges
- Stronghold placement: set Health=0, MaxHealth=0 (indestructible)

**Commit:**

```bash
git commit -am "feat: update map generator with road-connected spawns, 3 bridges, shallow fords, indestructible strongholds"
```

---

### Task 15: Create ObjectiveSystem

**Objective:** Check win conditions each tick.

**Files:**
- Create: `server/pkg/objective/objective.go`
- Create: `server/pkg/objective/objective_test.go`

Logic:
- Elimination: check if all entities of one faction are dead
- Capture: check Commander presence on target stronghold, tug-of-war counter
- Survival: check timer, if expired and AI has units → AI wins; if all AI dead → player wins

**Commit:**

```bash
git commit -am "feat: add ObjectiveSystem with Elimination, Capture, Survival"
```

---

## Phase 7: Match Flow & Persistence Integration

### Task 16: Token auth and match lifecycle

**Objective:** Token-based player identification, roster loading, match start/end.

**Files:**
- Modify: `server/pkg/game/session.go` — add player token, roster loading
- Modify: `server/pkg/game/matchmaker.go` — handle match creation with roster
- Modify: `server/cmd/server/main.go` — add DB connection

Key changes:
- On connect: extract token from URL query param or header
- FindOrCreatePlayer in DB
- Load Roster from DB
- On "Start Match": player selects Commander, load formation + combat_units from DB, spawn ECS entities
- Every 30 seconds during match: flush roster state to DB
- On match end: final flush, write survivors back, delete dead units

**Commit:**

```bash
git commit -am "feat: add token auth, roster loading, match lifecycle with PostgreSQL persistence"
```

---

## Phase 8: Tick Pipeline Assembly

### Task 17: Assemble the 13-system pipeline

**Objective:** Wire all systems into the game loop in correct order.

**Files:**
- Modify: `server/pkg/game/session.go` — NewGameSession creates all systems

New pipeline order:
1. TerrainSystem
2. CommanderSystem (tactical orders)
3. RecruitmentSystem (new)
4. MovementSystem
5. SpatialHash rebuild
6. CombatSystem (rewritten)
7. DeathSystem (rewritten)
8. LevelingSystem (new)
9. ObjectiveSystem (new)
10. FogSystem
11. AISystem
12. SnapshotSystem

Remove ProjectileSystem from the pipeline.

Update `ServerTicksPerSecond` from 5 to 10.

**Commit:**

```bash
git commit -am "feat: assemble 13-system pipeline at 10Hz, remove ProjectileSystem"
```

---

## Phase 9: Client Updates

### Task 18: Update client rendering for type distinction

**Objective:** Render different unit types as different colored shapes.

**Files:**
- Modify: `client/src/gl.js` — render by unit type
- Modify: `client/src/state.js` — parse new snapshot format

Key changes:
- Unit type from snapshot determines color/shape
- Light Infantry = small green circle
- Heavy Infantry = small orange circle
- Sniper = small blue triangle
- Anti-Armor Infantry = small red triangle
- Motor Gun = medium green square
- Motor Artillery = medium orange square
- Motor Missile = medium red square
- Commander = larger shape with white border

**Commit:**

```bash
git commit -am "feat: render 7 unit types as colored shapes"
```

---

### Task 19: Add client UI for Gold, Recruit, and Commander selection

**Objective:** Minimal HUD showing Gold balance, Recruit button, and Commander type selection.

**Files:**
- Modify: `client/src/gl.js` — HUD overlay
- Modify: `client/src/input.js` — keyboard shortcuts (R=Recruit, G=AttackGround, 1-4=TacticalOrders)

**Commit:**

```bash
git commit -am "feat: add HUD with Gold counter, Recruit button, AttackGround mode, tactical orders"
```

---

### Task 20: Extend snapshot protocol

**Objective:** Add unit type, level, Gold, and objective data to the network snapshot.

**Files:**
- Modify: `server/pkg/network/protocol.go` — extend UnitUpdate with type field
- Modify: `server/pkg/network/snapshot.go` — include Gold and objective state
- Modify: `server/pkg/network/protocol_test.go` — test new format

**Commit:**

```bash
git commit -am "feat: extend snapshot protocol with unit type, level, Gold, objective data"
```

---

## Summary

20 tasks across 9 phases. Each task is designed to be independently testable and committable.

**Dependency order:**
- Tasks 1-4 (definitions) can run in parallel
- Task 5-6 (persistence) depends on Task 1
- Task 7-8 (combat) depends on Tasks 1-3
- Task 9-10 (movement) depends on Task 4
- Task 11-12 (economy) depends on Tasks 1, 3
- Task 13-15 (map/objectives) depends on Task 3
- Task 16 (match flow) depends on Tasks 5-6, 11-12, 15
- Task 17 (pipeline) depends on all above
- Tasks 18-20 (client) depends on Task 20 (protocol), can run in parallel with Task 17

**Critical path:** Tasks 1 → 3 → 7 → 8 → 16 → 17 → 20 → 18
