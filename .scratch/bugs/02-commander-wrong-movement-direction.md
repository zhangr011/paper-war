Title: Commander steers in wrong direction from pathfinding target
Status: done
Labels: bug
Priority: P2
Area: server/movement

## Bug Description

When a move command is issued (target at y=20, commander at y=10), the commander moves slightly **away** from the target (y goes from 10.00 to 9.99) instead of toward it. After 200 ticks it has barely moved (distance 10.000 → 10.007).

## Reproduction

Spawn a team at (10,10), issue move command to (10,20). After 1 tick the commander is at (10.01, 9.99) — Y decreased instead of increasing toward 20.

Debug output from `TestDebugCommanderMovement`:
```
BEFORE: entity 1 role=3 pos=(10.00,10.00) path_target=(0.00,0.00)
AFTER handleMoveSquad: entity 1 role=3 pos=(10.00,10.00) path_target=(10.00,20.00)
AFTER 1 tick: entity 1 role=3 pos=(10.01,9.99)   ← WRONG: Y should increase
```

## Likely Cause

The flow field cache (`pathfinding.Cache`) computes a direction vector per tile. Possible issues:
1. Flow field may use a coordinate system where Y increases upward while the game uses Y-down (or vice versa), causing reversed Y steering
2. The tile lookup `tileX := int32(pos.X >> 12)` may be off — the commander at y=10.00 maps to tileY=0, which could be outside the cached flow field or map to an edge case
3. The flow field may not be cached for the target (10,20) yet on tick 1, returning a zero/default direction

## Files to Investigate

- `server/pkg/movement/movement.go` — `Tick()` lines 89-119: flow field force calculation
- `server/pkg/pathfinding/cache.go` — `Get()` and `GetDirection()`: how direction vectors are computed
- `server/pkg/pathfinding/flowfield.go` — flow field generation, coordinate conventions

## Verification

1. Spawn team at (10,10), move to (10,20), tick once, assert commander Y increased
2. Same test with (10,10) → (20,10), assert X increased
3. Long-run test: after 200 ticks, commander should be significantly closer to target
