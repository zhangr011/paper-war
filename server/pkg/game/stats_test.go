package game

import (
	"testing"
)

// TestMatchStatsTracksKill verifies that a single kill is attributed to the
// correct factions: killer gets +1 Kill, dead faction gets +1 Death, and the
// bounty is added to the killer's GoldEarned.
func TestMatchStatsTracksKill(t *testing.T) {
	s := NewMatchStats()
	s.RecordKill(0, 1, false, 5)
	if s.Factions[0].Kills != 1 {
		t.Errorf("player kills = %d, want 1", s.Factions[0].Kills)
	}
	if s.Factions[1].Deaths != 1 {
		t.Errorf("enemy deaths = %d, want 1", s.Factions[1].Deaths)
	}
	if s.Factions[0].GoldEarned != 5 {
		t.Errorf("player gold earned = %d, want 5", s.Factions[0].GoldEarned)
	}
}

// TestMatchStatsTracksCommanderKill verifies that a commander kill increments
// both Kills and CommanderKills for the killer's faction.
func TestMatchStatsTracksCommanderKill(t *testing.T) {
	s := NewMatchStats()
	s.RecordKill(1, 0, true, 10)
	if s.Factions[1].Kills != 1 {
		t.Errorf("enemy kills = %d, want 1", s.Factions[1].Kills)
	}
	if s.Factions[1].CommanderKills != 1 {
		t.Errorf("enemy commander kills = %d, want 1", s.Factions[1].CommanderKills)
	}
	if s.Factions[0].Deaths != 1 {
		t.Errorf("player deaths = %d, want 1", s.Factions[0].Deaths)
	}
}

// TestMatchStatsTracksRecruit verifies that recruiting increments the count
// and accumulates the gold spent.
func TestMatchStatsTracksRecruit(t *testing.T) {
	s := NewMatchStats()
	s.RecordRecruit(0, 15)
	s.RecordRecruit(0, 20)
	if s.Factions[0].UnitsRecruited != 2 {
		t.Errorf("player recruits = %d, want 2", s.Factions[0].UnitsRecruited)
	}
	if s.Factions[0].GoldSpent != 35 {
		t.Errorf("player gold spent = %d, want 35", s.Factions[0].GoldSpent)
	}
}

// TestMatchStatsAccumulates verifies that multiple events accumulate correctly
// across factions without cross-talk.
func TestMatchStatsAccumulates(t *testing.T) {
	s := NewMatchStats()
	// Player kills 3 enemy units, loses 1
	s.RecordKill(0, 1, false, 5)
	s.RecordKill(0, 1, false, 5)
	s.RecordKill(0, 1, false, 8)
	s.RecordKill(1, 0, false, 0)
	// Enemy recruits 2 units
	s.RecordRecruit(1, 15)
	s.RecordRecruit(1, 25)

	if s.Factions[0].Kills != 3 {
		t.Errorf("player kills = %d, want 3", s.Factions[0].Kills)
	}
	if s.Factions[0].GoldEarned != 18 {
		t.Errorf("player gold earned = %d, want 18", s.Factions[0].GoldEarned)
	}
	if s.Factions[1].Deaths != 3 {
		t.Errorf("enemy deaths = %d, want 3", s.Factions[1].Deaths)
	}
	if s.Factions[1].Kills != 1 {
		t.Errorf("enemy kills = %d, want 1", s.Factions[1].Kills)
	}
	if s.Factions[0].Deaths != 1 {
		t.Errorf("player deaths = %d, want 1", s.Factions[0].Deaths)
	}
	if s.Factions[1].UnitsRecruited != 2 {
		t.Errorf("enemy recruits = %d, want 2", s.Factions[1].UnitsRecruited)
	}
	if s.Factions[1].GoldSpent != 40 {
		t.Errorf("enemy gold spent = %d, want 40", s.Factions[1].GoldSpent)
	}
}

// TestMatchStatsIgnoresNoKiller verifies that deaths without a killer
// (faction 0xFF) still count as deaths for the dead faction but don't
// award kills or gold to anyone.
func TestMatchStatsIgnoresNoKiller(t *testing.T) {
	s := NewMatchStats()
	s.RecordKill(0xFF, 0, false, 0)
	if s.Factions[0].Deaths != 1 {
		t.Errorf("player deaths = %d, want 1", s.Factions[0].Deaths)
	}
	if s.Factions[0].Kills != 0 {
		t.Errorf("player kills = %d, want 0 (no killer)", s.Factions[0].Kills)
	}
	if s.Factions[1].Kills != 0 {
		t.Errorf("enemy kills = %d, want 0 (no killer)", s.Factions[1].Kills)
	}
}
