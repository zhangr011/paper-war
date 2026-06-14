Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

The RecruitmentSystem validates recruit requests against the Commander's Formation Template and Leading Skill cost budget. Currently `recruit.go` exists but may not fully enforce the budget cap.

The Formation Template defines how many of each CombatUnitType a Commander can field. The Leading Skill is the total cost budget (starts at 2 for starter, grows with kills). Each unit costs 1/2/4 slots (from CombatUnitTypeTable.Cost).

Example: Commander with Leading Skill 8 can field any combination totaling ≤ 8 cost points (e.g., 8 Light Infantry, or 4 Heavy Infantry, or 2 Motor Artillery).

Changes:
1. In `recruit.go` ValidateSquad: count current squad size by cost (sum of CombatUnitTypeTable[unitType].Cost for each existing unit). Reject if current cost + new unit cost > Commander's Leading Skill.
2. The Commander's Leading Skill is stored on UnitTypeComponent (or a new field). For v1, Leading Skill starts at `5 + (commanderLevel - 1)` or a constant. Starter roster uses Leading Skill = 7 (1 cmd cost-1 + 5 LI cost-1 = 6, budget allows 7).
3. On recruit success: deduct Gold, spawn unit at Commander position.

## Acceptance criteria

- [ ] Recruit validates Gold >= RecruitCost before spawning
- [ ] Recruit validates total squad cost (existing + new) <= Leading Skill budget
- [ ] Recruit spawns the correct CombatUnitType at Commander position
- [ ] Recruit deducts Gold from PlayerGold
- [ ] Cannot recruit when squad is at full budget
- [ ] Test: try recruiting when at budget cap — expect rejection
- [ ] Test: recruit LI (cost 1) succeeds when budget has room

## Blocked by

- Issue 03 (Commander type selection — Commander needs correct type/level for budget calculation)
