package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestFogFiltersEnemyUnits verifies the CORE acceptance criterion from issue #22:
// enemy units on tiles the player cannot see MUST be excluded from snapshots.
func TestFogFiltersEnemyUnits(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()

	// Player 1 at top of map (y=10), Player 2 far away at bottom (y=85).
	// Vision radius: commander=12 tiles, unit=6 tiles.
	// Map is 48x96, so y=85 is ~75 tiles from y=10 — well outside vision.
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(24.0), fixed.FromFloat(10.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(24.0), fixed.FromFloat(85.0), 1, component.UnitLightInfantry)
	gs.Tick()

	// Player 1's fog grid should NOT show tiles around y=85
	grid := gs.FogSys.GetGrid(1)
	if grid == nil {
		t.Fatal("player 1 fog grid missing")
	}
	// Check the enemy unit tile
	if grid.IsVisible(24, 85) {
		t.Error("BUG: enemy tile (24,85) is visible to player 1 — should be unexplored")
	}
	t.Logf("Enemy tile (24,85) visible=%v (expected false)", grid.IsVisible(24, 85))

	// Generate snapshot for player 1 — count enemy units in it
	// We need to decode the snapshot to check. Easier: count entities by team.
	// The snapshot's UnitSnapshot carries Team for new units (mask=0xFF).
	data := gs.GenerateSnapshot(1, fullView(gs))
	t.Logf("snapshot size: %d bytes", len(data))

	// Count how many player-2 units appear in player 1's snapshot.
	// We'll decode just enough: iterate the snapshot binary looking for new-unit entries.
	// New units have ChangedMask=0xFF followed by UnitType+Team bytes.
	p2Units := countEnemyUnitsInSnapshot(t, data, 2)
	t.Logf("player-2 units in player-1 snapshot: %d", p2Units)
	if p2Units > 0 {
		t.Errorf("BUG: %d enemy units visible through fog — should be 0", p2Units)
	}
}

// countEnemyUnitsInSnapshot decodes a binary snapshot and counts units
// belonging to the given team. Only counts NEW units (mask=0xFF) since
// those carry the Team byte; that's sufficient to detect fog leaks on
// first appearance.
func countEnemyUnitsInSnapshot(t *testing.T, data []byte, team uint8) int {
	t.Helper()
	if len(data) < 12 {
		return 0
	}
	// Skip header: tick(4) + prevtick(4) + unitcount(2) + eventcount(1) + basealert(1) = 12
	pos := 12
	count := 0
	for pos+5 <= len(data) {
		// entityID(4) + mask(1)
		mask := data[pos+4]
		pos += 5

		// Skip changed fields per mask bits
		// Bit 0: position (16 bytes), Bit 1: velocity (16), Bit 2: angle (2),
		// Bit 3: hp (4), Bit 4: targetID (4), Bit 5: morale (4), Bit 6: state (1), Bit 7: squadID (4)
		skipSizes := []int{16, 16, 2, 4, 4, 4, 1, 4}
		for i, sz := range skipSizes {
			if mask&(1<<i) != 0 {
				pos += sz
			}
		}
		// mask=0xFF means new unit: +UnitType(1) +Team(1)
		if mask == 0xFF {
			if pos+2 > len(data) {
				break
			}
			teamByte := data[pos+1]
			if teamByte == team {
				count++
			}
			pos += 2
		}
		// Stop if we hit the fog marker (0xFF 0xFE 0xFD 0xFC)
		if pos+3 < len(data) && data[pos] == 0xFF && data[pos+1] == 0xFE &&
			data[pos+2] == 0xFD && data[pos+3] == 0xFC {
			break
		}
	}
	return count
}
