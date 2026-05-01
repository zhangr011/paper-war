# Phase 4: Network Sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Build the network synchronization layer — binary input protocol, incremental state snapshots, viewport culling, and WebSocket transport.

**Architecture:** Server maintains per-client view state. Each tick, SnapshotSystem generates incremental diffs (bitmasked changed fields). ViewCulling filters to only visible units per client. WebSocket transport handles binary framing.

**Tech Stack:** Go, gorilla/websocket, pkg/ecs, pkg/component

**Spec reference:** `docs/superpowers/specs/2026-05-01-paper-war-rts-design.md` Section 7

---

## File Structure

```
server/pkg/
  network/
    protocol.go      # Command types, binary encode/decode
    protocol_test.go
    snapshot.go       # Incremental snapshot generation
    snapshot_test.go
    culling.go        # Per-client viewport culling
    culling_test.go
    transport.go      # WebSocket server + client session management
```

---

### Task 1: Binary Protocol (Encode/Decode)

**Files:**
- Create: `server/pkg/network/protocol.go`
- Create: `server/pkg/network/protocol_test.go`

Define command types and binary encoding:
- CmdMoveSquad (0x01): SquadID + TargetX + TargetY
- CmdAttackTarget (0x02): SquadID + TargetEntityID
- CmdAttackGround (0x03): SquadID + TargetX + TargetY
- CmdChangeFormation (0x04): SquadID + FormationType
- CmdTacticalOrder (0x05): SquadID + OrderType

Encode/decode functions using bytes.Buffer with binary.Write/Read.

---

### Task 2: Incremental Snapshot

**Files:**
- Create: `server/pkg/network/snapshot.go`
- Create: `server/pkg/network/snapshot_test.go`

SnapshotSystem:
- Tracks previous frame state per unit
- Generates UnitUpdate with ChangedMask bitmask:
  - bit 0: Position, bit 1: Velocity, bit 2: Angle
  - bit 3: HP, bit 4: TargetID, bit 5: Morale, bit 6: State
- Only includes units with at least one changed field
- Generates events (Damage, Death, TerrainChange, CommanderDown)

---

### Task 3: Viewport Culling

**Files:**
- Create: `server/pkg/network/culling.go`
- Create: `server/pkg/network/culling_test.go`

Per-client filtering:
- ClientView struct with ViewRect, OwnerID, VisibleSet
- Only include units:
  1. In viewport + owned by player (full state)
  2. In viewport + visible (enemy in fog of war)
  3. All owned commanders (always synced)
- Spatial Hash query for viewport overlap

---

### Task 4: WebSocket Transport

**Files:**
- Create: `server/pkg/network/transport.go`

WebSocket server using gorilla/websocket:
- ClientSession: connection, playerID, lastAckTick
- Hub: manages all sessions, broadcasts snapshots
- Message types: binary commands (uplink), binary snapshots (downlink)
