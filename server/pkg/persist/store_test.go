package persist

import (
	"context"
	"testing"
)

func TestMockStoreFindOrCreateNewPlayer(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, err := store.FindOrCreatePlayer(ctx, "token-abc", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Token != "token-abc" {
		t.Errorf("token = %q, want %q", p.Token, "token-abc")
	}
	if len(p.Commanders) != 1 {
		t.Fatalf("new player should have 1 starter commander, got %d", len(p.Commanders))
	}

	cmd := p.Commanders[0]
	if cmd.Type != "LightInfantry" {
		t.Errorf("starter commander type = %q, want LightInfantry", cmd.Type)
	}
	if cmd.Gold != 50 {
		t.Errorf("starter commander gold = %d, want 50", cmd.Gold)
	}
	if len(cmd.Units) != 5 {
		t.Errorf("starter commander units = %d, want 5", len(cmd.Units))
	}
	for i, u := range cmd.Units {
		if u.Type != "LightInfantry" {
			t.Errorf("unit %d type = %q, want LightInfantry", i, u.Type)
		}
	}
}

func TestMockStoreFindOrCreateReturnsExisting(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p1, _ := store.FindOrCreatePlayer(ctx, "token-xyz", "")
	p2, _ := store.FindOrCreatePlayer(ctx, "token-xyz", "")

	if p1.ID != p2.ID {
		t.Errorf("same token should return same player, got %d and %d", p1.ID, p2.ID)
	}
}

func TestMockStoreSaveCommander(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, _ := store.FindOrCreatePlayer(ctx, "token-save", "")

	// Update commander gold
	cmd := p.Commanders[0]
	cmd.Gold = 30
	cmd.Units = cmd.Units[:3] // lost 2 units

	err := store.SaveCommander(ctx, p.ID, cmd)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := store.LoadRoster(ctx, p.ID)
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

func TestMockStoreDeleteCommander(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, _ := store.FindOrCreatePlayer(ctx, "token-del", "")

	err := store.DeleteCommander(ctx, p.ID, p.Commanders[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := store.LoadRoster(ctx, p.ID)
	if len(roster) != 0 {
		t.Errorf("roster should be empty after deletion, got %d commanders", len(roster))
	}
}

func TestMockStoreAddSecondCommander(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, _ := store.FindOrCreatePlayer(ctx, "token-add", "")

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

	err := store.SaveCommander(ctx, p.ID, cmd2)
	if err != nil {
		t.Fatal(err)
	}

	roster, _ := store.LoadRoster(ctx, p.ID)
	if len(roster) != 2 {
		t.Fatalf("roster should have 2 commanders, got %d", len(roster))
	}
	if roster[1].Type != "HeavyInfantry" {
		t.Errorf("second commander type = %q, want HeavyInfantry", roster[1].Type)
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	cmd := Commander{
		ID:    1,
		Name:  "Test",
		Type:  "Sniper",
		Level: 5,
		Gold:  200,
		Formation: FormationTemplate{
			WeaponSlot:   "Light",
			ArmorSlot:    "Light",
			LeadingSkill: 80,
		},
		Units: []CombatUnit{
			{ID: 1, Type: "Sniper", Level: 3, KillPoints: 15},
			{ID: 2, Type: "LightInfantry", Level: 2},
		},
	}

	data, err := MarshalCommander(cmd)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalCommander(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != cmd.ID || got.Name != cmd.Name || got.Type != cmd.Type {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, cmd)
	}
	if got.Gold != cmd.Gold {
		t.Errorf("gold = %d, want %d", got.Gold, cmd.Gold)
	}
	if len(got.Units) != len(cmd.Units) {
		t.Errorf("units count = %d, want %d", len(got.Units), len(cmd.Units))
	}
	if got.Units[0].KillPoints != 15 {
		t.Errorf("unit 0 kill points = %d, want 15", got.Units[0].KillPoints)
	}
}

func TestStarterRosterFormationTemplate(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, _ := store.FindOrCreatePlayer(ctx, "token-formation", "")
	cmd := p.Commanders[0]

	if cmd.Formation.WeaponSlot != "Light" {
		t.Errorf("starter weapon slot = %q, want Light", cmd.Formation.WeaponSlot)
	}
	if cmd.Formation.ArmorSlot != "Light" {
		t.Errorf("starter armor slot = %q, want Light", cmd.Formation.ArmorSlot)
	}
	if cmd.Formation.LeadingSkill == 0 {
		t.Error("starter leading skill should not be 0")
	}
}

func TestSaveCommanderPersistsFormationAndCombatUnits(t *testing.T) {
	store := NewMockStore()
	ctx := context.Background()

	p, _ := store.FindOrCreatePlayer(ctx, "token-jsonb", "")

	// Save a commander with non-trivial formation + mixed combat units
	cmd := Commander{
		ID:    p.Commanders[0].ID,
		Name:  "Veteran Commander",
		Type:  "Sniper",
		Level: 5,
		Gold:  200,
		Formation: FormationTemplate{
			WeaponSlot:   "Light",
			ArmorSlot:    "Light",
			LeadingSkill: 150,
		},
		Units: []CombatUnit{
			{ID: 1, Type: "Sniper", Level: 4, KillPoints: 30},
			{ID: 2, Type: "HeavyInfantry", Level: 3, KillPoints: 10},
			{ID: 3, Type: "MissileArtillery", Level: 2, KillPoints: 5},
		},
	}

	err := store.SaveCommander(ctx, p.ID, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Load and verify round-trip
	roster, _ := store.LoadRoster(ctx, p.ID)
	if len(roster) != 1 {
		t.Fatalf("expected 1 commander, got %d", len(roster))
	}

	saved := roster[0]
	if saved.Name != "Veteran Commander" {
		t.Errorf("name = %q, want %q", saved.Name, "Veteran Commander")
	}
	if saved.Formation.WeaponSlot != "Light" {
		t.Errorf("formation weapon = %q, want Light", saved.Formation.WeaponSlot)
	}
	if saved.Formation.LeadingSkill != 150 {
		t.Errorf("formation leading_skill = %d, want 150", saved.Formation.LeadingSkill)
	}
	if len(saved.Units) != 3 {
		t.Fatalf("expected 3 combat units, got %d", len(saved.Units))
	}
	if saved.Units[0].Type != "Sniper" || saved.Units[0].Level != 4 || saved.Units[0].KillPoints != 30 {
		t.Errorf("unit[0] = %+v, want Sniper/4/30", saved.Units[0])
	}
	if saved.Units[2].Type != "MissileArtillery" {
		t.Errorf("unit[2] type = %q, want MissileArtillery", saved.Units[2].Type)
	}
}

func TestStoreInterfaceTypeSafety(t *testing.T) {
	// Verify MockStore satisfies Store interface at compile time
	var _ Store = NewMockStore()
}