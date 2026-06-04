package game

import (
	"testing"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestObjectiveEliminationInClash(t *testing.T) {
	gs := NewGameSession()
	gs.Lifecycle.Start()
	gs.EnableClashMode()

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(20.0), fixed.FromFloat(48.0), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(28.0), fixed.FromFloat(48.0), 1, component.UnitLightInfantry)

	for i := 0; i < 500; i++ {
		gs.Tick()
		if gs.Lifecycle.Phase == PhaseEnded {
			t.Logf("Match ended at tick %d, winner=%d, reason=%s", i, gs.Lifecycle.WinnerFaction, gs.Lifecycle.WinReason)
			return
		}
	}
	t.Fatalf("Match did not end in 500 ticks, phase=%d", gs.Lifecycle.Phase)
}
