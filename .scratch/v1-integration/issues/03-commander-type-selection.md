Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

Client sends `commander_type` with start_solo. Server reads it and spawns the Commander with the selected type (LI/HI/Sniper/AAI) instead of hardcoded LI. Commander stats are scaled from the CombatUnitTypeTable (3x HP, 2x dmg).

## Acceptance criteria

- [x] SpawnTeamWithType/SpawnSquadWithType accept cmdType parameter
- [x] Commander gets UnitTypeComponent with selected type
- [x] Commander HP/dmg/range/cooldown derived from CombatUnitTypeTable (3x HP, 2x dmg)
- [x] SpawnTeam() preserved as backward-compat wrapper (defaults to LI)
- [x] All existing tests pass

## Blocked by

None.
