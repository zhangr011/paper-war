package persist

import "context"

// LeaderboardEntry is one row in the leaderboard response.
// Rank is 1-indexed (rank=1 is the top player). Sorted by TotalKills desc
// as the primary metric — see Store.GetLeaderboard.
type LeaderboardEntry struct {
	Rank          uint32 `json:"rank"`
	PlayerID      uint32 `json:"player_id"`
	Name          string `json:"name"`
	MatchesPlayed uint32 `json:"matches_played"`
	MatchesWon    uint32 `json:"matches_won"`
	MatchesLost   uint32 `json:"matches_lost"`
	TotalKills    uint32 `json:"total_kills"`
	TotalDeaths   uint32 `json:"total_deaths"`
}

// LeaderboardLimit is the default max number of entries returned by
// GetLeaderboard when no explicit limit is provided. Sized to fit a single
// screen without scrolling.
const LeaderboardLimit = 10

// clampLeaderboardLimit bounds the requested limit to a safe range.
// Prevents a single client from requesting the entire player table.
func clampLeaderboardLimit(limit int) int {
	if limit <= 0 {
		return LeaderboardLimit
	}
	if limit > 100 {
		return 100
	}
	return limit
}

// suppress unused-context warning if future refactor moves the helper
// out of this file.
var _ = context.Background
