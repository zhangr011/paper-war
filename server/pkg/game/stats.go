package game

// FactionStats holds cumulative match statistics for one faction.
// Factions are indexed by FactionPlayer (0) and FactionEnemy (1).
type FactionStats struct {
	Kills          uint16 // enemy units killed by this faction
	Deaths         uint16 // units lost
	CommanderKills uint16 // enemy commanders slain
	UnitsRecruited uint16 // recruits completed
	GoldEarned     int32  // total bounty gold earned
	GoldSpent      int32  // total gold spent on recruits
}

// MatchStats accumulates per-faction statistics across an entire match.
// It is fed by the game session each tick from DeathSystem and RecruitmentSystem
// outputs, then snapshot into a wire payload at match end.
type MatchStats struct {
	Factions [2]FactionStats
}

// NoKiller is used as the killerFaction argument to RecordKill when a unit
// dies without an attacker (despawn, suicide, map hazard).
const NoKiller uint8 = 0xFF

// NewMatchStats returns a zeroed MatchStats ready for a new match.
func NewMatchStats() *MatchStats {
	return &MatchStats{}
}

// RecordKill attributes a kill to the killer's faction and a death to the
// dead unit's faction. If killerFaction is NoKiller, only the death is recorded.
// Bounty gold is credited to the killer's faction. If deadCommander is true,
// the kill counts as a commander kill.
func (s *MatchStats) RecordKill(killerFaction, deadFaction uint8, deadCommander bool, bounty int32) {
	if deadFaction < 2 {
		s.Factions[deadFaction].Deaths++
	}
	if killerFaction != NoKiller && killerFaction < 2 {
		s.Factions[killerFaction].Kills++
		if bounty > 0 {
			s.Factions[killerFaction].GoldEarned += bounty
		}
		if deadCommander {
			s.Factions[killerFaction].CommanderKills++
		}
	}
}

// RecordRecruit credits a completed recruit to the faction.
func (s *MatchStats) RecordRecruit(faction uint8, cost int32) {
	if faction < 2 {
		s.Factions[faction].UnitsRecruited++
		s.Factions[faction].GoldSpent += cost
	}
}

// AddRecruits credits multiple recruits at once (batch version for tick loop).
func (s *MatchStats) AddRecruits(faction uint8, count uint16, totalCost int32) {
	if faction < 2 {
		s.Factions[faction].UnitsRecruited += count
		s.Factions[faction].GoldSpent += totalCost
	}
}
