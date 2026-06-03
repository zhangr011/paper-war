---
ID: 04
Type: bug
Status: open
Severity: high
Component: movement
---

# Combat Units Move Backward Instead of Following Commander

## Observed Behavior

When a squad receives a move command (forward toward enemy), combat units move
backward (away from the target) instead of following their commander forward.

## Root Cause

**Two separate spawn paths set different BoidComponent weights:**

### Path A — `SpawnTeamFromRoster` (line 645-650, solo mode)
```
SeparationW:   1.5
FormationW:    0     ← NOT SET (Go zero value)
NeighborRange: 5.0
```
- No FormationRoleComponent
- No attraction to commander at all

### Path B — `spawnCombatUnitsWithType` (line 903-911, clash mode)
```
SeparationW:   1.5
FormationW:    2.0   ← SET
NeighborRange: 2.0
```
- Has FormationRoleComponent with OffsetX/OffsetY
- Properly follows commander

**Path A combat units have FormationW=0**, meaning the movement equation:

```
totalFX = flowFX + sepFX*1.5 + attrFX*0
                              ^^^^^^^^^^
                              zeroed out!
```

When the commander moves ahead of the squad, the separation force from the
commander (who is now in front) pushes combat units backward. Without the
attraction force to counterbalance, the net force is backward.

This is the same pattern as Bug 02 (commander steers wrong direction due to
same-squad separation), but affecting combat units instead.

## Fix

Spawn path A needs the same FormationW and FormationRoleComponent as path B:

1. `session.go` line ~645: add `FormationW`, `CohesionW`, `AlignmentW` to BoidComponent
2. `session.go` line ~625-678: compute formation grid (cols/rows/offsets) and add
   FormationRoleComponent with OffsetX/OffsetY — same logic as
   `spawnCombatUnitsWithType` lines 875-882

### Recommended BoidComponent values (match path B):
```go
BoidComponent{
    SquadID:       squadID,
    Role:          role,
    SeparationW:   fixed.FromFloat(1.5),
    CohesionW:     fixed.FromFloat(0.8),
    AlignmentW:    fixed.FromFloat(1.0),
    FormationW:    fixed.FromFloat(2.0),
    NeighborRange: fixed.FromFloat(2.0),
}
```

### Formation grid (same as spawnCombatUnitsWithType):
```go
cols := 1
for cols*cols < len(cmd.Units) { cols++ }
row := i / cols
col := i % cols
spacing := fixed.FromFloat(0.6)
ox := int64(col-(cols-1)/2) * spacing
oy := int64(row+1) * spacing
```

## Files

- `server/pkg/game/session.go` — SpawnTeamFromRoster (~line 625-678)
- `server/pkg/movement/movement.go` — force computation
- `server/pkg/boid/forces.go` — AttractionForce, SeparationForce
- `server/pkg/component/boid.go` — FormationW field

## Verification

1. Spawn a solo match (roster path)
2. Move squad forward via click command
3. Confirm combat units follow commander in formation (not backward)
4. Run `go test ./...` — all pass
