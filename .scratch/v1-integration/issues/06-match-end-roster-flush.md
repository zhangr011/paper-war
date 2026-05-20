Status: ready-for-agent

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

When a match ends (ObjectiveSystem triggers), serialize surviving CombatUnits back to the Store. Dead units are permanently removed from the roster (Permadeath). If the player's entire roster is destroyed, immediately grant a new starter roster.

The lifecycle.go already has a 30-second flush timer. Wire it to:
1. Collect all living entities per player (via OwnerComponent).
2. Map each entity back to roster data (type, level, kill points, HP).
3. Call `store.SaveCommander` for each surviving Commander with updated unit list.
4. For each dead entity tracked in DeathLog, remove it from the Commander's CombatUnit list.
5. After flush, check if player has zero Commanders → call `CreateStarterRoster`.

On match end (lifecycle.End()):
1. Final flush with full roster state.
2. Send `MsgMatchResult` (0x81) to all players.

## Acceptance criteria

- [ ] Surviving units are saved to Store after match end
- [ ] Dead units are removed from roster (Permadeath)
- [ ] If all Commanders dead → starter roster granted automatically
- [ ] 30-second periodic flush during match persists mid-game state
- [ ] Match result message sent to all players
- [ ] Test: kill all player units, verify starter roster created
- [ ] Test: survive match with 3 units, verify roster has exactly 3

## Blocked by

- Issue 05 (Roster deploy — need roster loading first before flush makes sense)
