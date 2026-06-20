package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestStatsResetBetweenMatches is a regression test for issue #34:
// "result statistics error again". Match stats were never reset between
// matches in the same server session, so they accumulated across runs.
// User observed: Blue 72 losses, Red 77 losses — impossible in a single
// solo match with ~12 units per side; this was actually 7 solo matches
// of accumulated stats.
func TestStatsResetBetweenMatches(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Phase = PhasePlaying

	// --- Match 1 ---
	gs.Reset()
	gs.Map.Objective.Type = 0
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(48), fixed.FromFloat(40), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(48), fixed.FromFloat(50), 1, component.UnitLightInfantry)
	for i := 0; i < 600; i++ {
		gs.Tick()
		if gs.Lifecycle.Phase != PhasePlaying {
			break
		}
	}
	stats1 := *gs.GetMatchStats()
	totalDeaths1 := stats1.Factions[0].Deaths + stats1.Factions[1].Deaths
	t.Logf("Match 1: blue deaths=%d, red deaths=%d, total=%d",
		stats1.Factions[0].Deaths, stats1.Factions[1].Deaths, totalDeaths1)

	if totalDeaths1 == 0 {
		t.Skip("match 1 had no combat — adjust positions")
	}

	// --- Match 2 (should start fresh, NOT include match 1's stats) ---
	gs.Reset()
	gs.Map.Objective.Type = 0
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(48), fixed.FromFloat(40), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(48), fixed.FromFloat(50), 1, component.UnitLightInfantry)
	for i := 0; i < 600; i++ {
		gs.Tick()
		if gs.Lifecycle.Phase != PhasePlaying {
			break
		}
	}
	stats2 := gs.GetMatchStats()
	totalDeaths2 := stats2.Factions[0].Deaths + stats2.Factions[1].Deaths
	t.Logf("Match 2: blue deaths=%d, red deaths=%d, total=%d",
		stats2.Factions[0].Deaths, stats2.Factions[1].Deaths, totalDeaths2)

	// REGRESSION CHECK: stats should NOT include match 1's deaths.
	// Pre-fix: match 2 would show roughly 2× the deaths of match 1.
	// With reset: match 2 deaths ≈ match 1 deaths (similar fight).
	if totalDeaths2 > totalDeaths1+4 {
		t.Errorf("STATS ACCUMULATION BUG: match 2 total deaths=%d, match 1=%d — stats not reset between matches (expected ≈%d±4, got %d)",
			totalDeaths2, totalDeaths1, totalDeaths1, totalDeaths2)
	}
}
