# 0007 — Movement Profile Assignment by Armor Type

Date: 2026-06-13

## Context

The server defines two movement profiles (Light and Heavy) in `component/profiles.go` with different terrain costs. Heavy units pay more for Forest (3 vs 2), Hill (4 vs 3), Swamp (4 vs 3), and cannot cross Shallow water at all (0 vs 2). The mapping function `ArmorTypeToProfileID()` exists but is never called.

All units currently receive `ProfileID: 0` (Light) at spawn time, regardless of their `ArmorType`. This means Motorized units (MotorGun, MotorArtillery, MotorMissile) with `ArmorHeavy` move as Light units — they cross Shallow water freely and pay lower terrain costs.

The session also registers only one profile with the TerrainSystem and MovementSystem, so even if a unit had `ProfileID: 1`, the terrain cost lookup would fail (profile not in the map).

## Decision

Assign `ProfileID` based on `ArmorType` at spawn time using the existing `ArmorTypeToProfileID()` function. Register both Light and Heavy profiles with TerrainSystem and MovementSystem during session initialization.

### Changes

1. **session.go init**: Replace the single hardcoded Light profile with `StandardMovementProfiles()` from `component/profiles.go`. Register both profiles with TerrainSystem and MovementSystem.

2. **session.go SpawnTeamWithType** (line 570): Commander gets `ProfileID: component.ArmorTypeToProfileID(cmdStats.Armor)`.

3. **session.go SpawnTeamFromRoster** (line 670): Same for roster commanders.

4. **session.go spawnCombatUnitsWithType** (line 759): CombatUnit gets `ProfileID: component.ArmorTypeToProfileID(cuStats.Armor)`.

5. **session.go spawnSingleUnit** (line 1026): Recruit unit gets `ProfileID: component.ArmorTypeToProfileID(stats.Armor)`.

### Gameplay Impact

- Motorized units (MG, MA, MM) now correctly move as Heavy: slower through Forest/Hill/Swamp, cannot cross Shallow water.
- Infantry units (LI, HI, Sniper, AAI) remain Light: faster terrain traversal, can ford Shallow water.
- Commanders use their type's armor for profile assignment.

## Consequences

- Maps with Shallow water (clash maps) now have genuine pathfinding differences between infantry and motorized units.
- The procedural campaign map (ADR-0006) uses only Deep water (no Shallow), so Heavy units are not additionally restricted there.
- Existing tests that spawn units and verify movement may need profile updates if they depend on specific terrain costs.
