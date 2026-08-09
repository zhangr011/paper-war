# Paper War

Server-authoritative large-scale multiplayer RTS. Players command Squads of units across a tile-based map with Boid flocking movement and Flow Field pathfinding.

## Language

**Tick**:
One simulation step on the server. Runs at 10 Hz (every 100ms).
_Avoid_: frame, update, step

**Squad**:
A group of units led by one Commander plus N CombatUnits. The basic unit of player command. CombatUnit types are determined by the Commander's Formation Template — newly recruited units fill specific template slots. Each CombatUnit keeps its type permanently, even through Commander promotion events.
_Avoid_: team, group, party

**Commander**:
The lead entity in a Squad. A special CombatUnit with a Damage Type and Armor Type. Has a Formation Template that defines the mix of CombatUnit types the Squad can recruit (e.g., all Gun/Light, or 50% Gun + 50% Cannon). Acts as formation center point, provides morale aura, and anchors vision (fog of war). Player orders route through the Commander. Can be attacked and killed like any CombatUnit. Persistent across matches with career-long Leading Skill progression. Auto-recenters within its Squad: when the Commander drifts >0.5 tile from the Squad centroid, CombatUnit flow is suppressed until it returns within 0.2 tile (ADR-0025).
_Avoid_: leader, hero, officer

**Formation Template**:
A Commander's recruitment blueprint. Defines slots per CombatUnitType (e.g., 8 Light Infantry + 2 Sniper). Each type has a cost (1/2/4). Slots scale proportionally with Leading Skill, which is the total cost budget. Example: a Mixed Commander with 4 Light Infantry (cost 1) + 3 Motor Artillery (cost 4) at Leading Skill 16 fields 4×1 + 3×4 = 16 pts. Different Commanders have different templates.
_Avoid_: composition, loadout, build

**CombatUnit**:
A non-Commander entity in a Squad. Has a CombatUnitType that determines weapon, armor, stats, cost, and collision radius. Types are permanent — never convert. The total cost of fielded units cannot exceed the Commander's Leading Skill. Has a Level (max 6) that grows through kill points (exponential: 2, 4, 8, 16, 32, 64 cumulative). Persistent across matches — death is permanent (Permadeath).
_Avoid_: soldier, unit (ambiguous), troop

**Collision**:
Friendly CombatUnits are physical bodies that do not overlap — overlapping units are pushed apart each tick by a positional correction (not a repulsion force). Garrisoned units are excluded (they stack inside a Stronghold by design); attack-frozen units are immovable obstacles. Enemy units do not collide — combat is ranged. See ADR-0030.
_Avoid_: separation (the removed force-based version), repulsion, physics body

**CombatUnitType**:
A named unit archetype that bundles weapon, armor, base stats, movement profile, and cost. Defines the Formation Template slots. v1 types: Light Infantry (Gun/Light/cost 1), Heavy Infantry (Cannon/Light/cost 2), Sniper (Sniper/Light/cost 1), Anti-Armor Infantry (Missile/Light/cost 2), Motor Gun (Gun/Heavy/cost 2), Motor Artillery (Cannon/Heavy/cost 4), Motor Missile (Missile/Heavy/cost 4). Type definitions live in code, not in the database.
_Avoid_: class, archetype

**Damage Type**:
The weapon category of a unit. Determines effectiveness against Armor Types. v1 has four types: Gun, Cannon, Sniper, Missile. Each has a distinct role in the Damage Matrix.
_Avoid_: weapon type, attack type

**Unit Cost**:
The Leading Skill cost of a unit in the Squad. Varies by CombatUnitType: 1 (Light Infantry, Sniper), 2 (Heavy Infantry, Anti-Armor Infantry, Motor Gun), 4 (Motor Artillery, Motor Missile). The Commander's own cost also counts toward the budget.
_Avoid_: supply, population

**Armor Type**:
The defense category of a unit. Determines vulnerability to Damage Types. v1 has three types: Light (vulnerable to Sniper, resistant to Missile), Heavy (vulnerable to Missile, resistant to Sniper), Building (only damaged by Cannon and Missile at 25%).
_Avoid_: defense type, unit class

**Damage Matrix**:
The lookup table for Weapon vs Armor Type. v1 is 4×3: Gun (100%/50%/0%), Cannon (50%/100%/25%), Sniper (150%/25%/0%), Missile (25%/150%/25%). Armor types: Light, Heavy, Building. Each weapon has a clear counter-profile: Sniper devastates Light, Missile shreds Heavy, Cannon and Missile are the only siege weapons (25% to Building).
_Avoid_: weapon table
**Splash**:
Cannon-type attacks deal area damage in a 2-tile radius. Full damage at impact point, 50% at 1 tile, 25% at 2 tiles. Only Cannon has splash. Gun, Sniper, and Missile are single-target only.
_Avoid_: AoE, explosion

**AttackGround**:
A player command that targets a terrain tile instead of a unit. Terrain tiles have Building armor. Cannon and Missile units deal 25% of base damage to Building armor via the Damage Matrix. Gun and Sniper deal 0% — cannot damage terrain. Used to open new paths or cut off enemy routes.
_Avoid_: attack terrain, siege

**Range Tolerance**:
The extra distance (1 tile) a CombatUnit may fire beyond its nominal Range when a nearby squadmate is already engaging — a spotter. Proximity-gated: the squadmate must be same-Squad and within SpotterRadius (~2 tiles), so a unit surged out of formation gets no benefit. The unit still picks its own target; the spotter only unlocks the overshoot. Keeps a mixed Squad firing together at contact instead of stringing out. Followers open fire with a short stagger (1-2 ticks) after their spotter — gated by the spotter's engagement tenure and varied per follower — so the Squad ripples into the fight rather than volleying at once. See ADR-0031.
_Avoid_: range bonus, extended range, shared range

**Leading Skill**:
A Commander attribute that determines the maximum number of CombatUnits in that Commander's Squad. Starts at 2, grows through combat events (kill thresholds) both within and across matches. Maximum 50. This is a career-long persistent stat — losing a Commander to Permadeath destroys months of Leading Skill progression.
_Avoid_: talent, capacity, leadership

**Kill Point**:
XP earned by a CombatUnit for each enemy killed. Accumulates to level up (2 pts for level 1, 4 for level 2, 8 for level 3, etc.). Persistent across matches.
_Avoid_: experience, XP

**Gold**:
The mid-game resource, earned by killing enemy units. Kill bounties = 80% of the killed unit's recruit cost (rounded). Commander bounties = max(100, 50 × Commander unit cost). Spent to recruit new CombatUnits at the Commander's position.
_Avoid_: coins, money, currency

**Recruit**:
Spending Gold to add a new CombatUnit to a Squad, up to the Commander's Leading Skill cap. New units spawn at the Commander's position. Recruited units join the player's persistent roster.
_Avoid_: spawn, buy, train

**Roster**:
A player's persistent collection of Commanders and their attached CombatUnits. Each Commander owns a separate pool of CombatUnits — switching Commanders switches the entire Squad. Survives between matches. CombatUnits that die in combat are permanently lost from the roster. When a Commander dies, all its attached CombatUnits are also lost (CASCADE). If the player's entire roster is destroyed, a new starter roster is granted immediately (1 Gun/Light Commander + 5 Light Infantry).
_Avoid_: army, collection, inventory

**Deploy**:
Selecting a Commander from the Roster to lead a Squad in a match. Auto-deploys the highest-level CombatUnits from that Commander's pool, up to the Leading Skill cap. For multi-Squad maps, the player deploys multiple Commanders.
_Avoid_: field, send, draft

**Permadeath**:
CombatUnits and Commanders that die in a match are permanently removed from the player's Roster. There is no respawn or recovery.
_Avoid_: death, elimination

**Commander Stats**:
A Commander's combat stats scale aggressively with level (max 10): HP multiplier ranges from 2x at level 1 to 5x at level 10, Damage multiplier from 1.5x to 3x. A level 10 Commander has survived dozens of battles and is exponentially more powerful — losing one to Permadeath is devastating because months of Leading Skill and stat progression are destroyed.
_Avoid_: hero stats

**Player**:
A human or AI connected to a game session. Owns one or more Squads identified by PlayerID.
_Avoid_: user, client (means the browser)

**Faction**:
A side in the conflict (Player or Enemy). Determines ally/enemy relationships.
_Avoid_: side, team

**Map**:
The board a match is played on: a 2D grid of Tiles with Spawn positions and an Objective. Produced either procedurally (Match Map) or by hand (Clash Map). The two variants share the same structure but differ in size, features, and how they are used — see ADR-0022.
_Avoid_: board, level, scenario

**Clash Map**:
A Map used by the spectator/balance harness. Terrain-only in live use — its Spawns and Objective are overridden at runtime. Hand-authored as Go source.
_Avoid_: test map, arena

**Match Map**:
A Map used by solo and PvP queue. Full features: procedural terrain, elevation, Spawns, and an Objective (Elimination/Capture/Survival).
_Avoid_: real map, game map

**Tile**:
One cell in a Map. Has a Terrain Type and, for hills, an Elevation band. Some tiles carry destructible health.
_Avoid_: cell, grid square

**Terrain Type**:
The classification of a Tile — Plain, Road, Shallow, Deep, Forest, Hill, Swamp, Bridge, Wall, Snow, Desert. Determines movement cost per Movement Profile and, for some, destructibility. (Strongholds were once terrain types but are now Buildings — see Stronghold.)
_Avoid_: tile type, ground

**Elevation**:
The discrete hill-layer band of a Hill Tile: low, mid, or peak. Visual only — affects rendering, not movement or combat.
_Avoid_: height, altitude

**Building**:
A structure entity placed on a Map with HP and an owning Faction (or neutral). Two kinds: player-built defensive structures (Watchtower, Barricade, Turret, placed via the build system for gold) and Strongholds (pre-placed, capturable). Buildings have Building armor — only Cannon and Missile damage them.
_Avoid_: structure, turret

**Stronghold**:
A capturable, garrisonable Building that grants buffs to the units inside it. Neutral at match start; a Faction claims it by reducing its HP to zero (it then flips to that Faction and restores HP). Garrisoned CombatUnits fire out and share incoming damage with the Stronghold by a level-scaled split. A distinct concept from a Target.
_Avoid_: fortress, base, objective

**Garrison**:
The CombatUnits inside a Stronghold, up to its Capacity. They receive the Stronghold's buffs (defense, recovery), fire out at enemies, and absorb a share of damage dealt to the Stronghold.
_Avoid_: occupants, defenders

**Capacity**:
The maximum number of CombatUnits a Stronghold's Garrison can hold. Scales with the Stronghold's level.
_Avoid_: slots, space

**Target**:
The Capture objective's win point — the location a Faction must control to win a Capture match. A distinct concept from a Stronghold, which is a capturable resource but not itself the win condition.
_Avoid_: stronghold (when you mean the objective point)

**Objective**:
The win condition for a match, determined by the map. Types: Elimination (wipe all enemies), Capture (hold a target stronghold for N ticks), Survival (player eliminates all AI defenders before time expires). In v1, the player is always the attacker and the AI is always the defender. Stored as an explicit field on GameMap with type-specific data.
_Avoid_: win condition, game mode

## Relationships

- A **Player** owns one **Roster**
- A **Roster** contains one or more **Commanders**
- Each **Commander** owns a separate pool of **CombatUnits**
- A **Squad** is formed by **Deploying** one **Commander** plus their **CombatUnits** (up to Leading Skill cap)
- A **Squad** contains exactly one **Commander** and zero or more **CombatUnits**
- Each **CombatUnit** has its own weapon and armor type (determined by its **CombatUnitType**), independent of the Commander
- A **Player** belongs to exactly one **Faction**
- A map's **Objective** determines the match win condition

## Example dialogue

> **Dev:** "When a **Commander** dies, what happens to its **Squad**?"
> **Domain expert:** "The highest-level **CombatUnit** promotes to **Commander**. It keeps its own **Damage Type** and **Armor Type**, which may shift the **Squad's** strategic role."

> **Dev:** "If I switch Commanders before a match, do I keep the same **CombatUnits**?"
> **Domain expert:** "No — each **Commander** has their own attached pool. Switching Commanders switches the entire **Squad**."

## Flagged ambiguities

- "unit" was used to mean both **CombatUnit** (an entity) and "a game unit" (generic) — resolved: **CombatUnit** is the specific term; "unit" is only used informally.
- "team" was used to mean both **Squad** and **Faction** — resolved: **Squad** is the command group, **Faction** is the side.
- "formation" was used to describe both Squad arrangement and formation types (Line/Wedge/Circle/Scatter) — resolved: v1 has no formation switching; the formation-slot system was removed. A Squad's arrangement is a compact cluster centered on the Commander, with friendly-unit spacing governed by Collision (ADR-0030), not slot geometry. "Formation" refers only to the informal spatial arrangement.
