package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

func setupRecruitWorld() (*ecs.World, *ecs.EntityManager, *RecruitmentSystem,
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
	*ecs.ComponentPool[component.BoidComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	ecs.Entity,
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()

	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)

	recruitSys := &RecruitmentSystem{}
	w.AddSystem(recruitSys)
	w.Init()

	cmd := em.Create()
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})
	attackPool.Add(cmd, component.AttackComponent{Damage: 10, Range: fixed.FromFloat(5.0), Cooldown: 2})
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(10.0), Y: fixed.FromFloat(10.0)})
	velPool.Add(cmd, component.VelocityComponent{})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander, NeighborRange: fixed.FromFloat(5.0)})
	movePool.Add(cmd, component.MovementComponent{})
	pathPool.Add(cmd, component.PathfindingComponent{})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(cmd, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight, Level: 1})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})

	return w, em, recruitSys, healthPool, unitTypePool, boidPool, ownerPool, cmd
}

func TestRecruitLightInfantry(t *testing.T) {
	w, em, recruitSys, _, unitTypePool, boidPool, ownerPool, cmd := setupRecruitWorld()

	// Set gold so recruit can afford it
	recruitSys.PlayerGold = map[uint32]int32{1: 100}

	recruitSys.Recruit(RecruitRequest{
		CommanderEntity: cmd,
		UnitType:        component.UnitLightInfantry,
	})

	w.Tick(1)

	// Should have created a new entity (cmd=1, new=2)
	newEntity := ecs.Entity(2)
	ut, ok := unitTypePool.Get(newEntity)
	if !ok {
		t.Fatal("recruited unit should exist")
	}
	if ut.Type != component.UnitLightInfantry {
		t.Errorf("recruited type = %d, want LightInfantry", ut.Type)
	}
	if ut.Level != 1 {
		t.Errorf("recruited level = %d, want 1", ut.Level)
	}

	bc, ok := boidPool.Get(newEntity)
	if !ok {
		t.Fatal("recruited unit should have boid component")
	}
	if bc.SquadID != 1 {
		t.Errorf("squad ID = %d, want 1", bc.SquadID)
	}

	// Owner should be copied from commander
	owner, ok := ownerPool.Get(newEntity)
	if !ok {
		t.Fatal("recruited unit should have owner")
	}
	if owner.Faction != component.FactionPlayer {
		t.Errorf("faction = %d, want player", owner.Faction)
	}

	// Gold should be deducted
	if recruitSys.GoldDeductions[1] != 15 {
		t.Errorf("gold deduction = %d, want 15", recruitSys.GoldDeductions[1])
	}

	_ = em // suppress unused warning
}

func TestRecruitNoGold(t *testing.T) {
	w, _, recruitSys, _, unitTypePool, _, _, cmd := setupRecruitWorld()

	// Player has no gold
	recruitSys.PlayerGold = map[uint32]int32{1: 0}

	recruitSys.Recruit(RecruitRequest{
		CommanderEntity: cmd,
		UnitType:        component.UnitLightInfantry,
	})

	w.Tick(1)

	// Should NOT have created a new entity (no gold)
	newEntity := ecs.Entity(2)
	if _, ok := unitTypePool.Get(newEntity); ok {
		t.Error("should not recruit when player has no gold")
	}
	_ = w
}

func TestRecruitBudgetCap(t *testing.T) {
	w, em, recruitSys, _, _, _, _, cmd := setupRecruitWorld()

	// Commander Level=1 → budget = 5 + 1*2 = 7 cost points
	// Recruit enough LI (cost 1 each) to fill budget
	recruitSys.PlayerGold = map[uint32]int32{1: 500}

	// Recruit 8 LI (8 cost, exceeds budget of 7)
	for i := 0; i < 8; i++ {
		recruitSys.Recruit(RecruitRequest{
			CommanderEntity: cmd,
			UnitType:        component.UnitLightInfantry,
		})
	}

	w.Tick(1)

	// Count squad members
	count := 0
	recruitSys.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == 1 && bc.Role != component.RoleCommander {
			count++
		}
	})

	// Budget = 7, each LI costs 1, so max 7 units
	if count > 7 {
		t.Errorf("squad size = %d, should be capped at budget 7", count)
	}
	if count < 1 {
		t.Error("should have recruited at least 1 unit")
	}
	_ = em
}

func TestRecruitSquadSizeLimit(t *testing.T) {
	w, em, recruitSys, _, _, _, _, cmd := setupRecruitWorld()

	// Set gold high enough for many recruits
	recruitSys.PlayerGold = map[uint32]int32{1: 500}

	// Recruit 12 units (should fill up at cost budget)
	for i := 0; i < 12; i++ {
		recruitSys.Recruit(RecruitRequest{
			CommanderEntity: cmd,
			UnitType:        component.UnitLightInfantry,
		})
	}

	w.Tick(1)

	// Count squad members
	count := 0
	recruitSys.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == 1 && bc.Role != component.RoleCommander {
			count++
		}
	})

	if count > 12 {
		t.Errorf("squad size = %d, should be capped at max", count)
	}
	if count < 1 {
		t.Error("should have recruited at least 1 unit")
	}
	_ = em
}

func TestKillBounty(t *testing.T) {
	tests := []struct {
		unitType component.CombatUnitType
		want     int32
	}{
		{component.UnitLightInfantry, 12},  // 15 * 80% = 12
		{component.UnitHeavyInfantry, 20},  // 25 * 80% = 20
		{component.UnitSniper, 40},         // 50 * 80% = 40
		{component.UnitAntiArmorInfantry, 24}, // 30 * 80% = 24
	}

	for _, tt := range tests {
		got := KillBounty(tt.unitType)
		if got != tt.want {
			t.Errorf("KillBounty(%v) = %d, want %d", tt.unitType, got, tt.want)
		}
	}
}

func TestWeaponCategory(t *testing.T) {
	tests := []struct {
		weapon component.WeaponType
		want   string
	}{
		{component.WeaponGun, "Light"},
		{component.WeaponSniper, "Light"},
		{component.WeaponCannon, "Heavy"},
		{component.WeaponMissile, "Heavy"},
	}
	for _, tt := range tests {
		got := weaponCategory(tt.weapon)
		if got != tt.want {
			t.Errorf("weaponCategory(%v) = %q, want %q", tt.weapon, got, tt.want)
		}
	}
}
