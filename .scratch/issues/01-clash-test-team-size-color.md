---
Title: Enhance clash test — configurable team size + blue vs red colors
Status: needs-triage
Severity: enhancement
Type: frontend+server
Created: 2026-05-31
---

# Enhance clash test — configurable team size + blue vs red colors

## Parent

`.scratch/bugs/03-spectator-match-result-display.md` (related — same clash mode context)

## Description

The current Clash Test button spawns a fixed matchup: 2 squads × 5 LI per team (level 3). The frontend should let the player configure team sizes before starting, and both sides should be visually distinct with blue vs red coloring.

## Requirements

### 1. Team size selector (frontend)

In the lobby, when preparing a clash test, show two team configuration panels:

- **Team 1 (Blue)** — unit count slider or preset buttons (5 / 10 / 15 / 20)
- **Team 2 (Red)** — same options
- Default: 10 vs 10 (current behavior)

The selector could be a simple row of buttons:
```
Team 1 (Blue):  [5] [10] [15] [20]
Team 2 (Red):   [5] [10] [15] [20]
```

### 2. Send team sizes to server

The `start_clash` message should include:
```json
{
  "type": "start_clash",
  "team1_units": 10,
  "team2_units": 10
}
```

### 3. Server-side team size handling

Update `start_clash` handler in `main.go`:
- Read `team1_units` and `team2_units` from message (default 10)
- Spawn the requested number of units per team
- Split into squads: 1 squad per 10 units, min 1 squad per team

### 4. Blue vs Red unit rendering

Currently units are rendered by faction (FactionPlayer vs FactionEnemy) but both may look similar. Change rendering to:
- **Team 1 (playerID=1)** → Blue tinted units
- **Team 2 (playerID=2)** → Red tinted units
- This applies to commander circles and combat unit squares

The snapshot already carries `OwnerComponent` (playerID/faction). The client renderer (`main.js` → `buildUnitDescriptors`) should map playerID to team color:
- playerID 1 → `#4488FF` (blue)
- playerID 2 → `#FF4444` (red)

This replaces the current faction-based coloring so spectators see clear sides.

### 5. Spectator HUD

Show a simple overlay:
```
BLUE  10 units    vs    RED  10 units
```

Update as units die. This gives the spectator a scoreboard.

## Affected Files

- `client/index.html` — team size selector UI
- `client/style.css` — selector styling
- `client/src/app.js` — wire team size selector + send in start_clash message
- `client/src/main.js` — team-based unit coloring + spectator scoreboard HUD
- `client/src/gl.js` — color quads for team units
- `server/cmd/server/main.go` — parse team1_units/team2_units from start_clash
- `server/pkg/game/session.go` — SpawnTeamWithType already supports unit count via level, may need a direct unit count variant

## Scope

v1 enhancement — this is for the clash test debug tool only. Not blocking for gameplay.
