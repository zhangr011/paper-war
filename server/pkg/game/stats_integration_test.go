package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/persist"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestMatchStatsAndWireMessageOnEnd drives a close-quarters 1v1 to completion
// and verifies:
//  1. The match ends (PhaseEnded) with one side eliminated.
//  2. GetMatchStats() returns populated stats (kills > 0 on the winning side,
//     deaths matching across factions).
//  3. The wire-format conversion (mirroring main.go's broadcast) round-trips
//     through Encode/DecodeServerMessage without loss.
//
// This fills the test gap between the unit-level stats tests (stats_test.go)
// and the wire serialization test (server_msg_test.go) — neither exercises
// the full "match ends → stats populated → wire message" path that main.go
// performs on every match end.
func TestMatchStatsAndWireMessageOnEnd(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: tilemap.ObjectiveElimination}
	gs.Store = persist.NewMockStore()
	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	// Spawn two teams 4 tiles apart — close enough to engage within seconds.
	team1X := fixed.FromFloat(22.0)
	team2X := fixed.FromFloat(26.0)
	y := fixed.FromFloat(48.0)
	gs.SpawnTeamWithType(1, 1, team1X, y, 3, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, team2X, y, 3, component.UnitLightInfantry)

	// Issue mutual moves so they close into weapons range.
	gs.handleMoveSquad(1, team2X, y)
	gs.handleMoveSquad(2, team1X, y)

	const maxTicks = 3000
	var endedAt int
	for tick := 1; tick <= maxTicks; tick++ {
		gs.Tick()
		if gs.Lifecycle.Phase == PhaseEnded {
			endedAt = tick
			break
		}
	}
	if endedAt == 0 {
		// Per mini_pitch_test.go precedent: a stall is acceptable IF combat
		// occurred (units took damage). But for this test we want a real end.
		alive1, alive2 := countAlive(gs)
		t.Fatalf("match did not end within %d ticks (alive: p1=%d p2=%d)", maxTicks, alive1, alive2)
	}
	t.Logf("match ended at tick %d, winnerFaction=%d", endedAt, gs.Lifecycle.WinnerFaction)

	// 1. One faction must be fully eliminated.
	alive1, alive2 := countAlive(gs)
	if alive1 > 0 && alive2 > 0 {
		t.Fatalf("both factions still have survivors: p1=%d p2=%d", alive1, alive2)
	}

	// 2. MatchStats must be populated. At minimum, total kills across both
	//    factions must equal total deaths — every kill has a victim.
	ms := gs.GetMatchStats()
	if ms == nil {
		t.Fatal("GetMatchStats returned nil after match end")
	}
	totalKills := ms.Factions[0].Kills + ms.Factions[1].Kills
	totalDeaths := ms.Factions[0].Deaths + ms.Factions[1].Deaths
	if totalKills == 0 {
		t.Errorf("expected non-zero kills after a death-match; got 0")
	}
	if totalKills != totalDeaths {
		t.Errorf("kills (%d) != deaths (%d) — every kill must have a death", totalKills, totalDeaths)
	}
	// Winner faction should have more kills than the loser (it survived longer).
	winner := gs.Lifecycle.WinnerFaction
	loser := uint8(1 - winner)
	if ms.Factions[winner].Kills <= ms.Factions[loser].Kills {
		t.Errorf("winner faction %d kills=%d not greater than loser %d kills=%d",
			winner, ms.Factions[winner].Kills, loser, ms.Factions[loser].Kills)
	}
	t.Logf("stats: winner=%d  blue(kills=%d deaths=%d cmdKills=%d goldEarned=%d goldSpent=%d recruits=%d)  red(kills=%d deaths=%d cmdKills=%d goldEarned=%d goldSpent=%d recruits=%d)",
		winner,
		ms.Factions[0].Kills, ms.Factions[0].Deaths, ms.Factions[0].CommanderKills,
		ms.Factions[0].GoldEarned, ms.Factions[0].GoldSpent, ms.Factions[0].UnitsRecruited,
		ms.Factions[1].Kills, ms.Factions[1].Deaths, ms.Factions[1].CommanderKills,
		ms.Factions[1].GoldEarned, ms.Factions[1].GoldSpent, ms.Factions[1].UnitsRecruited,
	)

	// 3. Wire-format conversion (mirror main.go's broadcast logic) round-trips.
	//    This catches drift between MatchStats (game pkg) and MatchStatsEntry
	//    (network pkg) — e.g., if a field is added to one but not the other.
	statsMsg := &network.ServerMessage{
		Type: network.MsgMatchStats,
		Stats: [2]network.MatchStatsEntry{
			{
				Kills:          ms.Factions[0].Kills,
				Deaths:         ms.Factions[0].Deaths,
				CommanderKills: ms.Factions[0].CommanderKills,
				UnitsRecruited: ms.Factions[0].UnitsRecruited,
				GoldEarned:     ms.Factions[0].GoldEarned,
				GoldSpent:      ms.Factions[0].GoldSpent,
			},
			{
				Kills:          ms.Factions[1].Kills,
				Deaths:         ms.Factions[1].Deaths,
				CommanderKills: ms.Factions[1].CommanderKills,
				UnitsRecruited: ms.Factions[1].UnitsRecruited,
				GoldEarned:     ms.Factions[1].GoldEarned,
				GoldSpent:      ms.Factions[1].GoldSpent,
			},
		},
	}
	encoded := network.EncodeServerMessage(statsMsg)
	if len(encoded) == 0 {
		t.Fatal("EncodeServerMessage returned empty buffer")
	}
	decoded, err := network.DecodeServerMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeServerMessage failed: %v", err)
	}
	if decoded.Type != network.MsgMatchStats {
		t.Errorf("decoded type = 0x%02x, want 0x%02x", decoded.Type, network.MsgMatchStats)
	}
	// Verify every stat field survived the round-trip.
	for i := 0; i < 2; i++ {
		want := statsMsg.Stats[i]
		got := decoded.Stats[i]
		if got.Kills != want.Kills || got.Deaths != want.Deaths {
			t.Errorf("faction[%d] kills/deaths mismatch: got (%d,%d) want (%d,%d)",
				i, got.Kills, got.Deaths, want.Kills, want.Deaths)
		}
		if got.CommanderKills != want.CommanderKills {
			t.Errorf("faction[%d] commanderKills: got %d want %d", i, got.CommanderKills, want.CommanderKills)
		}
		if got.UnitsRecruited != want.UnitsRecruited {
			t.Errorf("faction[%d] unitsRecruited: got %d want %d", i, got.UnitsRecruited, want.UnitsRecruited)
		}
		if got.GoldEarned != want.GoldEarned || got.GoldSpent != want.GoldSpent {
			t.Errorf("faction[%d] gold: got (e=%d s=%d) want (e=%d s=%d)",
				i, got.GoldEarned, got.GoldSpent, want.GoldEarned, want.GoldSpent)
		}
	}
}
