# AI Opponent & Fog of War — Design Spec

## Overview

Two features for Paper War solo mode:
1. **AI Opponent** — Simple reactive AI that controls enemy squads (PlayerID=2)
2. **Fog of War** — Commander-centered squad vision that hides enemy units outside visible range

---

## 1. AI Opponent

### 1.1 Architecture

A new `AISystem` in `server/pkg/ai/` runs each tick for all squads owned by the AI player. Each AI squad acts independently.

### 1.2 Decision Loop (per squad, per tick)

```
1. SCAN: Find nearest enemy squad within commander vision radius (12 tiles)
   - Query spatial hash for enemy commanders within vision range
   - If AI "fairness" enabled: only consider enemies within AI player's FogGrid visible tiles
   
2. DECIDE:
   - If squad commander HP < 30% → RETREAT (move toward own map edge)
   - If enemy spotted, in attack range → ATTACK (CmdAttackTarget on enemy commander)
   - If enemy spotted, out of range → APPROACH (CmdMoveSquad toward enemy commander position)
   - If no enemy visible → PATROL (move to random map point; pick new point on arrival)

3. COOLDOWN: Only re-evaluate every 30 ticks (~6 seconds) to avoid jittery behavior
   - Emergency overrides (low HP retreat) bypass cooldown
```

### 1.3 AI State

```go
type AIState struct {
    SquadID       uint32
    State         uint8   // 0=Idle, 1=Patrol, 2=Approach, 3=Attack, 4=Retreat
    TargetSquadID uint32
    PatrolTargetX int64   // fixed-point
    PatrolTargetY int64
    NextEvalTick  uint32  // next tick to re-evaluate decision
}
```

### 1.4 Fair AI

The AI checks its own FogGrid before scanning for enemies. This means:
- AI squads patrol until they actually "see" the player
- No cheating — AI only reacts to what it can genuinely see
- Creates natural scouting and encounter dynamics

### 1.5 Files

- Create: `server/pkg/ai/ai.go` — AISystem, AIState, decision loop
- Create: `server/pkg/ai/ai_test.go` — unit tests
- Modify: `server/pkg/game/session.go` — add AISystem to tick pipeline, create AISystem instance
- Modify: `server/cmd/server/main.go` — no changes needed (AI activates for PlayerID=2 squads automatically)

---

## 2. Fog of War

### 2.1 Visibility Model

- Each **commander** has a `VisionRadius` of 12 tiles (stored in CommanderComponent or a constant)
- A player's visible area = union of all their alive commanders' vision circles
- **Binary fog:** tiles are either visible (clear) or fogged (dark). No explored/last-seen state.
- Terrain blocking: **not in first pass**. Pure radius-based visibility.

### 2.2 Server-Side: FogSystem

```go
type FogGrid struct {
    Width, Height int32
    Visible       []uint8  // 0=fogged, 1=visible (per player, indexed by playerID)
}
```

Per tick, for each player:
1. Clear all tiles to fogged (0)
2. For each alive commander owned by this player:
   - Mark all tiles within `VisionRadius` as visible (1)
3. Store the grid for snapshot filtering

### 2.3 Snapshot Filtering

`GenerateSnapshot` already takes `clientID` and `view Rect`. Enhance it:
1. Look up the player's FogGrid via `OwnerID` from `ClientView`
2. Own units (matching OwnerID) → always include
3. Enemy units → only include if their tile position is visible in FogGrid
4. Commander entities of own squads → always include (even off-screen)

### 2.4 Client-Side

- Receive `fog_update` as part of snapshot (or separate binary message)
- Render fog overlay: a dark semi-transparent layer over fogged tiles
- Fogged tiles show terrain but no units
- Clear transition: tiles switch between visible/fogged each snapshot

### 2.5 Files

- Create: `server/pkg/fog/fog.go` — FogGrid, FogSystem
- Create: `server/pkg/fog/fog_test.go` — unit tests
- Modify: `server/pkg/game/session.go` — add FogSystem to tick pipeline, pass FogGrid to snapshot generation
- Modify: `server/pkg/network/snapshot.go` — filter enemy units by FogGrid visibility
- Modify: `server/pkg/game/session.go` — `GenerateSignature` signature change to accept FogGrid
- Modify: `client/src/state.js` — parse fog data
- Modify: `client/src/main.js` — render fog overlay
- Modify: `client/src/gl.js` — fog shader or overlay rendering

---

## 3. Integration

### 3.1 Tick Pipeline Update

Current order:
```
InputSystem → CommanderAISystem → TerrainSystem → FlowFieldSystem → 
BoidSystem → MovementSystem → SpatialHashUpdate → CombatSystem → 
ProjectileSystem → DeathSystem → SnapshotSystem
```

New order:
```
InputSystem → CommanderAISystem → TerrainSystem → FlowFieldSystem → 
BoidSystem → MovementSystem → SpatialHashUpdate → CombatSystem → 
ProjectileSystem → DeathSystem → AISystem → FogSystem → SnapshotSystem
```

- **AISystem** after DeathSystem: so AI reacts to current state after combat resolves
- **FogSystem** before SnapshotSystem: so fog is up-to-date when snapshots are generated

### 3.2 Solo Game Flow

No changes to solo game setup. Enemy squads (PlayerID=2) will automatically be controlled by AISystem. FogSystem activates for both players.

### 3.3 Multiplayer Compatibility

Both systems are player-count agnostic:
- AISystem: only controls squads where PlayerID matches configured AI player IDs
- FogSystem: computes per-player fog, works for any number of players

---

## 4. Constants

| Constant | Value | Notes |
|----------|-------|-------|
| AI Vision Radius | 12 tiles | Same as player commander vision |
| AI Eval Interval | 30 ticks (~6s) | How often AI re-evaluates per squad |
| AI Retreat HP Threshold | 30% | Commander HP % to trigger retreat |
| Commander Vision Radius | 12 tiles | Fog of war visibility range |
