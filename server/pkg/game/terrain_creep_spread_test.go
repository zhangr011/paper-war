package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestLiveCreepSpread verifies the Phase 4 creep overlay spreads from an alive
// commander through a real gs.Tick() loop and lands on gs.Map (readable via
// CreepData, which is what the client receives at 2Hz). The CreepSystem
// re-derives the overlay every SpreadInterval (5) ticks from commander
// positions, so a player commander at (8,8) must seed CreepOwner=1 on its tile
// and nearby tiles within a few spread cycles.
//
// This also guards the ResetWithMap wiring: creepSys must operate on gs.Map
// after a reset, not the original NewGameSession map — the same stale-instance
// trap that broke terrainSys.
func TestLiveCreepSpread(t *testing.T) {
	m := tilemap.NewGameMap(16, 16)

	gs := NewGameSession()
	gs.ResetWithMap(m)
	gs.EnableClashMode()
	gs.Lifecycle.Phase = PhasePlaying

	gs.SpawnSquadWithType(1, 1, fixed.FromFloat(8.0), fixed.FromFloat(8.0), 0, component.UnitLightInfantry)
	// A far-away enemy commander keeps the Elimination objective from ending
	// the match at tick 1 (no enemies → instant win), which would halt gs.Tick
	// before creepSys's first SpreadInterval tick (5). Pin AI movement so it
	// stays put and doesn't interact.
	gs.SpawnSquadWithType(2, 2, fixed.FromFloat(2.0), fixed.FromFloat(2.0), 0, component.UnitLightInfantry)
	if gs.AISys != nil {
		gs.AISys.MoveDisabled = true
		gs.AISys.RecruitDisabled = true
	}

	// SpreadInterval=5 → 15 ticks = 3 spread cycles, enough to seed the source
	// tile and immediate neighbours.
	for i := 0; i < 15; i++ {
		gs.Tick()
	}

	data := gs.CreepData() // raw w*h bytes, one CreepOwner (0/1/2) per tile

	// Source tile (8,8) must be claimed by the player (CreepOwner=1).
	src := data[8*16+8]
	if src != 1 {
		t.Errorf("source tile (8,8) CreepOwner=%d, want 1 (creep did not seed under the commander)", src)
	}

	// At least a few tiles around the source should be claimed (BFS spread).
	claimed := 0
	for _, c := range data {
		if c == 1 {
			claimed++
		}
	}
	if claimed < 2 {
		t.Errorf("creep spread: %d tile(s) claimed, want ≥2 (creep did not spread from the source)", claimed)
	}
}
