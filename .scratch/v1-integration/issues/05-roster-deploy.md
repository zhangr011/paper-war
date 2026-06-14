Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

When a match starts, load the player's persistent Commander and CombatUnits from the Store (MockStore for now) into ECS entities, instead of hardcoding 1 Commander + 5 Light Infantry.

Currently SpawnTeam creates a generic Gun/Light Commander + N Light Infantry. This should instead:
1. Call `store.LoadRoster(playerID)` to get the player's Commanders.
2. For the selected Commander, spawn it with the correct type, level, HP, and stats from the roster data.
3. Spawn the Commander's attached CombatUnits with their persisted types, levels, and HP.
4. For a new player (no roster), fall back to `CreateStarterRoster` then deploy from that.

This connects the persistence layer (issue 06 from v1 plan) to the match spawn logic.

## Acceptance criteria

- [ ] SpawnTeam loads Commander data from Store instead of hardcoding
- [ ] Commander spawns with correct CombatUnitType, level, HP from roster
- [ ] CombatUnits spawn with their persisted types and levels
- [ ] New player gets starter roster via CreateStarterRoster
- [ ] PlayerGold initialized from roster data
- [ ] Test: load a roster with a Sniper commander + 2 HI, verify correct entities spawned

## Blocked by

- Issue 03 (Commander type selection)
- Issue 04 (Formation template budget — needed for roster validation)
