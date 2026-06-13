//go:build pgx

package persist

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Println("Skipping PostgresStore tests: DATABASE_URL not set")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func newTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	ctx := context.Background()
	store, err := NewPostgresStore(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Clean tables for isolation
	_, err = store.pool.Exec(ctx, `DELETE FROM commanders; DELETE FROM players`)
	if err != nil {
		t.Fatalf("clean test data: %v", err)
	}

	return store
}

func TestPostgresFindOrCreateNewPlayer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.FindOrCreatePlayer(ctx, "token-abc")
	if err != nil {
		t.Fatal(err)
	}
	if p.Token != "token-abc" {
		t.Errorf("token = %q, want %q", p.Token, "token-abc")
	}
	if p.ID == 0 {
		t.Error("player ID should not be 0")
	}
	if len(p.Commanders) != 1 {
		t.Fatalf("new player should have 1 starter commander, got %d", len(p.Commanders))
	}

	cmd := p.Commanders[0]
	if cmd.Type != "LightInfantry" {
		t.Errorf("starter type = %q, want LightInfantry", cmd.Type)
	}
	if cmd.Gold != 50 {
		t.Errorf("starter gold = %d, want 50", cmd.Gold)
	}
	if len(cmd.Units) != 5 {
		t.Errorf("starter units = %d, want 5", len(cmd.Units))
	}
	if cmd.Formation.WeaponSlot != "Light" {
		t.Errorf("starter weapon slot = %q, want Light", cmd.Formation.WeaponSlot)
	}
}

func TestPostgresFindOrCreateReturnsExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p1, _ := s.FindOrCreatePlayer(ctx, "token-xyz")
	p2, _ := s.FindOrCreatePlayer(ctx, "token-xyz")

	if p1.ID != p2.ID {
		t.Errorf("same token should return same player, got %d and %d", p1.ID, p2.ID)
	}
}

func TestPostgresSaveCommander(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, _ := s.FindOrCreatePlayer(ctx, "token-save")

	cmd := p.Commanders[0]
	cmd.Gold = 30
	cmd.Units = cmd.Units[:3] // lost 2 units

	err := s.SaveCommander(ctx, p.ID, cmd)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := s.LoadRoster(ctx, p.ID)
	if len(roster) != 1 {
		t.Fatalf("roster should have 1 commander, got %d", len(roster))
	}
	if roster[0].Gold != 30 {
		t.Errorf("gold = %d, want 30", roster[0].Gold)
	}
	if len(roster[0].Units) != 3 {
		t.Errorf("units = %d, want 3", len(roster[0].Units))
	}
}

func TestPostgresDeleteCommander(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, _ := s.FindOrCreatePlayer(ctx, "token-del")

	err := s.DeleteCommander(ctx, p.ID, p.Commanders[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := s.LoadRoster(ctx, p.ID)
	if len(roster) != 0 {
		t.Errorf("roster should be empty after deletion, got %d commanders", len(roster))
	}
}

func TestPostgresAddSecondCommander(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, _ := s.FindOrCreatePlayer(ctx, "token-add")

	cmd2 := Commander{
		ID:    2,
		Name:  "Second Commander",
		Type:  "HeavyInfantry",
		Level: 1,
		Gold:  0,
		Formation: FormationTemplate{
			WeaponSlot:   "Heavy",
			ArmorSlot:    "Heavy",
			LeadingSkill: 150,
		},
		Units: []CombatUnit{},
	}

	err := s.SaveCommander(ctx, p.ID, cmd2)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := s.LoadRoster(ctx, p.ID)
	if len(roster) != 2 {
		t.Fatalf("roster should have 2 commanders, got %d", len(roster))
	}
	if roster[1].Type != "HeavyInfantry" {
		t.Errorf("second commander type = %q, want HeavyInfantry", roster[1].Type)
	}
}

func TestPostgresSaveCommanderUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, _ := s.FindOrCreatePlayer(ctx, "token-upsert")

	// Save same commander with updated fields
	cmd := p.Commanders[0]
	cmd.Gold = 999
	cmd.Level = 5
	cmd.Units = []CombatUnit{
		{ID: 1, Type: "Sniper", Level: 4, KillPoints: 30},
	}

	err := s.SaveCommander(ctx, p.ID, cmd)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := s.LoadRoster(ctx, p.ID)
	if len(roster) != 1 {
		t.Fatalf("upsert should keep 1 commander, got %d", len(roster))
	}
	if roster[0].Gold != 999 {
		t.Errorf("gold = %d, want 999", roster[0].Gold)
	}
	if roster[0].Level != 5 {
		t.Errorf("level = %d, want 5", roster[0].Level)
	}
	if len(roster[0].Units) != 1 {
		t.Fatalf("units = %d, want 1", len(roster[0].Units))
	}
	if roster[0].Units[0].KillPoints != 30 {
		t.Errorf("unit[0].KillPoints = %d, want 30", roster[0].Units[0].KillPoints)
	}
}

func TestPostgresFormationRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, _ := s.FindOrCreatePlayer(ctx, "token-formation")
	cmd := p.Commanders[0]
	cmd.Formation = FormationTemplate{
		WeaponSlot:   "Heavy",
		ArmorSlot:    "Light",
		LeadingSkill: 200,
	}

	s.SaveCommander(ctx, p.ID, cmd)

	roster, _ := s.LoadRoster(ctx, p.ID)
	if roster[0].Formation.WeaponSlot != "Heavy" {
		t.Errorf("weapon = %q, want Heavy", roster[0].Formation.WeaponSlot)
	}
	if roster[0].Formation.ArmorSlot != "Light" {
		t.Errorf("armor = %q, want Light", roster[0].Formation.ArmorSlot)
	}
	if roster[0].Formation.LeadingSkill != 200 {
		t.Errorf("leading_skill = %d, want 200", roster[0].Formation.LeadingSkill)
	}
}

func TestPostgresStoreSatisfiesInterface(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	// Verify PostgresStore satisfies Store interface at compile time
	var _ Store = &PostgresStore{}
}
