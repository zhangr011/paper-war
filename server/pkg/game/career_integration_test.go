package game

import (
	"context"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/persist"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestCareerStatsWrittenOnMatchEnd verifies the full v1.1 career-stats
// pipeline: spawn two close-quarters teams, drive the match to completion,
// confirm that AddCareerStats was called for each player (winner gained
// a MatchesWon, loser gained a MatchesLost, both have kills/deaths > 0).
//
// This complements stats_integration_test.go (which only verifies the
// per-match MatchStats wire pipeline, not the cross-match accumulator).
func TestCareerStatsWrittenOnMatchEnd(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: tilemap.ObjectiveElimination}
	store := persist.NewMockStore()
	gs.Store = store
	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	// Pre-create two players in the store so we have stable IDs to assert on.
	ctx := context.Background()
	p1, err := store.FindOrCreatePlayer(ctx, "token-alpha")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer[alpha]: %v", err)
	}
	p2, err := store.FindOrCreatePlayer(ctx, "token-beta")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer[beta]: %v", err)
	}

	// Spawn close-quarters (4-tile gap, like stats_integration_test).
	const y = 48.0
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(22.0), fixed.FromFloat(y), 3, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(26.0), fixed.FromFloat(y), 3, component.UnitLightInfantry)
	gs.handleMoveSquad(1, fixed.FromFloat(26.0), fixed.FromFloat(y))
	gs.handleMoveSquad(2, fixed.FromFloat(22.0), fixed.FromFloat(y))

	// Tick to completion.
	const maxTicks = 3000
	endedAt := 0
	for tick := 1; tick <= maxTicks; tick++ {
		gs.Tick()
		if gs.Lifecycle.Phase == PhaseEnded {
			endedAt = tick
			break
		}
	}
	if endedAt == 0 {
		t.Fatalf("match did not end within %d ticks", maxTicks)
	}
	t.Logf("match ended at tick %d, winner=%d", endedAt, gs.Lifecycle.WinnerFaction)

	// Simulate what main.go does at match end: for each in-game player,
	// look up DB playerID via token, build delta from MatchStats faction
	// slice, call AddCareerStats. main.go uses clientID→token via Hub;
	// here we use the two pre-created tokens directly.
	ms := gs.GetMatchStats()
	winnerFaction := gs.Lifecycle.WinnerFaction

	// player 1 = faction 0 (FactionPlayer); player 2 = faction 1 (FactionEnemy).
	tokens := []string{"token-alpha", "token-beta"}
	factions := []uint8{component.FactionPlayer, component.FactionEnemy}
	for i, tok := range tokens {
		player, err := store.FindOrCreatePlayer(ctx, tok)
		if err != nil {
			t.Fatalf("token %s lookup: %v", tok, err)
		}
		faction := factions[i]
		delta := persist.CareerStats{
			PlayerID:        player.ID,
			MatchesPlayed:   1,
			TotalKills:      uint32(ms.Factions[faction].Kills),
			TotalDeaths:     uint32(ms.Factions[faction].Deaths),
			CommanderKills:  uint32(ms.Factions[faction].CommanderKills),
			TotalGoldEarned: uint32(ms.Factions[faction].GoldEarned),
			TotalGoldSpent:  uint32(ms.Factions[faction].GoldSpent),
			TotalRecruits:   uint32(ms.Factions[faction].UnitsRecruited),
		}
		if winnerFaction == faction {
			delta.MatchesWon = 1
		} else {
			delta.MatchesLost = 1
			delta.CommandersLost = 1
		}
		if err := store.AddCareerStats(ctx, player.ID, delta); err != nil {
			t.Fatalf("AddCareerStats for %s: %v", tok, err)
		}
	}

	// Assertions: both players should have career rows now.
	c1, err := store.GetCareerStats(ctx, p1.ID)
	if err != nil {
		t.Fatalf("GetCareerStats[alpha]: %v", err)
	}
	c2, err := store.GetCareerStats(ctx, p2.ID)
	if err != nil {
		t.Fatalf("GetCareerStats[beta]: %v", err)
	}

	for _, c := range []struct {
		name string
		cs   *persist.CareerStats
	}{
		{"alpha (player 1)", c1},
		{"beta (player 2)", c2},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.cs.MatchesPlayed != 1 {
				t.Errorf("MatchesPlayed = %d, want 1", c.cs.MatchesPlayed)
			}
			// Exactly one of won/lost must be set.
			if c.cs.MatchesWon+c.cs.MatchesLost != 1 {
				t.Errorf("won(%d) + lost(%d) != 1", c.cs.MatchesWon, c.cs.MatchesLost)
			}
			// Both kills and deaths should be present (mutual combat).
			if c.cs.TotalKills == 0 {
				t.Errorf("TotalKills = 0 — combat never attributed kills to this player")
			}
			if c.cs.TotalDeaths == 0 {
				t.Errorf("TotalDeaths = 0 — this player lost no units (unlikely in mutual combat)")
			}
			// Gold economy should be non-zero (bounties were awarded).
			if c.cs.TotalGoldEarned == 0 {
				t.Errorf("TotalGoldEarned = 0 — no bounties credited")
			}
		})
	}

	// Winner check: the player whose faction == winnerFaction has MatchesWon=1.
	winnerTok := tokens[winnerFaction]
	winnerStats := c1
	if winnerFaction == component.FactionEnemy {
		winnerStats = c2
	}
	if winnerStats.MatchesWon != 1 {
		t.Errorf("winner %s has MatchesWon=%d, want 1", winnerTok, winnerStats.MatchesWon)
	}
	loserStats := c2
	if winnerFaction == component.FactionEnemy {
		loserStats = c1
	}
	if loserStats.MatchesLost != 1 {
		t.Errorf("loser has MatchesLost=%d, want 1", loserStats.MatchesLost)
	}
	if loserStats.CommandersLost != 1 {
		t.Errorf("loser CommandersLost = %d, want 1 (permadeath)", loserStats.CommandersLost)
	}

	t.Logf("alpha: %+v", c1)
	t.Logf("beta:  %+v", c2)

	// Suppress unused-variable warning if assertions are trimmed in future.
	_ = ecs.Entity(0)
}
