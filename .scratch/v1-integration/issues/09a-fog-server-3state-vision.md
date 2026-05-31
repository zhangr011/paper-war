Status: ready-for-agent

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

Rewrite the server-side fog of war system with 3-state visibility and multi-source vision.

### Current state

`pkg/fog/fog.go` is 77 lines: binary `[]uint8` (0=fogged, 1=visible), `Clear()` zeros everything, `Reveal(cx,cy)` draws a 12-tile radius circle. `session.go:updateFog()` only iterates `CommanderComponent` entities for vision sources. Tests cover grid creation, reveal circle, clear, and bounds.

### Changes required

**1. 3-state FogGrid**

Replace the binary `Visible []uint8` with 3 states:

```
const (
    FogUnexplored = 0  // never seen — fully black on client
    FogExplored   = 1  // previously seen, not currently in vision — dimmed
    FogVisible    = 2  // currently in vision — full detail
)
```

**2. Preserve explored memory**

`Clear()` must downgrade `FogVisible → FogExplored` but never reset `FogExplored → FogUnexplored`. Currently `Clear()` zeros everything, losing all memory of seen areas.

```go
func (fg *FogGrid) Clear() {
    for i, v := range fg.Visible {
        if v == FogVisible {
            fg.Visible[i] = FogExplored
        }
    }
}
```

**3. Combat units contribute vision**

`updateFog()` currently only reads `CommanderComponent` entities. Add `BoidComponent` entities (squad members) as secondary vision sources with a shorter radius:

- Commander vision: 12 tiles (existing `VisionRadiusTiles`)
- Combat unit vision: 6 tiles (new constant `UnitVisionRadiusTiles = 6`)

Implementation: after collecting commander positions, also iterate `BoidComponent` where `Role != RoleCommander` and `HP > 0`, collecting their tile positions per owner. Then reveal around all sources.

**4. Single-pass update (no clear-then-reveal gap)**

Current flow: `Clear()` all grids → `Reveal()` around commanders. If all commanders are dead, grid stays fully black for one tick.

New flow:
1. Build a "newly visible" set per player (from all vision sources)
2. For each player grid: downgrade `FogVisible → FogExplored`, then set newly visible tiles to `FogVisible`

This avoids the fully-black frame edge case.

### Acceptance criteria

- [ ] `FogGrid.Visible` uses 3 values: 0 (unexplored), 1 (explored), 2 (visible)
- [ ] `Clear()` only downgrades visible→explored, never loses explored memory
- [ ] Combat units with HP > 0 contribute vision at 6-tile radius
- [ ] No fully-black frames when all commanders are dead (if combat units survive, they see)
- [ ] `IsVisible()` returns true for both `FogVisible` and `FogExplored` (client needs to know both)
- [ ] New method `IsCurrentlyVisible(tx, ty)` returns true only for `FogVisible` state
- [ ] `Data()` returns the raw grid for network transmission (client will handle 3-state rendering)
- [ ] Existing tests updated for 3-state semantics (Clear behavior changes)
- [ ] New tests: explored memory preserved across clear/reveal cycles, unit vision sources, edge case of all-commanders-dead, single-pass correctness

### Files to modify

- `server/pkg/fog/fog.go` — 3-state constants, Clear downgrade, Reveal unchanged, IsCurrentlyVisible method
- `server/pkg/fog/fog_test.go` — update existing tests, add new test cases
- `server/pkg/game/session.go` — `updateFog()` rewrite: collect unit vision sources, single-pass grid update

### Out of scope

- Terrain vision blocking (walls, forests) — separate follow-up
- Fog-aware AI — v1 AI has full vision by design
- Client rendering of 3 states — Issue 09-B
- Bandwidth optimization (RLE) — v1 doesn't need it (46 KB/s is fine for PvAI)
- CombatSystem fog checks — server-authoritative combat doesn't need fog gating
