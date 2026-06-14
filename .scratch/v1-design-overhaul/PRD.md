# PRD: v1 Design Overhaul — CombatUnitType System, Persistent Roster, and Map Objectives

## Problem Statement

The current Paper War prototype has a flat unit system with only melee/ranged distinction, a single movement profile, no persistence between matches, no economy, and no win conditions. The prototype was built to validate ECS architecture and movement/pathfinding but cannot support the v1 game design: 7 distinct CombatUnitTypes with a 4×3 damage matrix, Commander Formation Templates, persistent rosters with permadeath, Gold-based recruitment, and map-driven objectives (Elimination, Capture, Survival).

## Solution

Rewrite and extend the game to implement the complete v1 design resolved across 107 grilling-session questions. This introduces a typed combat system (7 CombatUnitTypes), a 4×3 damage matrix (Gun/Cannon/Sniper/Missile vs Light/Heavy/Building), Commander-centric persistent rosters stored in PostgreSQL as JSONB, a Gold economy with kill bounties and recruitment, smart auto-targeting with priority tiers, Commander promotion on death, kill-point leveling, two movement profiles (Light and Heavy), map objectives, and a 13-system tick pipeline at 10 Hz.

## User Stories

### Type System & Combat

1. As a player, I want 7 distinct CombatUnitTypes with unique stats and roles, so that army composition matters strategically
2. As a player, I want Light Infantry (Gun/Light) to be cheap and numerous, so that I can field a large swarm
3. As a player, I want Heavy Infantry (Cannon/Light) to deal splash damage at range, so that I can area-denie enemy clusters
4. As a player, I want Snipers to deal devastating damage to Light armor (150%) but nearly nothing to Heavy (25%), so that I can counter infantry swarms
5. As a player, I want Anti-Armor Infantry to shred Heavy armor (150%) but struggle against Light (25%), so that I can counter motor units
6. As a player, I want Motor Gun units to be tough and fast-firing, so that I can hold the front line
7. As a player, I want Motor Artillery to deal heavy splash damage, so that I can siege fortified positions
8. As a player, I want Motor Missile to deal the highest single-target damage in the game, so that I can eliminate high-value targets
9. As a player, I want the damage matrix to create clear counter-play (Sniper vs Light, Missile vs Heavy), so that composition decisions are meaningful
10. As a player, I want Cannon splash to deal full damage at impact, 50% at 1 tile, and 25% at 2 tiles, so that positioning matters against artillery
11. As a player, I want Gun, Sniper, and Missile to be single-target only, so that only Cannon provides area damage
12. As a player, I want units to auto-target enemies they are most effective against (150% > 100% > 50% > 25%), so that micro is optional but rewarding
13. As a player, I want units to skip 25% targets unless no other enemy exists, so that Snipers don't waste shots on Heavy armor

### Terrain & Movement

14. As a player, I want Light armor units to traverse most terrain at cost 1-2, so that infantry is mobile
15. As a player, I want Heavy armor units to be faster on roads (cost 1) but slower on plains (cost 2), so that road control matters
16. As a player, I want Heavy armor units to be unable to cross shallow water, swamps, or deep water, so that terrain creates natural barriers for motor units
17. As a player, I want Light armor units to cross shallow water (cost 2) and forests (cost 2), so that infantry can flank through difficult terrain
18. As a player, I want both Light and Heavy to move at the same speed on roads and bridges, so that mixed squads stay together on roads
19. As a player, I want movement to use attraction (toward formation offset from Commander) and separation (from nearby units) only, so that squads move naturally without complex Boid behavior
20. As a player, I want Cannon and Missile units to be able to destroy terrain via AttackGround, so that I can open new paths or breach walls
21. As a player, I want terrain tiles to have Building armor, so that terrain damage uses the same damage matrix as combat
22. As a player, I want terrain destruction to take real time (25% of base damage per hit), so that siege is a deliberate strategic commitment
23. As a player, I want Gun and Sniper units to be unable to damage terrain (0% to Building armor), so that only siege-capable units can alter the map

### Commanders & Formation Templates

24. As a player, I want each Commander to have a Formation Template defining which CombatUnitTypes the Squad can recruit, so that Commander choice determines army composition
25. As a player, I want Formation Template slots to scale proportionally with Leading Skill, so that a growing Commander fields a larger army
26. As a player, I want Leading Skill to be the total cost budget for the Squad, so that cheap units allow more total entities
27. As a player, I want the Commander's own unit cost to count toward the Leading Skill budget, so that Heavy Commanders field fewer units but are individually stronger
28. As a player, I want a Gun/Light Commander (cost 1) to field more units than a Cannon/Heavy Commander (cost 4), so that there is a trade-off between Commander power and army size
29. As a player, I want the Commander to be a hero unit with stats scaling from 2x HP / 1.5x damage at level 1 to 5x HP / 3x damage at level 10, so that veteran Commanders are exponentially more powerful
30. As a player, I want the Commander to fight normally without special AI behavior, so that the Commander is part of the combat simulation
31. As a player, I want the Commander to issue tactical orders (Follow, Charge, Retreat, Hold), so that I can control squad behavior at a high level

### Permadeath & Promotion

32. As a player, I want CombatUnits that die in a match to be permanently removed from my roster, so that every loss has real consequences
33. As a player, I want Commanders that die to permanently die with all attached CombatUnits, so that Commander survival is the highest priority
34. As a player, I want the highest-level surviving CombatUnit to promote to Commander when the Commander dies, so that the squad doesn't disintegrate
35. As a player, I want a promoted unit to keep its own CombatUnitType (weapon and armor), so that a promoted Sniper Commander creates a Sniper-led squad
36. As a player, I want a promoted Commander's Squad to potentially become a mixed-type squad, so that the squad's strategic role shifts after promotion

### Leveling

37. As a player, I want CombatUnits to earn kill points for each enemy killed, so that combat experience is rewarded
38. As a player, I want CombatUnits to level up (max 6) at exponential kill point thresholds (2, 6, 14, 30, 62 cumulative), so that veteran units are rare and valuable
39. As a player, I want each level to grant +10% HP and +10% damage, so that a level 6 unit is 50% stronger than a level 1 unit
40. As a player, I want Commanders to level up to max 10 with the same kill point system, so that Commander progression is longer and more meaningful
41. As a player, I want kill points and levels to update immediately during combat, so that a unit that just killed an enemy might level up and survive the next hit
42. As a player, I want Commander stats to scale aggressively with level (2x→5x HP, 1.5x→3x damage), so that a level 10 Commander is a force of nature

### Economy

43. As a player, I want to earn Gold by killing enemy units, so that aggression is rewarded economically
44. As a player, I want kill bounties to equal 80% of the killed unit's recruit cost, so that killing expensive units is more lucrative
45. As a player, I want Commander kill bounties to equal max(100, 50 × Commander unit cost), so that Commander kills are always a big deal (minimum 100 Gold)
46. As a player, I want to spend Gold to recruit new CombatUnits at the Commander's position, so that I can reinforce during a match
47. As a player, I want recruitment to respect Formation Template slots and Leading Skill budget, so that I can't recruit units my Commander doesn't support
48. As a player, I want unspent Gold to be lost at match end, so that hoarding is punished and spending is encouraged

### Persistence & Roster

49. As a player, I want my roster (Commanders and CombatUnits) to persist between matches, so that my army grows over time
50. As a player, I want the server to store my roster in PostgreSQL, so that progress survives server restarts
51. As a player, I want each Commander to store its Formation Template and attached CombatUnits as JSONB on a single Commander row, so that loading one Commander loads everything
52. As a player, I want roster changes to flush to the database every 30 seconds during a match, so that a server crash loses at most 30 seconds of progress
53. As a player, I want lobby changes to flush to the database with a 10-second debounce, so that rapid adjustments are batched into one write
54. As a player, I want to connect via token auth (no login screen), so that I can start playing immediately
55. As a player, I want a starter roster (1 Gun/Light Commander + 5 Light Infantry) to be created on first connection, so that new players can play right away
56. As a player, I want to receive a new starter roster immediately if my entire roster is destroyed, so that the game never stops
57. As a player, I want my roster to be loaded into memory at match start, so that the match simulation doesn't need database access during gameplay
58. As a player, I want the surviving roster to be written back to the database at match end, so that permadeath is reflected in my persistent roster

### Map & Objectives

59. As a player, I want the map generator to guarantee road-connected spawns, so that both sides have reliable paths to the battlefield
60. As a player, I want the map to have a minimum of 3 bridges, so that water barriers are always crossable
61. As a player, I want shallow water fords placed far from bridges, so that flanking routes exist but require detours
62. As a player, I want strongholds to be indestructible, so that capture objectives can't be destroyed
63. As a player, I want walls to be able to cross roads, so that sieging a road is sometimes necessary
64. As a player, I want map objectives to determine the win condition, so that each match feels different
65. As a player, I want Elimination objectives to have no timeout, so that the match ends only when one side is wiped out
66. As a player, I want Capture objectives to require my Commander on the target stronghold with a tug-of-war counter (300 ticks hold to win), so that holding ground is the goal
67. As a player, I want Survival objectives to have a per-map timer where I must eliminate all AI defenders before time expires, so that aggression is rewarded
68. As a player, I want to always be the attacker (player) against an AI defender, so that v1 is simple Player vs AI

### AI

69. As a player, I want the AI to field a random Commander type, so that each match has different enemy composition
70. As a player, I want the AI to use smart auto-targeting, so that it doesn't waste attacks on ineffective targets
71. As a player, I want the AI to recruit units proportionally to its Formation Template slots, so that its army composition is balanced
72. As a player, I want the AI to patrol near the objective on Capture maps, so that it defends the target
73. As a player, I want the AI to fight normally on Survival maps, so that it's a real threat
74. As a player, I want the AI to never use AttackGround, so that it doesn't destroy its own defensive terrain

### Client & UI

75. As a player, I want to see 7 distinct unit types as different colored shapes, so that I can visually identify unit types
76. As a player, I want Light Infantry shown as small green circles, so that I can distinguish cheap infantry
77. As a player, I want Snipers shown as small blue triangles, so that I can spot my long-range units
78. As a player, I want Motor units shown as medium squares (green/orange/red by weapon), so that I can identify armored vehicles
79. As a player, I want my Commander shown with a white border, so that I always know where my Commander is
80. As a player, I want a Gold counter in the HUD, so that I know how much I can spend on recruitment
81. As a player, I want a Recruit button (keyboard shortcut R), so that I can reinforce during combat
82. As a player, I want an AttackGround mode (keyboard shortcut G), so that I can order siege units to destroy terrain
83. As a player, I want tactical order keys (1-4 for Follow/Charge/Retreat/Hold), so that I can control squad behavior quickly
84. As a player, I want click-based input (left click select, right click context action), so that controls are intuitive
85. As a player, I want a lobby screen where I see my roster and select a Commander, so that I can choose my army before battle
86. As a player, I want a "Start Match" button in the lobby, so that I control when to commit

### Match Flow

87. As a player, I want the match to start with auto-deployed highest-level CombatUnits up to my Leading Skill cap, so that my strongest units fight first
88. As a player, I want the server to tick at 10 Hz, so that the simulation is responsive
89. As a player, I want the client to use interpolation only (no prediction), so that what I see matches server state
90. As a player, I want the server to run a 13-system pipeline (Terrain → Commander → Recruitment → Movement → SpatialHash → Combat → Death → Leveling → Fog → Objective → AI → Snapshot), so that all game logic executes in deterministic order
91. As a player, I want ProjectileSystem removed from v1 (instant damage only), so that combat is simpler to implement

## Implementation Decisions

### CombatUnitType system

All unit types are defined as a Go lookup table in code (not in the database). Each CombatUnitType bundles weapon type, armor type, HP, damage, range, cooldown, unit cost, recruit cost, and kill bounty. The type ID is stored on each entity's UnitTypeComponent. Type definitions are canonical and never change at runtime.

Types:
- Light Infantry: Gun/Light, cost 1, HP 80, Dmg 15, Range 5, CD 3, Recruit 15g, Bounty 12g
- Heavy Infantry: Cannon/Light, cost 2, HP 60, Dmg 25, Range 7, CD 5, Recruit 25g, Bounty 20g
- Sniper: Sniper/Light, cost 1, HP 40, Dmg 45, Range 10, CD 8, Recruit 50g, Bounty 40g
- Anti-Armor Infantry: Missile/Light, cost 2, HP 60, Dmg 35, Range 8, CD 6, Recruit 30g, Bounty 24g
- Motor Gun: Gun/Heavy, cost 2, HP 120, Dmg 15, Range 5, CD 2, Recruit 25g, Bounty 20g
- Motor Artillery: Cannon/Heavy, cost 4, HP 150, Dmg 40, Range 7, CD 5, Recruit 50g, Bounty 40g
- Motor Missile: Missile/Heavy, cost 4, HP 130, Dmg 50, Range 9, CD 7, Recruit 60g, Bounty 48g

Sniper/Heavy does not exist as a type.

### Damage matrix

A 4×3 lookup: rows = Gun/Cannon/Sniper/Missile, columns = Light/Heavy/Building. Returns a fixed-point percentage (100 = 1.0x). Cannon and Missile deal 25% to Building armor. Gun and Sniper deal 0% to Building. Terrain tiles are treated as Building armor for AttackGround calculations. There is no separate "terrain damage" system — the damage matrix handles everything.

### Smart auto-targeting

Four priority tiers: 150% targets first, then 100%, then 50%, then 25% as last resort. Each unit scans nearby enemies (via SpatialHash), groups them by damage multiplier, and picks the closest enemy from the highest available tier.

### Splash

Only Cannon-type attacks generate splash. Radius 2 tiles. Damage: 100% at center, 50% at 1 tile, 25% at 2 tiles. Missile, Gun, and Sniper are strictly single-target.

### Movement profiles

Two MovementProfiles replace the single default. Light profile: most terrain at cost 1-2, can cross shallow water (cost 2). Heavy profile: plains at cost 2, roads at cost 1, cannot cross shallow water/swamp/deep water. Both move identically on roads and bridges (cost 1). Profile is assigned based on CombatUnitType armor: ArmorLight → ProfileLight, ArmorHeavy → ProfileHeavy.

Boid forces reduced to attraction (formation offset from Commander) and separation (from nearby units) only. Cohesion and alignment weights removed.

### Commander and Formation Template

CommanderComponent extended with LeadingSkill (max 50) and IsCommander flag. Formation Template is stored as JSONB in the commanders table: an array of `{type: "light_infantry", slots: 8}` entries. Slot counts scale proportionally with Leading Skill. The Commander's own unit cost counts toward the Leading Skill budget.

Commander combat stats scale with level: HP multiplier from 2x (level 1) to 5x (level 10), damage multiplier from 1.5x to 3x. Base stats come from the CombatUnitType table, then multiplied by the level-based scaling factor.

### Commander promotion on death

When the Commander entity dies, DeathSystem finds the highest-level CombatUnit in the same Squad (by SquadID) and promotes it. The promoted unit keeps its CombatUnitType. The Squad may become mixed-type after promotion. All attached CombatUnits remain with the Squad.

### Leveling

Kill points accumulate on UnitTypeComponent. CombatUnit max level 6, Commander max level 10. Exponential thresholds (2, 4, 8, 16, 32, 64 per level). CombatUnit leveling grants +10% HP and +10% damage per level (multiplicative). Commander leveling uses the Commander Stats multiplier table. Level-ups are applied immediately during the match — a unit that just earned enough kill points levels up before the next combat tick.

### Gold economy

Kill bounties = 80% of recruit cost (rounded). Commander bounties = max(100, 50 × Commander unit cost). Gold is stored on the Commander's UnitTypeComponent. Gold is earned immediately on kill. Gold is spent via Recruit command. Unspent Gold is lost at match end. Starting Gold per match = 50.

### Persistence

PostgreSQL with two tables: `players` (id, token) and `commanders` (id, player_id, weapon, armor, unit_type, leading_skill, kill_points, level, formation JSONB, combat_units JSONB). Token auth: unique token on first connection, stored in localStorage. Starter roster created immediately on first connection.

Roster loaded into memory at match start. Periodic flush every 30 seconds during match writes kill points, levels, Leading Skill, and dead unit removal to DB. Lobby changes use immediate write with 10-second debounce. Final flush on match end. On server crash, roster reverts to last successful flush.

### Map generator

Road-connected spawns guaranteed. Minimum 3 bridges. Shallow water fords placed far from bridges. Strongholds indestructible (Health=0). Walls can cross roads.

### Objectives

Objective type and data stored on GameMap. Elimination: no timeout, ends when one faction has no living entities. Capture: Commander presence on target stronghold, tug-of-war counter (+1/-1 per tick per faction), 300 ticks hold to win. Survival: per-map timer, player wins by eliminating all AI before timer expires. AI fights normally on Survival maps.

### Tick pipeline

13 systems at 10 Hz (changed from 5 Hz), ordered by priority:
1. TerrainSystem
2. CommanderSystem (tactical orders)
3. RecruitmentSystem
4. MovementSystem
5. SpatialHashUpdate
6. CombatSystem
7. DeathSystem
8. LevelingSystem
9. FogSystem
10. ObjectiveSystem
11. AISystem
12. SnapshotSystem

ProjectileSystem removed. Instant damage only.

### Client

WebGL colored shapes. Unit type determines shape (circle/square/triangle) and color (green/orange/blue/red). Commander has white border. HUD shows Gold counter. Keyboard shortcuts: R=Recruit, G=AttackGround, 1-4=TacticalOrders. Click-based input: left click select, right click context action.

### Deployment

Single server for v1. Token auth, no login screen.

## Testing Decisions

### What makes a good test

Tests should verify external behavior, not implementation details. For pure functions (Damage Matrix, CombatUnitType table, Leveling thresholds), test the input/output mapping directly. For ECS systems, test the observable state changes after ticking — entity health values, kill point totals, dead entity counts, level values. For persistence, test the CRUD contract — write a roster, read it back, verify round-trip fidelity.

### Modules to test

All modules will have tests:

1. **CombatUnitType definitions** — verify all 7 types have valid stats, bounty = 80% of recruit cost, all weapon/armor combinations are valid
2. **Damage Matrix** — verify all 12 weapon×armor combinations, verify CanDamageTerrain flags, verify 150% bonuses for Sniper→Light and Missile→Heavy
3. **LevelingSystem** — verify kill point thresholds for CombatUnit (max 6) and Commander (max 10), verify stat scaling (+10% per level for CombatUnits, multiplier table for Commanders), verify immediate level-up during combat
4. **RecruitmentSystem** — verify Gold balance enforcement, Formation Template slot limits, Leading Skill budget enforcement (including Commander's own cost), unit spawning at Commander position with correct type and movement profile
5. **ObjectiveSystem** — verify Elimination end condition, Capture tug-of-war counter and hold target, Survival timer and AI win condition
6. **Persistence layer** — verify FindOrCreatePlayer idempotency, LoadRoster round-trip, SaveCommander JSONB serialization, DeleteCommander cascade, CreateStarterRoster composition, periodic flush correctness
7. **CombatSystem** — verify damage calculation with matrix, verify splash radius and falloff, verify smart targeting priority (150% > 100% > 50% > 25%), verify Gold bounty award, verify kill point award
8. **DeathSystem** — verify entity removal, verify Commander promotion (highest-level CombatUnit promotes), verify promoted unit keeps its type, verify death logging for persistence
9. **Session/Match lifecycle** — verify roster loading into ECS entities, verify 30-second periodic flush, verify final flush on match end, verify starter roster creation on full roster destruction
10. **Network protocol** — verify snapshot encoding with new fields (unit type, level, Gold, objective state), verify backward compatibility with changed fields
11. **Client rendering** — verify unit type maps to correct shape/color, verify Commander white border, verify HUD Gold counter updates

### Prior art

Existing test patterns in the codebase use Go's standard `testing` package with table-driven tests. See `server/pkg/combat/combat_test.go`, `server/pkg/boid/forces_test.go`, `server/pkg/fog/fog_test.go`, `server/pkg/movement/speed_test.go` for established patterns. New tests should follow the same conventions.

## Out of Scope

- Multiplayer (PvP) — v1 is Player vs AI only
- Sound effects or audio
- Sprites or animated graphics — colored shapes only
- Morale system — cut from v1
- Formation switching (Line/Wedge/Circle/Scatter) — default loose cluster only
- Prediction-based client reconciliation — interpolation only
- Multiple Squads per player — one Squad per match in v1
- Map selection — server generates a random map with random objective
- Account system — token auth only, no login screen
- Matchmaking — single server, start match on button click
- Air units — deferred to post-v1
- Prediction or lag compensation — client interpolates only
- Server clustering or horizontal scaling — single server

## Further Notes

- CONTEXT.md contains the authoritative glossary with 26 domain terms. All code and PRDs should use this vocabulary exclusively.
- 5 ADRs exist in `docs/adr/` covering tick rate, permadeath, simplified movement, Commander Formation Template, and map generator two-profile design.
- The CombatUnitType system replaces the old melee/ranged role system entirely. The existing `RoleMelee`/`RoleRanged` and `AttackMelee`/`AttackRanged`/`AttackArtillery` enums should be replaced with the new WeaponType-based system.
- The existing `ProjectileComponent` and `ProjectileSystem` are removed — all damage is instant in v1.
- The `Morale` field on `HealthComponent` should be removed or ignored — morale is cut from v1.
- The implementation plan with 20 tasks across 9 phases is in `docs/plans/2026-05-19-v1-implementation.md`.
