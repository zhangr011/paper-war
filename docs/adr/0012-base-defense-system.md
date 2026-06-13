# 0012 — Base Defense System

Date: 2026-06-13

## Context

The player had no defensive options beyond mobile units. The AI could recall squads
to defend its spawn (Issue #9), but the player had no structures, no base-under-attack
warning, and no visual spawn marker. Strongholds were purely decorative with no gameplay
effect.

## Decision

Implement four base defense features:

### 1. Buildable Defensive Structures

Three structure types with distinct roles:

| Type | Cost | HP | Role |
|------|------|----|------|
| Watchtower | 50g | 100 | Reveals fog (cosmetic in v1) |
| Barricade | 20g | 200 | Cover, blocks movement |
| Turret | 80g | 100 | Auto-attacks enemies (DMG 15, Range 6, CD 8) |

**Constraints:**
- Max 10 structures per player
- Must be placed within 10 tiles of player spawn
- CmdBuild (0x08) network protocol command

**Implementation:** `StructureComponent` + `BuildSystem` in `pkg/combat/build.go`.
Gold deducted via shared `PlayerGold` map with per-tick `GoldDeductions` tracking.

### 2. Base Under Attack Warning

Server detects enemies within 12 tiles of player spawn, sets `BaseAlert` byte in
snapshot header. Client shows pulsing red border overlay + minimap ping circle.

### 3. Spawn Markers

Player base = green flag on map + green dot on minimap.
Enemy base = red flag on map + red dot on minimap.
Spacebar = jump camera to player base.

### 4. Stronghold Defensive Bonus

Units standing on stronghold tiles take reduced damage:
- Level 1: 25% reduction
- Level 2-4: +6% per level (31%, 37%, 43%)
- Level 5: 49% reduction

Computed via `StrongholdDefenseBonus(level)` in combat damage application.

## Files Changed

**Server:**
- `pkg/component/structure.go` — StructureType enum + StructureComponent + StructureTypeTable
- `pkg/combat/build.go` — BuildSystem + BuildRequest + StrongholdDefenseBonus
- `pkg/combat/combat.go` — Stronghold damage reduction in damage application
- `pkg/game/session.go` — CmdBuild handling, configureAIStrategy spawns, checkBaseAlert, TerrainFn
- `pkg/network/protocol.go` — CmdBuild (0x08) encode/decode
- `pkg/network/snapshot.go` — BaseAlert byte in header

**Client:**
- `connection.js` — CMD_BUILD constant, sendBuild(), baseAlert parsing
- `main.js` — Build mode, spawn markers, structure rendering, base alert overlay
- `input.js` — onLeftClick intercept for build mode
- `index.html` — Build buttons + base alert overlay div
- `style.css` — Build button styles + base alert animation

## Consequences

- Snapshot header grows by 1 byte (12 bytes total: tick+prevtick+unitcount+eventcount+basealert)
- All snapshot tests updated to account for new header byte
- Structures are tracked locally on client for immediate render; server tracks via StructureComponent
- Turret auto-attack piggybacks on CombatSystem (has AttackComponent)
