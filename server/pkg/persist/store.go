package persist

import (
	"context"
	"encoding/json"
	"fmt"
)

// CombatUnit represents a single combat unit in a Commander's roster.
type CombatUnit struct {
	ID       uint8  `json:"id"`
	Type     string `json:"type"`     // CombatUnitType name
	Level    uint8  `json:"level"`
	KillPoints int32 `json:"kill_points"`
}

// FormationTemplate defines the weapon+armor slots and Leading Skill budget.
type FormationTemplate struct {
	WeaponSlot string  `json:"weapon_slot"` // "Light" or "Heavy"
	ArmorSlot  string  `json:"armor_slot"`
	LeadingSkill int32 `json:"leading_skill"` // Gold budget
}

// Commander represents a Commander in the roster.
type Commander struct {
	ID         uint8            `json:"id"`
	Name       string           `json:"name"`
	Type       string           `json:"type"`       // CombatUnitType name
	Level      uint8            `json:"level"`
	Gold       int32            `json:"gold"`
	Formation  FormationTemplate `json:"formation"`
	Units      []CombatUnit     `json:"units"`
}

// Player represents a player's persistent data.
type Player struct {
	ID         uint32
	Token      string
	Commanders []Commander
}

// Store is the persistence interface. Implemented by PostgresStore for production
// and MockStore for testing.
type Store interface {
	// FindOrCreatePlayer looks up a player by token, creating a new one if not found.
	// New players get a starter roster: 1 Gun Commander + 5 LI, 50 Gold.
	FindOrCreatePlayer(ctx context.Context, token string) (*Player, error)

	// LoadRoster loads all commanders and units for a player.
	LoadRoster(ctx context.Context, playerID uint32) ([]Commander, error)

	// SaveCommander upserts a single commander's data (formation, units, gold, level).
	SaveCommander(ctx context.Context, playerID uint32, cmd Commander) error

	// DeleteCommander removes a commander from the roster (permadeath).
	DeleteCommander(ctx context.Context, playerID uint32, cmdID uint8) error

	// CreateStarterRoster creates the initial roster for a new player:
	// 1 Gun Commander + 5 Light Infantry, 50 Gold.
	CreateStarterRoster(ctx context.Context, playerID uint32) error

	// GetCareerStats returns the cumulative cross-match stats for a player.
	// Returns a zero-valued CareerStats (PlayerID set) for players with no
	// recorded matches yet — does NOT return an error for unknown IDs, so
	// callers can render an empty career UI without special-casing.
	GetCareerStats(ctx context.Context, playerID uint32) (*CareerStats, error)

	// AddCareerStats atomically accumulates a delta into the player's career
	// totals. Called once per match end with that match's stats. For new
	// players, creates the career row.
	AddCareerStats(ctx context.Context, playerID uint32, delta CareerStats) error
}

// --- MockStore for testing ---

type MockStore struct {
	Players map[uint32]*Player
	ByToken map[string]*Player
	Careers map[uint32]*CareerStats
	nextID  uint32
}

func NewMockStore() *MockStore {
	return &MockStore{
		Players: make(map[uint32]*Player),
		ByToken: make(map[string]*Player),
		Careers: make(map[uint32]*CareerStats),
		nextID:  1,
	}
}

func (s *MockStore) FindOrCreatePlayer(ctx context.Context, token string) (*Player, error) {
	if p, ok := s.ByToken[token]; ok {
		return p, nil
	}

	p := &Player{
		ID:    s.nextID,
		Token: token,
	}
	s.nextID++
	s.Players[p.ID] = p
	s.ByToken[token] = p

	if err := s.CreateStarterRoster(ctx, p.ID); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *MockStore) LoadRoster(ctx context.Context, playerID uint32) ([]Commander, error) {
	p, ok := s.Players[playerID]
	if !ok {
		return nil, fmt.Errorf("player %d not found", playerID)
	}
	return p.Commanders, nil
}

func (s *MockStore) SaveCommander(ctx context.Context, playerID uint32, cmd Commander) error {
	p, ok := s.Players[playerID]
	if !ok {
		return fmt.Errorf("player %d not found", playerID)
	}
	for i, c := range p.Commanders {
		if c.ID == cmd.ID {
			p.Commanders[i] = cmd
			return nil
		}
	}
	p.Commanders = append(p.Commanders, cmd)
	return nil
}

func (s *MockStore) DeleteCommander(ctx context.Context, playerID uint32, cmdID uint8) error {
	p, ok := s.Players[playerID]
	if !ok {
		return fmt.Errorf("player %d not found", playerID)
	}
	for i, c := range p.Commanders {
		if c.ID == cmdID {
			p.Commanders = append(p.Commanders[:i], p.Commanders[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("commander %d not found", cmdID)
}

func (s *MockStore) CreateStarterRoster(ctx context.Context, playerID uint32) error {
	p, ok := s.Players[playerID]
	if !ok {
		return fmt.Errorf("player %d not found", playerID)
	}

	units := make([]CombatUnit, 5)
	for i := range units {
		units[i] = CombatUnit{
			ID:    uint8(i + 1),
			Type:  "LightInfantry",
			Level: 1,
		}
	}

	cmd := Commander{
		ID:    1,
		Name:  "Starter Commander",
		Type:  "LightInfantry",
		Level: 1,
		Gold:  50,
		Formation: FormationTemplate{
			WeaponSlot:   "Light",
			ArmorSlot:    "Light",
			LeadingSkill: 100,
		},
		Units: units,
	}

	p.Commanders = append(p.Commanders, cmd)
	return nil
}

// GetCareerStats returns the player's career totals, or a zero-valued
// CareerStats for players who haven't played any matches yet.
func (s *MockStore) GetCareerStats(ctx context.Context, playerID uint32) (*CareerStats, error) {
	if c, ok := s.Careers[playerID]; ok {
		// Return a copy so callers can't mutate our stored value.
		out := *c
		return &out, nil
	}
	return &CareerStats{PlayerID: playerID}, nil
}

// AddCareerStats accumulates delta into the player's career totals.
// Creates the career entry on first call.
func (s *MockStore) AddCareerStats(ctx context.Context, playerID uint32, delta CareerStats) error {
	c, ok := s.Careers[playerID]
	if !ok {
		c = &CareerStats{PlayerID: playerID}
		s.Careers[playerID] = c
	}
	c.Add(delta)
	return nil
}

// Helper: marshal commander to JSON (for PostgresStore JSONB column)
func MarshalCommander(cmd Commander) ([]byte, error) {
	return json.Marshal(cmd)
}

// Helper: unmarshal commander from JSON
func UnmarshalCommander(data []byte) (Commander, error) {
	var cmd Commander
	err := json.Unmarshal(data, &cmd)
	return cmd, err
}
