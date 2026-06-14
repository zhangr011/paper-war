# Map Generator Constraints for Two Movement Profiles

**Superseded by [ADR-0006 — Procedural Map Generation: Heightmap Pipeline](./0006-procedural-map-heightmap.md)** on 2026-06-13.

The heightmap-driven pipeline replaces the road/bridge/wall approach. Key changes:
- Road network removed (natural terrain provides corridors)
- Bridge guarantee changed from minimum 3 to 1-2 at narrowest river points
- Wall placement removed (ridge passes are natural chokepoints)
- Connectivity guarantee changed from road-connected spawns to BFS path exists
- All water is Deep (no Shallow fords)

See ADR-0006 for full rationale and pipeline design.

---

# Original ADR-0005

The map generator was originally designed for a single movement profile. With Heavy units being road-dependent (cost 2 on plain, 4 on difficult terrain, impassable on water/swamp), the generator needs new constraints to ensure fair gameplay. Three changes were made:

1. **Road-connected spawns** — every spawn area is guaranteed a road path to at least one bridge. Heavy Squads always have a viable route to the river.

2. **Minimum 3 bridges + shallow fords far from bridges** — bridges are the only crossing for Heavy (minimum 3, scaling with map width). Shallow water fords are placed far from bridges, giving Light Squads an alternative crossing at cost 2. This creates crossing asymmetry: Heavy depends on bridges (destroyable), Light has more options.

3. **Walls can cross roads** — walls are allowed to block road corridors, creating strategic chokepoints that only Cannon can breach. Both sides face the same walls (symmetric map), so it's fair.

4. **Strongholds are indestructible** — they define the map's strategic layout and serve as objective targets. Only bridges and walls are destructible.

Considered options:
- Sparse roads with no connectivity guarantee — rejected: Heavy Squads could be trapped in spawn area
- 2 bridges only — rejected: too easy to lock down Heavy with bridge destruction
- Indestructible objective strongholds, destructible others — rejected: adds complexity for no v1 gameplay benefit
