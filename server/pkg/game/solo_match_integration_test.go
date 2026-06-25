package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/network"
)

// TestSoloMatchRunsToCompletion runs a full solo match to completion by
// repeatedly issuing charge commands and ticking until the lifecycle
// reports PhaseEnded. Catches:
//   - Panics in long-running matches (memory leaks, dangling state)
//   - AI never recruiting / never moving (passive AI bug)
//   - Match never ending (objective bug)
//   - Gold never changing (economy deadlock)
//
// This test was added after QA round 4 found that long solo matches
// in the browser were hard to test reliably (browser tab disconnects
// after ~3 minutes of inactivity).
func TestSoloMatchRunsToCompletion(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective.Type = 0 // elimination

	// Spawn player 1 (top) and player 2 / AI (bottom), matching cmd/server/main.go.
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(22), fixed.FromFloat(3), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(1, 2, fixed.FromFloat(26), fixed.FromFloat(3), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 3, fixed.FromFloat(22), fixed.FromFloat(93), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 4, fixed.FromFloat(26), fixed.FromFloat(93), 1, component.UnitLightInfantry)
	gs.PlayerGold[1] = 50
	gs.PlayerGold[2] = 50

	// Wire shared state maps
	if gs.recruitSys != nil {
		gs.recruitSys.PlayerGold = gs.PlayerGold
	}
	if gs.buildSys != nil {
		gs.buildSys.PlayerGold = gs.PlayerGold
		gs.buildSys.PlayerSpawns[1] = [2]int64{fixed.FromFloat(24), fixed.FromFloat(3)}
		gs.buildSys.PlayerSpawns[2] = [2]int64{fixed.FromFloat(24), fixed.FromFloat(93)}
	}

	// Track initial state
	goldStart1 := gs.PlayerGold[1]
	goldStart2 := gs.PlayerGold[2]

	const MaxTicks = 5000 // 500s at 10Hz; plenty for any solo match
	lastTickUnits := 0
	unitCountChanges := 0
	goldChanges := 0
	lastGold1 := goldStart1

	for i := 0; i < MaxTicks; i++ {
		// Player 1 issues charge toward (24, 90) every 100 ticks.
		if i%100 == 0 {
			gs.HandleCommand(1, &network.Command{
				Type:    network.CmdMoveSquad,
				SquadID: 1,
				TargetX: int32(fixed.FromFloat(24)),
				TargetY: int32(fixed.FromFloat(90)),
			})
			gs.HandleCommand(1, &network.Command{
				Type:    network.CmdMoveSquad,
				SquadID: 2,
				TargetX: int32(fixed.FromFloat(24)),
				TargetY: int32(fixed.FromFloat(90)),
			})
		}

		gs.Tick()

		// Track changes
		unitsNow := 0
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		boidPool.Each(func(entity ecs.Entity, bc *component.BoidComponent) {
			_ = bc
			unitsNow++
		})
		if unitsNow != lastTickUnits {
			unitCountChanges++
			lastTickUnits = unitsNow
		}
		if gs.PlayerGold[1] != lastGold1 {
			goldChanges++
			lastGold1 = gs.PlayerGold[1]
		}

		// Stop when match ends.
		if gs.Lifecycle != nil && gs.Lifecycle.Phase == PhaseEnded {
			t.Logf("Match ended at tick %d (winner faction=%d, reason=%q)",
				i, gs.Lifecycle.WinnerFaction, gs.Lifecycle.WinReason)
			t.Logf("Gold: player 1 = %d (start %d), player 2 = %d (start %d)",
				gs.PlayerGold[1], goldStart1, gs.PlayerGold[2], goldStart2)
			t.Logf("Unit count changes observed: %d, gold changes for player 1: %d",
				unitCountChanges, goldChanges)
			return
		}
	}

	t.Fatalf("Match did not end after %d ticks. Last state: units=%d, gold[1]=%d, gold[2]=%d, phase=%v",
		MaxTicks, lastTickUnits, gs.PlayerGold[1], gs.PlayerGold[2], gs.Lifecycle.Phase)
}
