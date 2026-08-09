package game

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"testing"
)

func TestObjectiveEliminationInClash(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()
	gs.EnableClashMode()
	// Re-enable movement for this test — we're testing the elimination
	// objective mechanism, not clash balance.
	gs.AISys.MoveDisabled = false
	if gs.AISys2 != nil {
		gs.AISys2.MoveDisabled = false
	}

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(22.0), fixed.FromFloat(48.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(26.0), fixed.FromFloat(48.0), 1, component.UnitLightInfantry)

	for i := 0; i < 500; i++ {
		gs.Tick()
		if gs.Lifecycle.Phase == PhaseEnded {
			t.Logf("Match ended at tick %d, winner=%d, reason=%s", i, gs.Lifecycle.WinnerFaction, gs.Lifecycle.WinReason)
			return
		}
	}
	t.Fatalf("Match did not end in 500 ticks, phase=%d", gs.Lifecycle.Phase)
}
