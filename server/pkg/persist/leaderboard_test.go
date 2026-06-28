package persist

import (
	"context"
	"testing"
)

// TestGetLeaderboardEmptyStoreReturnsEmpty verifies that a fresh store with
// no career entries returns an empty (non-nil-able, length-zero) slice.
func TestGetLeaderboardEmptyStoreReturnsEmpty(t *testing.T) {
	s := NewMockStore()
	entries, err := s.GetLeaderboard(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetLeaderboard on empty store: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("empty store returned %d entries, want 0", len(entries))
	}
}

// TestGetLeaderboardExcludesZeroMatchPlayers verifies that players who have
// a career row but zero matches played are filtered out — they have no
// stats worth ranking.
func TestGetLeaderboardExcludesZeroMatchPlayers(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()

	// Player 1: 5 kills, 1 match.
	s.FindOrCreatePlayer(ctx, "tok-1", "alice")
	s.AddCareerStats(ctx, 1, CareerStats{MatchesPlayed: 1, TotalKills: 5})
	// Player 2: 0 matches, 0 kills — should be excluded.
	s.FindOrCreatePlayer(ctx, "tok-2", "bob")
	s.AddCareerStats(ctx, 2, CareerStats{}) // creates row but all zeros

	entries, err := s.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("GetLeaderboard: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (zero-match player excluded)", len(entries))
	}
	if entries[0].Name != "alice" {
		t.Errorf("entry[0].Name = %q, want \"alice\"", entries[0].Name)
	}
}

// TestGetLeaderboardSortedByKillsDesc verifies the sort order: most kills
// first, ties broken by MatchesWon desc, then PlayerID asc.
func TestGetLeaderboardSortedByKillsDesc(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()

	// Three players with distinct kill counts.
	s.FindOrCreatePlayer(ctx, "tok-low", "low")
	s.AddCareerStats(ctx, 1, CareerStats{MatchesPlayed: 1, TotalKills: 3})
	s.FindOrCreatePlayer(ctx, "tok-high", "high")
	s.AddCareerStats(ctx, 2, CareerStats{MatchesPlayed: 1, TotalKills: 10})
	s.FindOrCreatePlayer(ctx, "tok-mid", "mid")
	s.AddCareerStats(ctx, 3, CareerStats{MatchesPlayed: 1, TotalKills: 7})

	entries, err := s.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("GetLeaderboard: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	wantOrder := []string{"high", "mid", "low"}
	for i, want := range wantOrder {
		if entries[i].Name != want {
			t.Errorf("entry[%d].Name = %q, want %q (kills=%d)", i, entries[i].Name, want, entries[i].TotalKills)
		}
		if entries[i].Rank != uint32(i+1) {
			t.Errorf("entry[%d].Rank = %d, want %d", i, entries[i].Rank, i+1)
		}
	}
}

// TestGetLeaderboardKillsTieBreak verifies the tie-break rules: equal kills
// → more wins ranks higher; equal kills + wins → lower playerID ranks higher.
func TestGetLeaderboardKillsTieBreak(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()

	// Two players with identical kills.
	s.FindOrCreatePlayer(ctx, "tok-a", "alice")
	s.AddCareerStats(ctx, 1, CareerStats{MatchesPlayed: 2, MatchesWon: 1, MatchesLost: 1, TotalKills: 5})
	s.FindOrCreatePlayer(ctx, "tok-b", "bob")
	s.AddCareerStats(ctx, 2, CareerStats{MatchesPlayed: 2, MatchesWon: 2, MatchesLost: 0, TotalKills: 5})

	entries, _ := s.GetLeaderboard(ctx, 10)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	// bob has more wins despite same kills → ranks first.
	if entries[0].Name != "bob" {
		t.Errorf("tie-break by wins: entry[0].Name = %q, want \"bob\"", entries[0].Name)
	}
}

// TestGetLeaderboardLimitClamped verifies that GetLeaderboard respects the
// limit parameter and clamps abusive values to [1, 100].
func TestGetLeaderboardLimitClamped(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()
	// Seed 5 players.
	for i := 1; i <= 5; i++ {
		s.FindOrCreatePlayer(ctx, "tok-"+string(rune('a'+i)), "p"+string(rune('a'+i)))
		s.AddCareerStats(ctx, uint32(i), CareerStats{MatchesPlayed: 1, TotalKills: uint32(i * 2)})
	}

	// Limit 3 → only top 3.
	entries, _ := s.GetLeaderboard(ctx, 3)
	if len(entries) != 3 {
		t.Errorf("limit=3 returned %d entries, want 3", len(entries))
	}

	// Limit 0 → default (10), but only 5 players exist.
	entries, _ = s.GetLeaderboard(ctx, 0)
	if len(entries) != 5 {
		t.Errorf("limit=0 returned %d entries, want 5 (default applied)", len(entries))
	}

	// Limit -5 → clamped to default (10), returns 5.
	entries, _ = s.GetLeaderboard(ctx, -5)
	if len(entries) != 5 {
		t.Errorf("limit=-5 returned %d entries, want 5 (default applied)", len(entries))
	}

	// Limit 1000 → clamped to 100, returns all 5.
	entries, _ = s.GetLeaderboard(ctx, 1000)
	if len(entries) != 5 {
		t.Errorf("limit=1000 returned %d entries, want 5 (clamped to 100)", len(entries))
	}
}

// TestFindOrCreatePlayerNameUpdate verifies the last-login-wins behavior:
// calling FindOrCreatePlayer with the same token but a different name
// updates the stored Player.Name. Important for renames + leaderboard display.
func TestFindOrCreatePlayerNameUpdate(t *testing.T) {
	s := NewMockStore()
	ctx := context.Background()

	p1, _ := s.FindOrCreatePlayer(ctx, "tok-rename", "alice")
	if p1.Name != "alice" {
		t.Errorf("first login name = %q, want \"alice\"", p1.Name)
	}

	// Same token, different name — should update.
	p2, _ := s.FindOrCreatePlayer(ctx, "tok-rename", "alice_renamed")
	if p2.ID != p1.ID {
		t.Errorf("rename changed playerID: got %d, want %d", p2.ID, p1.ID)
	}
	if p2.Name != "alice_renamed" {
		t.Errorf("renamed player name = %q, want \"alice_renamed\"", p2.Name)
	}

	// Verify the persisted record was updated (not just the returned pointer).
	p3, _ := s.FindOrCreatePlayer(ctx, "tok-rename", "")
	if p3.Name != "alice_renamed" {
		t.Errorf("name not persisted across calls: got %q, want \"alice_renamed\" (last login wins, empty name = no change)", p3.Name)
	}
}
