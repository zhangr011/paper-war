# Stronghold Is a Building, Not a Terrain Type

**Status:** Accepted (2026-07-18)

Stronghold moves from a terrain classification (`TerrainStronghold1-5`, an indestructible neutral tile) to a **Building entity** — a capturable, garrisonable structure with HP. This retires the 5 terrain enums and relocates the stronghold's three current roles (tile defense bonus, AI waypoint, capture target) onto an entity.

## Key decisions

- **Stronghold ≠ Target.** The Capture objective's win point (`Objective.TargetX/Y`) is a separate concept. The code currently ties the target to a stronghold via `assignProceduralObjective`; that linkage is severed — strongholds are a map resource, not the win condition.
- **Capture by damage-to-flip.** A stronghold is neutral at match start. Reducing its HP to 0 flips it to the attacker's Faction and restores HP; repeatable. Building armor means only Cannon and Missile can flip a stronghold, giving siege units a dedicated job. The old occupation-hold Capture mechanic is retired for strongholds.
- **Garrison, not aura.** A faction moves CombatUnits into a stronghold up to a level-scaled Capacity. Garrisoned units receive buffs (defense + HP recovery), fire out with their own weapons, and are immune to *direct* targeting — enemies must damage the stronghold to reach them.
- **Level-scaled damage split.** Incoming damage to a garrisoned stronghold is split stronghold : garrison (≈70% : 30% at L1 → 90% : 10% at L5); the garrison's share divides evenly across garrisoned units. Garrisoned units can die from the split during a long siege. When the stronghold flips, surviving garrisoned units are evicted adjacent with their remaining HP.
- **Neutral strongholds must be flipped before they can be garrisoned** — an "opening siege" race each match.

## Why

The terrain representation couldn't express ownership, destructibility, capture, or occupancy — all things a control point wants. Reusing the existing Building/Structure machinery (`StructureComponent`, `HealthComponent`, `BuildSystem`) gives stronghold those for free and removes a confusing overlap between two "thing on the map" concepts.

## Consequences

- Multi-system migration: map generator (`placeStrongholds` spawns entities, not terrain), AI (`as.Strongholds` targets entities + new garrison/capture logic), combat (`StrongholdDefenseBonus` becomes a garrison buff; new damage-split), objective (sever target↔stronghold link), persistence (strongholds are match state, not map), client rendering (draw as a structure, not a terrain tile).
- The Clash Map editor (#53, ADR-0022) authors terrain only; stronghold placement becomes a separate position layer on the map (the deferred Spawns/Objective authoring follow-up now also covers stronghold positions).
- The 5 levels gain double meaning: Capacity and damage-split shelter both scale with level.

See issue #54 for the phased migration. Out of scope: the Target's own identity (it remains whatever `Objective.TargetX/Y` is today).
