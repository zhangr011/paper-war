Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

Players issue AttackGround commands. Server sets GroundTargetX/Y on AttackComponent. CombatSystem fires at ground position when no entity target and weapon is Cannon/Missile — deals splash damage to all units in area.

## Acceptance criteria

- [x] AttackComponent has GroundTargetX/Y fields
- [x] handleAttackGround sets GroundTargetX/Y on all squad units
- [x] CombatSystem fires ground attack when TargetID==0 and ground target set
- [x] Only Cannon/Missile weapons fire ground attacks
- [x] Ground attack uses applySplash at target position
- [x] All tests pass

## Blocked by

None.
