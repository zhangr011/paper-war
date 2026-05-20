Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

When a CombatUnit dies, the DeathSystem awards a Gold bounty to the killer's Commander. The server sends a MsgGoldUpdate (0x80) to the client with the new gold total. Gold = 80% of RecruitCost (pre-computed in CombatUnitTypeTable[].KillBounty).

## Acceptance criteria

- [x] DeathSystem collects GoldBounties map[playerID]bounty each tick
- [x] Session.Tick() applies bounties to PlayerGold
- [x] Session.GetGoldUpdates() returns changed player→gold pairs (delta from last sent)
- [x] Client already handles MsgGoldUpdate (issue 15)
- [x] All existing tests pass

## Blocked by

None.
