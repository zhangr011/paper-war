Title: Combat units collapse to commander position — FormationRoleComponent offsets always zero
Status: ready-for-agent
Labels: bug
Priority: P1
Area: server/movement, server/game

## Bug Description

All combat units in a squad converge to the exact same tile as the commander within a few ticks, forming a single overlapping blob instead of maintaining a spread formation.

## Root Cause

In `session.go:spawnCombatUnitsWithType()` (line 851), combat units are given initial spawn positions that include grid offsets (`ox`, `oy`, lines 862-869), but the `FormationRoleComponent` is created with only the `Role` field set (line 923-925):

```go
gs.addComponent(unitEntity, component.FormationRoleComponent{
    Role: role,
})
```

`OffsetX` and `OffsetY` are Go zero-values (0, 0). The `MovementSystem` reads these offsets in `movement.go:134-138` to compute the attraction target:

```go
if fr, ok := s.formationRolePool.Get(e); ok {
    target[0] += fr.OffsetX
    target[1] += fr.OffsetY
}
```

Since both are 0, every unit's attraction target = commander position. Separation force pushes them apart temporarily, but formation attraction dominates and squashes them back together every tick.

The commander's own FormationRoleComponent (line 497-499) also has no offsets, which is correct (commander IS the formation anchor).

## Fix

In `spawnCombatUnitsWithType()`, store the computed `ox`/`oy` in the FormationRoleComponent:

```go
gs.addComponent(unitEntity, component.FormationRoleComponent{
    Role:    role,
    OffsetX: ox,
    OffsetY: oy,
})
```

This makes the initial grid layout persistent — each unit maintains its offset relative to the commander as the squad moves.

## Files to Change

- `server/pkg/game/session.go` — `spawnCombatUnitsWithType()` line 923: add OffsetX/OffsetY
- `server/pkg/game/session.go` — any other spawn paths that create combat units with FormationRoleComponent

## Verification

1. Spawn a team, run 100 ticks, check that combat unit positions are spread around the commander (not all at the same coordinates)
2. Move the squad, verify units maintain formation offsets while following
3. Existing tests should still pass
