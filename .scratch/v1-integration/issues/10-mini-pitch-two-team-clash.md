Title: Mini pitch: automated 2-team clash test
Status: ready-for-agent
Labels: feature, test
Priority: P2
Area: server/game

## Goal

Create a minimal test scenario that spawns 2 full teams (player vs AI) and lets them fight to completion. The "mini pitch" should be a standalone Go test that exercises the full tick pipeline end-to-end: spawn, move toward each other, engage combat, units die, match ends.

## Why

Currently we test individual systems in isolation (combat, movement, death, etc.) but have no test that verifies the full game loop with two opposing teams actually fighting. This would catch integration bugs like:
- Combat units not reaching enemies
- Damage not being applied across factions
- Match end not triggering when one side is eliminated
- Gold bounties not flowing after kills
- Roster flush not persisting survivors

## Specification

### Test: `TestMiniPitchTwoTeamClash`

1. **Setup**: Create a `GameSession` with a small map (e.g. 24x48 or reuse the default 48x96). Wire a `MockStore` for persistence.

2. **Spawn**:
   - Team 1 (player, squadID=1): `SpawnTeamWithType(1, 1, cx=5.0, cy=24.0, level=3, UnitLightInfantry)`
   - Team 2 (enemy, squadID=2): `SpawnTeamWithType(2, 2, cx=43.0, cy=24.0, level=3, UnitLightInfantry)`
   - Level 3 gives each team `InitialTeamCombatUnits + 2*CombatUnitsPerTeamLevel` combat units (a decent fight)

3. **Move**: Issue `handleMoveSquad(1, enemy_spawn_x, enemy_spawn_y)` and `handleMoveSquad(2, player_spawn_x, player_spawn_y)` so both teams march toward each other.

4. **Tick loop**: Run `Tick()` in a loop (max ~3000 ticks = 5 minutes at 10Hz). Each tick:
   - Check if `Lifecycle.Phase == PhaseEnded` → break
   - Optionally log tick number, alive counts per faction

5. **Assertions**:
   - Match ended (phase = PhaseEnded)
   - One team has survivors, the other has zero alive units
   - Winner's gold > start gold (bounties collected)
   - Roster was flushed to Store (survivors persisted)
   - Eliminated team got a starter roster via `CreateStarterRoster`

6. **Output**: Log a short battle report:
   ```
   MINI PITCH RESULTS
   Ticks: 847
   Winner: Player (team 1)
   Team 1 survivors: 3/11
   Team 2 survivors: 0/11
   Gold earned: 72
   ```

### File

`server/pkg/game/mini_pitch_test.go`

### Notes

- This is a server-side only test — no client/browser needed
- The AI system may or may not be wired; for v1, just use direct `handleMoveSquad` commands for both sides
- If the movement bug (issue #02 — commander wrong direction) blocks teams from reaching each other, consider spawning teams closer together (e.g. 15 tiles apart instead of 38) as a workaround
- Keep the test under 5 seconds wall time
