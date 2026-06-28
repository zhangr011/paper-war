package persist

// CareerStats holds a player's cumulative stats across all matches.
// Distinct from MatchStats (which is per-match) — CareerStats is the
// running total written back to the Store at match end.
//
// CareerStats are append-only: each match end adds a delta via
// Store.AddCareerStats. Reads return the current totals.
type CareerStats struct {
	PlayerID         uint32 `json:"player_id"`
	MatchesPlayed    uint32 `json:"matches_played"`
	MatchesWon       uint32 `json:"matches_won"`
	MatchesLost      uint32 `json:"matches_lost"`
	TotalKills       uint32 `json:"total_kills"`
	TotalDeaths      uint32 `json:"total_deaths"`
	CommanderKills   uint32 `json:"commander_kills"`
	CommandersLost   uint32 `json:"commanders_lost"`
	TotalGoldEarned  uint32 `json:"total_gold_earned"`
	TotalGoldSpent   uint32 `json:"total_gold_spent"`
	TotalRecruits    uint32 `json:"total_recruits"`
}

// Add accumulates a delta into the receiver. Used by MockStore and by
// the integration test harness; PostgresStore uses a single SQL upsert
// instead.
func (c *CareerStats) Add(delta CareerStats) {
	c.MatchesPlayed += delta.MatchesPlayed
	c.MatchesWon += delta.MatchesWon
	c.MatchesLost += delta.MatchesLost
	c.TotalKills += delta.TotalKills
	c.TotalDeaths += delta.TotalDeaths
	c.CommanderKills += delta.CommanderKills
	c.CommandersLost += delta.CommandersLost
	c.TotalGoldEarned += delta.TotalGoldEarned
	c.TotalGoldSpent += delta.TotalGoldSpent
	c.TotalRecruits += delta.TotalRecruits
}
