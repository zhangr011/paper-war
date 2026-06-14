Status: ready-for-human

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

Manual end-to-end playtest to verify the full game loop works: login → select Commander → start match → move units → recruit new units → attack enemy → win/lose → verify Gold updates → verify roster persistence.

This is a HITL (human-in-the-loop) issue — requires running the server, opening a browser, and playing through scenarios.

Scenarios to verify:
1. Login with username → lobby appears with roster
2. Select Commander type → Start Match → correct Commander spawns
3. Select squad → right-click to move → units move
4. Kill enemy unit → Gold counter increases by bounty amount
5. Click recruit button (if enough Gold) → new unit spawns at Commander
6. Press A + right-click (Attack Ground) near wall → wall takes damage
7. Win match (kill all enemies) → Victory overlay appears
8. Start new match → roster reflects surviving units from previous match
9. Lose all units → starter roster granted

## Acceptance criteria

- [ ] Full game loop playable from login to match end
- [ ] Gold counter updates correctly during combat
- [ ] Recruit works with correct Gold deduction
- [ ] AttackGround damages terrain
- [ ] Match result overlay appears
- [ ] Roster persists between matches (survivors carry over)
- [ ] Permadeath confirmed (dead units gone from roster)

## Blocked by

- Issue 01 (Gold bounty + GoldUpdate)
- Issue 02 (AttackGround terrain damage)
- Issue 03 (Commander type selection)
- Issue 05 (Roster deploy)
