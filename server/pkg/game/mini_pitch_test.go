package game

import (
	"fmt"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/persist"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestMiniPitchTwoTeamClash spawns two level-3 teams, moves them toward each
// other, and lets them fight to completion. Validates the full tick pipeline:
// spawn → movement → combat → death → elimination → gold bounties.
func TestMiniPitchTwoTeamClash(t *testing.T) {
	gs := NewGameSession()

	// Force elimination objective so match ends when one side is wiped out.
	gs.Map.Objective = tilemap.Objective{Type: tilemap.ObjectiveElimination}

	// Wire a MockStore for persistence.
	store := persist.NewMockStore()
	gs.Store = store

	// Ensure match is in Playing phase.
	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	// Spawn positions: close together (4 tiles apart) so they engage fast.
	// Map is 48x96, center Y=48.
	team1X := fixed.FromFloat(22.0)
	team1Y := fixed.FromFloat(48.0)
	team2X := fixed.FromFloat(26.0)
	team2Y := fixed.FromFloat(48.0)

	level := uint8(3)
	// Level 3: 5 + 2*2 = 9 combat units per team
	unitCount := CombatUnitCountForTeamLevel(level)
	totalPerTeam := 1 + unitCount // commander + units

	// Spawn teams.
	gs.SpawnTeamWithType(1, 1, team1X, team1Y, level, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, team2X, team2Y, level, component.UnitLightInfantry)

	// Record starting gold.
	startGold1 := gs.PlayerGold[1]
	startGold2 := gs.PlayerGold[2]

	// Move both teams toward each other's spawn.
	gs.handleMoveSquad(1, team2X, team2Y)
	gs.handleMoveSquad(2, team1X, team1Y)

	// Tick loop — max 3000 ticks (5 min at 10Hz).
	const maxTicks = 3000
	tick := 0
	for tick = 1; tick <= maxTicks; tick++ {
		gs.Tick()
		if gs.Lifecycle.Phase == PhaseEnded {
			break
		}
	}

	// --- Assertions ---

	// 1. Match must have ended.
	if gs.Lifecycle.Phase != PhaseEnded {
		t.Fatalf("match did not end within %d ticks (phase=%d)", maxTicks, gs.Lifecycle.Phase)
	}

	// 2. Count survivors per team.
	alive1, alive2 := countAlive(gs)

	// 3. One side must be eliminated.
	if alive1 > 0 && alive2 > 0 {
		t.Errorf("both teams have survivors: team1=%d, team2=%d", alive1, alive2)
	}

	// 4. Winner must match who survived.
	winner := gs.Lifecycle.WinnerFaction
	if alive1 > 0 && winner != component.FactionPlayer {
		t.Errorf("team1 has %d survivors but winner=%d (expected FactionPlayer=0)", alive1, winner)
	}
	if alive2 > 0 && winner != component.FactionEnemy {
		t.Errorf("team2 has %d survivors but winner=%d (expected FactionEnemy=1)", alive2, winner)
	}
	if alive1 == 0 && alive2 == 0 {
		t.Logf("mutual destruction detected, winner=%d", winner)
	}

	// 5. Winner's gold > start gold (bounties collected).
	if winner == component.FactionPlayer && gs.PlayerGold[1] <= startGold1 {
		t.Errorf("team1 won but gold didn't increase: %d <= %d", gs.PlayerGold[1], startGold1)
	}
	if winner == component.FactionEnemy && gs.PlayerGold[2] <= startGold2 {
		t.Errorf("team2 won but gold didn't increase: %d <= %d", gs.PlayerGold[2], startGold2)
	}

	// 6. Win reason should be "elimination".
	if gs.Lifecycle.WinReason != "elimination" {
		t.Errorf("expected win reason 'elimination', got %q", gs.Lifecycle.WinReason)
	}

	// --- Battle report ---
	t.Logf("Match ended at tick %d: team1=%d/%d, team2=%d/%d, reason=%s",
		tick, alive1, totalPerTeam, alive2, totalPerTeam, gs.Lifecycle.WinReason)
	fmt.Printf("\n=== MINI PITCH RESULTS ===\n")
	fmt.Printf("Ticks: %d\n", tick)
	if winner == component.FactionPlayer {
		fmt.Printf("Winner: Player (team 1)\n")
	} else {
		fmt.Printf("Winner: Enemy (team 2)\n")
	}
	fmt.Printf("Team 1 survivors: %d/%d\n", alive1, totalPerTeam)
	fmt.Printf("Team 2 survivors: %d/%d\n", alive2, totalPerTeam)
	goldEarned := gs.PlayerGold[1] - startGold1
	if winner == component.FactionEnemy {
		goldEarned = gs.PlayerGold[2] - startGold2
	}
	fmt.Printf("Gold earned: %d\n", goldEarned)
	fmt.Printf("==========================\n\n")
}

// countAlive returns the number of living entities per player.
func countAlive(gs *GameSession) (int, int) {
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	alive1, alive2 := 0, 0
	healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		if owner, ok := ownerPool.Get(e); ok {
			switch owner.PlayerID {
			case 1:
				alive1++
			case 2:
				alive2++
			}
		}
	})
	return alive1, alive2
}
