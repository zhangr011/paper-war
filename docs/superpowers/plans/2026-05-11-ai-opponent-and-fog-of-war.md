# AI Opponent & Fog of War — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a simple reactive AI opponent and commander-centered fog of war to Paper War solo mode.

**Architecture:** Server-side `AISystem` controls enemy squads with scan/approach/attack/retreat/patrol states. Server-side `FogSystem` computes per-player visibility grids. Snapshot generation filters enemy units by fog visibility. Client renders fog overlay.

**Tech Stack:** Go (server), JavaScript/WebGL (client)

**Spec reference:** `docs/superpowers/specs/2026-05-11-ai-opponent-and-fog-of-war-design.md`

---

## File Structure

```
server/pkg/
  ai/
    ai.go            # AISystem, AIState, decision loop
    ai_test.go       # Unit tests for AI decisions
  fog/
    fog.go           # FogGrid, FogSystem
    fog_test.go      # Unit tests for fog visibility
  game/
    session.go       # Add AISystem + FogSystem to tick pipeline
  network/
    snapshot.go      # Filter enemy units by fog visibility
client/
  src/
    state.js         # Parse fog data from snapshots
    main.js          # Fog overlay rendering
    gl.js            # Fog render pass
```

---

### Task 1: FogGrid & FogSystem

**Files:**
- Create: `server/pkg/fog/fog.go`
- Create: `server/pkg/fog/fog_test.go`

- [ ] **Step 1: Write the FogGrid and FogSystem**

```go
// server/pkg/fog/fog.go
package fog

const VisionRadiusTiles = 12

type FogGrid struct {
    Width, Height int32
    Visible       []uint8 // 0=fogged, 1=visible
}

func NewFogGrid(w, h int32) *FogGrid {
    return &FogGrid{
        Width:   w,
        Height:  h,
        Visible: make([]uint8, w*h),
    }
}

// Clear resets all tiles to fogged.
func (fg *FogGrid) Clear() {
    for i := range fg.Visible {
        fg.Visible[i] = 0
    }
}

// Reveal marks tiles within VisionRadiusTiles of (cx, cy) as visible.
// cx, cy are tile coordinates (not fixed-point).
func (fg *FogGrid) Reveal(cx, cy int32) {
    r := int32(VisionRadiusTiles)
    for dy := -r; dy <= r; dy++ {
        for dx := -r; dx <= r; dx++ {
            // Circular check
            if dx*dx+dy*dy > r*r {
                continue
            }
            tx, ty := cx+dx, cy+dy
            if tx >= 0 && tx < fg.Width && ty >= 0 && ty < fg.Height {
                fg.Visible[ty*fg.Width+tx] = 1
            }
        }
    }
}

// IsVisible returns true if the tile at (tx, ty) is visible.
func (fg *FogGrid) IsVisible(tx, ty int32) bool {
    if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
        return false
    }
    return fg.Visible[ty*fg.Width+tx] == 1
}

// FogSystem computes per-player visibility grids.
type FogSystem struct {
    Grids map[uint32]*FogGrid // key = playerID
    MapW, MapH int32
}

func NewFogSystem(mapW, mapH int32) *FogSystem {
    return &FogSystem{
        Grids: make(map[uint32]*FogGrid),
        MapW:  mapW,
        MapH:  mapH,
    }
}

// Update recomputes visibility for all players.
// cmdPool is the CommanderComponent pool, posPool is the PositionComponent pool,
// ownerPool is the OwnerComponent pool, healthPool is the HealthComponent pool.
func (fs *FogSystem) Update(
    cmdPool interface{ Each(func(interface{})) },
    posPool interface{ Each(func(interface{})) },
    ownerPool interface{ Each(func(interface{})) },
    healthPool interface{ Each(func(interface{})) },
    getEntityOwner func(uint32) uint32,
    getEntityPos func(uint32) (int64, int64),
    getEntityHealth func(uint32) (int32, int32),
) {
    // Collect alive commander positions per player
    type cmdInfo struct { playerID uint32; tx, ty int32 }
    var commanders []cmdInfo

    // Iterate commanders, check alive via health, get position and owner
    // This will be typed properly using the actual ECS pool types

    // For each player, create/clear grid, reveal around each commander
    for _, cmd := range commanders {
        grid, ok := fs.Grids[cmd.playerID]
        if !ok {
            grid = NewFogGrid(fs.MapW, fs.MapH)
            fs.Grids[cmd.playerID] = grid
        }
        grid.Reveal(cmd.tx, cmd.ty)
    }
}

// GetGrid returns the FogGrid for a player.
func (fs *FogSystem) GetGrid(playerID uint32) *FogGrid {
    return fs.Grids[playerID]
}
```

Note: The `Update` method signature uses interfaces for testability but the actual implementation will use the concrete ECS pool types from the codebase. Adjust to use the actual typed pool iteration pattern used in other systems (e.g., `*ecs.ComponentPool[component.CommanderComponent]`).

- [ ] **Step 2: Write FogGrid tests**

```go
// server/pkg/fog/fog_test.go
package fog

import "testing"

func TestNewFogGrid(t *testing.T) {
    fg := NewFogGrid(10, 10)
    if len(fg.Visible) != 100 {
        t.Errorf("expected 100 tiles, got %d", len(fg.Visible))
    }
}

func TestReveal(t *testing.T) {
    fg := NewFogGrid(64, 64)
    fg.Reveal(32, 32)
    // Center should be visible
    if !fg.IsVisible(32, 32) {
        t.Error("center should be visible")
    }
    // Tile within radius should be visible
    if !fg.IsVisible(38, 32) {
        t.Error("tile within radius should be visible")
    }
    // Tile outside radius should not be visible
    if fg.IsVisible(0, 0) {
        t.Error("far tile should not be visible")
    }
}

func TestClear(t *testing.T) {
    fg := NewFogGrid(10, 10)
    fg.Reveal(5, 5)
    fg.Clear()
    if fg.IsVisible(5, 5) {
        t.Error("tile should be fogged after clear")
    }
}

func TestIsVisibleBounds(t *testing.T) {
    fg := NewFogGrid(10, 10)
    if fg.IsVisible(-1, 0) {
        t.Error("negative x should be false")
    }
    if fg.IsVisible(0, -1) {
        t.Error("negative y should be false")
    }
    if fg.IsVisible(10, 0) {
        t.Error("x >= width should be false")
    }
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/zhangrong/repo/paper-war/server && go test ./pkg/fog/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add server/pkg/fog/
git commit -m "feat: add FogGrid and FogSystem for fog of war"
```

---

### Task 2: AISystem

**Files:**
- Create: `server/pkg/ai/ai.go`
- Create: `server/pkg/ai/ai_test.go`

- [ ] **Step 1: Write AISystem**

```go
// server/pkg/ai/ai.go
package ai

import (
    "math/rand"

    "github.com/user/paper-war/server/pkg/component"
    "github.com/user/paper-war/server/pkg/ecs"
    "github.com/user/paper-war/server/pkg/fixed"
    "github.com/user/paper-war/server/pkg/fog"
)

const (
    StateIdle    uint8 = 0
    StatePatrol  uint8 = 1
    StateApproach uint8 = 2
    StateAttack  uint8 = 3
    StateRetreat uint8 = 4

    EvalInterval    uint32 = 30 // ticks between re-evaluations (~6s at 5Hz)
    RetreatHPThreshold = 0.3   // 30% HP triggers retreat
)

type AIState struct {
    SquadID       uint32
    CommanderID   uint32
    State         uint8
    TargetSquadID uint32
    PatrolX       int64 // fixed-point
    PatrolY       int64
    NextEvalTick  uint32
}

type AISystem struct {
    States    map[uint32]*AIState // key = squadID
    AIPlayerID uint32
    FogSystem  *fog.FogSystem
    MapW, MapH int32
}

func NewAISystem(aiPlayerID uint32, fogSys *fog.FogSystem, mapW, mapH int32) *AISystem {
    return &AISystem{
        States:     make(map[uint32]*AIState),
        AIPlayerID: aiPlayerID,
        FogSystem:  fogSys,
        MapW:       mapW,
        MapH:       mapH,
    }
}

// RegisterSquad tracks an AI-controlled squad.
func (as *AISystem) RegisterSquad(squadID, commanderID uint32) {
    as.States[squadID] = &AIState{
        SquadID:     squadID,
        CommanderID: commanderID,
        State:       StateIdle,
    }
    as.pickPatrolTarget(as.States[squadID])
}

// Update runs the AI decision loop for all registered squads.
// Returns a list of commands to execute.
func (as *AISystem) Update(
    tick uint32,
    cmdPool *ecs.ComponentPool[component.CommanderComponent],
    posPool *ecs.ComponentPool[component.PositionComponent],
    ownerPool *ecs.ComponentPool[component.OwnerComponent],
    healthPool *ecs.ComponentPool[component.HealthComponent],
) []AICommand {
    var cmds []AICommand
    aiFog := as.FogSystem.GetGrid(as.AIPlayerID)

    for squadID, state := range as.States {
        // Check if commander is alive
        pos, hasPos := posPool.Get(state.CommanderID)
        health, hasHealth := healthPool.Get(state.CommandinerID)
        if !hasPos || !hasHealth {
            continue // commander dead or missing
        }

        // Emergency: low HP retreat bypasses cooldown
        if float64(health.HP)/float64(health.MaxHP) < RetreatHPThreshold && state.State != StateRetreat {
            state.State = StateRetreat
            // Retreat toward map edge
            retreatX := fixed.FromFloat(0)
            if fixed.ToInt(pos.X) > as.MapW/2 {
                retreatX = fixed.FromFloat(float64(as.MapW - 1))
            }
            cmds = append(cmds, AICommand{
                Type:     CmdMove,
                SquadID:  squadID,
                TargetX:  retreatX,
                TargetY:  pos.Y,
            })
            continue
        }

        // Cooldown check
        if tick < state.NextEvalTick {
            continue
        }
        state.NextEvalTick = tick + EvalInterval

        // Scan for nearest enemy commander within vision
        bestDist := int64(-1)
        bestEnemyID := uint32(0)
        bestEnemyX, bestEnemyY := int64(0), int64(0)

        cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
            owner, hasOwner := ownerPool.Get(uint32(e))
            if !hasOwner || owner.PlayerID == as.AIPlayerID {
                return // skip own units
            }
            ePos, hasPos := posPool.Get(uint32(e))
            if !hasPos {
                return
            }
            // Fog check: can AI see this enemy?
            if aiFog != nil {
                ex := int32(fixed.ToInt(ePos.X))
                ey := int32(fixed.ToInt(ePos.Y))
                if !aiFog.IsVisible(ex, ey) {
                    return // can't see, skip
                }
            }
            dx := ePos.X - pos.X
            dy := ePos.Y - pos.Y
            dist := dx*dx + dy*dy
            if bestDist < 0 || dist < bestDist {
                bestDist = dist
                bestEnemyID = uint32(e)
                bestEnemyX = ePos.X
                bestEnemyY = ePos.Y
            }
        })

        if bestEnemyID != 0 {
            state.TargetSquadID = bestEnemyID
            // Check if in attack range (~3 tiles)
            attackRange := fixed.FromFloat(3.0)
            if bestDist <= attackRange*attackRange {
                state.State = StateAttack
                cmds = append(cmds, AICommand{
                    Type:     CmdAttack,
                    SquadID:  squadID,
                    TargetID: bestEnemyID,
                })
            } else {
                state.State = StateApproach
                cmds = append(cmds, AICommand{
                    Type:    CmdMove,
                    SquadID: squadID,
                    TargetX: bestEnemyX,
                    TargetY: bestEnemyY,
                })
            }
        } else {
            // No enemy visible, patrol
            state.State = StatePatrol
            cmds = append(cmds, AICommand{
                Type:    CmdMove,
                SquadID: squadID,
                TargetX: state.PatrolX,
                TargetY: state.PatrolY,
            })
            // Check if near patrol target, pick new one
            dx := state.PatrolX - pos.X
            dy := state.PatrolY - pos.Y
            if dx*dx+dy*dy < fixed.FromFloat(4.0)*fixed.FromFloat(4.0) {
                as.pickPatrolTarget(state)
            }
        }
    }
    return cmds
}

func (as *AISystem) pickPatrolTarget(state *AIState) {
    margin := fixed.FromFloat(5.0)
    maxF := fixed.FromFloat(float64(as.MapW) - 5.0)
    maxFY := fixed.FromFloat(float64(as.MapH) - 5.0)
    state.PatrolX = margin + fixed.FromFloat(rand.Float64())*(maxF-margin*2)
    state.PatrolY = margin + fixed.FromFloat(rand.Float64())*(maxFY-margin*2)
}

const (
    CmdMove   uint8 = 1
    CmdAttack uint8 = 2
)

type AICommand struct {
    Type     uint8
    SquadID  uint32
    TargetX  int64
    TargetY  int64
    TargetID uint32
}
```

- [ ] **Step 2: Write AI tests**

```go
// server/pkg/ai/ai_test.go
package ai

import "testing"

func TestNewAISystem(t *testing.T) {
    sys := NewAISystem(2, nil, 64, 64)
    if sys.AIPlayerID != 2 {
        t.Error("expected AI player ID 2")
    }
}

func TestRegisterSquad(t *testing.T) {
    sys := NewAISystem(2, nil, 64, 64)
    sys.RegisterSquad(1, 100)
    state, ok := sys.States[1]
    if !ok {
        t.Fatal("squad 1 should be registered")
    }
    if state.CommanderID != 100 {
        t.Error("commander ID mismatch")
    }
    if state.State != StateIdle {
        t.Error("initial state should be Idle")
    }
    // Patrol target should be within map bounds
    if state.PatrolX == 0 && state.PatrolY == 0 {
        t.Error("patrol target should be set")
    }
}

func TestPickPatrolTarget(t *testing.T) {
    sys := NewAISystem(2, nil, 64, 64)
    state := &AIState{}
    sys.pickPatrolTarget(state)
    // Should be within map bounds (5 to 59)
    px := fixed.ToInt(state.PatrolX)
    py := fixed.ToInt(state.PatrolY)
    if px < 5 || px > 59 {
        t.Errorf("patrol X out of bounds: %d", px)
    }
    if py < 5 || py > 59 {
        t.Errorf("patrol Y out of bounds: %d", py)
    }
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/zhangrong/repo/paper-war/server && go test ./pkg/ai/ -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add server/pkg/ai/
git commit -m "feat: add AISystem for reactive AI opponent"
```

---

### Task 3: Integrate FogSystem & AISystem into GameSession

**Files:**
- Modify: `server/pkg/game/session.go`

- [ ] **Step 1: Add imports and fields to GameSession**

Add to imports:
```go
"github.com/user/paper-war/server/pkg/ai"
"github.com/user/paper-war/server/pkg/fog"
```

Add to `GameSession` struct:
```go
FogSys   *fog.FogSystem
AISys    *ai.AISystem
```

- [ ] **Step 2: Initialize FogSystem and AISystem in NewGameSession**

After existing system initialization (after `projPool` and component registrations), add:

```go
// Fog system (64x64 map)
gs.FogSys = fog.NewFogSystem(64, 64)

// AI system (player 2 is AI)
gs.AISys = ai.NewAISystem(2, gs.FogSys, 64, 64)
```

- [ ] **Step 3: Register AI squads in SpawnSquad**

In `SpawnSquad`, after creating the commander entity, add AI registration:

```go
// Register with AI system if this is an AI player
if playerID == gs.AISys.AIPlayerID {
    gs.AISys.RegisterSquad(squadID, uint32(cmdEntity))
}
```

- [ ] **Step 4: Update Tick() to run FogSystem and AISystem**

In `Tick()`, after the death system and before snapshot generation, add:

```go
// Fog of war: compute visibility per player
gs.FogSys.Update(cmdPool, posPool, ownerPool, healthPool)

// AI: run decision loop, execute resulting commands
aiCmds := gs.AISys.Update(gs.tickCount, cmdPool, posPool, ownerPool, healthPool)
for _, cmd := range aiCmds {
    switch cmd.Type {
    case ai.CmdMove:
        gs.handleMoveSquad(cmd.SquadID, cmd.TargetX, cmd.TargetY)
    case ai.CmdAttack:
        gs.handleAttackTarget(cmd.SquadID, cmd.TargetID)
    }
}
```

Note: The FogSystem.Update and AISystem.Update need access to component pools. Extract pool references at the start of `Tick()` similar to how other systems access them (using `gs.World.Pool(component.XXX{})`).

- [ ] **Step 5: Build and test**

Run: `cd /Users/zhangrong/repo/paper-war/server && go build ./... && go test ./... -count=1`
Expected: BUILD OK, all tests pass

- [ ] **Step 6: Commit**

```bash
git add server/pkg/game/session.go
git commit -m "feat: integrate FogSystem and AISystem into game loop"
```

---

### Task 4: Fog-Aware Snapshot Filtering

**Files:**
- Modify: `server/pkg/network/snapshot.go`
- Modify: `server/pkg/game/session.go`

- [ ] **Step 1: Modify GenerateSnapshot to accept FogGrid**

In `session.go`, change `GenerateSnapshot` to look up the player's FogGrid:

```go
func (gs *GameSession) GenerateSnapshot(clientID uint32, view network.Rect) []byte {
    // Look up player ID from client — for now, solo game: clientID 0 → player 1
    playerID := uint32(1)
    
    var fogGrid *fog.FogGrid
    if gs.FogSys != nil {
        fogGrid = gs.FogSys.GetGrid(playerID)
    }
    
    return gs.SnapGen.Generate(view, playerID, fogGrid, gs.World, gs.Sh)
}
```

- [ ] **Step 2: Update SnapshotGenerator.Generate to filter by fog**

In `snapshot.go`, update `Generate` (or equivalent) to accept `fogGrid *fog.FogGrid`:

```go
func (sg *SnapshotGenerator) Generate(view Rect, ownerID uint32, fogGrid *fog.FogGrid, world *ecs.World, sh *spatial.Hash) []byte {
    // When iterating units for snapshot:
    // - Own units (owner == ownerID): always include
    // - Enemy units: only include if fogGrid.IsVisible(tileX, tileY) == true
    // - If fogGrid is nil: include all (backwards compatible)
}
```

- [ ] **Step 3: Build and test**

Run: `cd /Users/zhangrong/repo/paper-war/server && go build ./... && go test ./... -count=1`
Expected: BUILD OK, all tests pass

- [ ] **Step 4: Commit**

```bash
git add server/pkg/network/snapshot.go server/pkg/game/session.go
git commit -m "feat: fog-aware snapshot filtering hides enemy units"
```

---

### Task 5: Client Fog Overlay

**Files:**
- Modify: `client/src/state.js`
- Modify: `client/src/main.js`
- Modify: `client/src/gl.js` (if needed)

- [ ] **Step 1: Parse fog data from server snapshot**

In `state.js`, add fog state tracking:

```javascript
// In the state object or snapshot handler:
this.fogVisible = null; // Uint8Array, 64x64
```

When receiving a snapshot that includes fog data (a new field or separate message), decode the fog grid:
```javascript
// Parse fog grid from snapshot
if (fogData) {
    this.fogVisible = new Uint8Array(fogData);
}
```

- [ ] **Step 2: Add fog overlay rendering**

In `main.js` (or `gl.js`), add a fog render pass after terrain rendering:

```javascript
// After drawing terrain tiles, draw fog overlay
function renderFog(gl, fogVisible, mapW, mapH, camera) {
    if (!fogVisible) return;
    
    // For each visible tile in viewport, if fogged, draw dark overlay
    const startX = Math.max(0, Math.floor(camera.left()));
    const startY = Math.max(0, Math.floor(camera.top()));
    const endX = Math.min(mapW, Math.ceil(camera.right()));
    const endY = Math.min(mapH, Math.ceil(camera.bottom()));
    
    for (let ty = startY; ty < endY; ty++) {
        for (let tx = startX; tx < endX; tx++) {
            if (!fogVisible[ty * mapW + tx]) {
                // Draw semi-transparent dark quad over this tile
                drawFogTile(gl, camera, tx, ty);
            }
        }
    }
}
```

Use a simple alpha-blended dark quad (rgba(0,0,0,0.6)) for fogged tiles.

- [ ] **Step 3: Hide enemy units in fogged areas**

In the unit rendering loop, skip drawing enemy units whose tile position is fogged:

```javascript
// Before rendering each unit:
if (unit.ownerID !== myPlayerID) {
    const tx = Math.floor(unit.x);
    const ty = Math.floor(unit.y);
    if (fogVisible && !fogVisible[ty * mapW + tx]) {
        continue; // skip hidden enemy
    }
}
```

- [ ] **Step 4: Test manually**

Run: `cd /Users/zhangrong/repo/paper-war/server && go run ./cmd/server/`
Open http://localhost:8090, click Solo Game. Verify:
- Fogged tiles show dark overlay
- Enemy units disappear when in fogged area
- Moving your squads reveals new tiles

- [ ] **Step 5: Commit**

```bash
git add client/
git commit -m "feat: client fog overlay and enemy unit hiding"
```

---

### Task 6: End-to-End Testing & Polish

**Files:**
- Modify: any files with bugs found during testing

- [ ] **Step 1: Start server and play a full solo game**

Run: `cd /Users/zhangrong/repo/paper-war/server && go run ./cmd/server/`
Open http://localhost:8090, click Solo Game.

Verify:
- [ ] Player squads spawn on left, enemy on right
- [ ] Enemy squads start patrolling (moving to random points)
- [ ] When your squad gets within vision range of an enemy, enemy becomes visible
- [ ] Enemy AI reacts: approaches your squad and attacks
- [ ] If enemy commander HP drops below 30%, it retreats
- [ ] Fog overlay covers tiles not near your commanders
- [ ] Fog lifts as you move commanders around the map

- [ ] **Step 2: Fix any bugs found**

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "fix: polish AI and fog of war integration"
```

- [ ] **Step 4: Push**

```bash
git push
```
