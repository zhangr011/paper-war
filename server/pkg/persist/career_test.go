package persist

import (
	"context"
	"testing"
)

// TestGetCareerStatsNewPlayerReturnsZeros verifies that a player who has
// never played a match gets back a zero-valued CareerStats (not an error).
// This lets the login handler render an empty career UI without special-casing.
func TestGetCareerStatsNewPlayerReturnsZeros(t *testing.T) {
	s := NewMockStore()
	c, err := s.GetCareerStats(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetCareerStats for unknown player returned error: %v", err)
	}
	if c == nil {
		t.Fatal("GetCareerStats returned nil — callers expect a zero struct")
	}
	if c.PlayerID != 999 {
		t.Errorf("PlayerID = %d, want 999", c.PlayerID)
	}
	if c.MatchesPlayed != 0 || c.TotalKills != 0 {
		t.Errorf("new player career stats should be zeroed, got %+v", c)
	}
}

// TestAddCareerStatsAccumulates verifies that three sequential adds
// correctly sum every field.
func TestAddCareerStatsAccumulates(t *testing.T) {
	s := NewMockStore()
	const pid = uint32(1)
	ctx := context.Background()

	deltas := []CareerStats{
		{MatchesPlayed: 1, MatchesWon: 1, TotalKills: 5, TotalDeaths: 2, TotalGoldEarned: 100, TotalGoldSpent: 50, TotalRecruits: 3, CommanderKills: 0, CommandersLost: 0},
		{MatchesPlayed: 1, MatchesLost: 1, TotalKills: 3, TotalDeaths: 6, TotalGoldEarned: 60, TotalGoldSpent: 80, TotalRecruits: 2, CommanderKills: 0, CommandersLost: 1},
		{MatchesPlayed: 1, MatchesWon: 1, TotalKills: 7, TotalDeaths: 1, TotalGoldEarned: 140, TotalGoldSpent: 30, TotalRecruits: 4, CommanderKills: 1, CommandersLost: 0},
	}
	for i, d := range deltas {
		if err := s.AddCareerStats(ctx, pid, d); err != nil {
			t.Fatalf("AddCareerStats[%d] failed: %v", i, err)
		}
	}

	c, err := s.GetCareerStats(ctx, pid)
	if err != nil {
		t.Fatalf("GetCareerStats failed: %v", err)
	}
	if c.MatchesPlayed != 3 {
		t.Errorf("MatchesPlayed = %d, want 3", c.MatchesPlayed)
	}
	if c.MatchesWon != 2 {
		t.Errorf("MatchesWon = %d, want 2", c.MatchesWon)
	}
	if c.MatchesLost != 1 {
		t.Errorf("MatchesLost = %d, want 1", c.MatchesLost)
	}
	if c.TotalKills != 15 {
		t.Errorf("TotalKills = %d, want 15", c.TotalKills)
	}
	if c.TotalDeaths != 9 {
		t.Errorf("TotalDeaths = %d, want 9", c.TotalDeaths)
	}
	if c.TotalGoldEarned != 300 {
		t.Errorf("TotalGoldEarned = %d, want 300", c.TotalGoldEarned)
	}
	if c.TotalGoldSpent != 160 {
		t.Errorf("TotalGoldSpent = %d, want 160", c.TotalGoldSpent)
	}
	if c.TotalRecruits != 9 {
		t.Errorf("TotalRecruits = %d, want 9", c.TotalRecruits)
	}
	if c.CommanderKills != 1 {
		t.Errorf("CommanderKills = %d, want 1", c.CommanderKills)
	}
	if c.CommandersLost != 1 {
		t.Errorf("CommandersLost = %d, want 1", c.CommandersLost)
	}
}

// TestGetCareerStatsReturnsCopy verifies that mutations to the returned
// CareerStats don't leak back into the store. This is important because
// the integration test and main.go both read totals after AddCareerStats
// and could accidentally corrupt stored state.
func TestGetCareerStatsReturnsCopy(t *testing.T) {
	s := NewMockStore()
	const pid = uint32(1)
	ctx := context.Background()

	s.AddCareerStats(ctx, pid, CareerStats{MatchesPlayed: 1, TotalKills: 5})

	c, _ := s.GetCareerStats(ctx, pid)
	c.MatchesPlayed = 99 // mutate the returned copy
	c.TotalKills = 999

	c2, _ := s.GetCareerStats(ctx, pid)
	if c2.MatchesPlayed != 1 || c2.TotalKills != 5 {
		t.Errorf("store state was mutated via returned CareerStats: got %+v, want {MatchesPlayed:1 TotalKills:5}", c2)
	}
}

// TestFindOrCreatePlayerIssuesUniquePlayerIDs verifies two distinct tokens
// resolve to distinct player rows. Critical for the v1.1 login flow —
// without this, every client shares the same roster row.
func TestFindOrCreatePlayerIssuesUniquePlayerIDs(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()

	p1, err := s.FindOrCreatePlayer(ctx, "token-alpha", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer[1] failed: %v", err)
	}
	p2, err := s.FindOrCreatePlayer(ctx, "token-beta", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer[2] failed: %v", err)
	}
	if p1.ID == p2.ID {
		t.Errorf("two distinct tokens resolved to same playerID %d", p1.ID)
	}
	if p1.Token != "token-alpha" || p2.Token != "token-beta" {
		t.Errorf("player tokens not stored correctly: %q vs %q", p1.Token, p2.Token)
	}

	// Same token resolves to same player (idempotent).
	p1Again, err := s.FindOrCreatePlayer(ctx, "token-alpha", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer[idempotent] failed: %v", err)
	}
	if p1Again.ID != p1.ID {
		t.Errorf("idempotent lookup returned different playerID: got %d, want %d", p1Again.ID, p1.ID)
	}
}