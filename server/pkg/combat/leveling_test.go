package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

func setupLevelingWorld() (*ecs.World, *ecs.EntityManager,
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.AttackComponent],
	*ecs.ComponentPool[component.KillPointsComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
	*ecs.ComponentPool[component.BoidComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	kpPool := ecs.NewComponentPool[component.KillPointsComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.KillPointsComponent{}, kpPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	w.AddSystem(&LevelingSystem{})
	w.Init()

	return w, em, healthPool, attackPool, kpPool, unitTypePool, boidPool
}

func TestLevelingCombatUnitOneLevel(t *testing.T) {
	w, em, healthPool, _, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	e := em.Create()
	healthPool.Add(e, component.HealthComponent{HP: 100, MaxHP: 100})
	kpPool.Add(e, component.KillPointsComponent{Points: 3})
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleMelee})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 2 {
		t.Errorf("expected level 2 after 3 kill points (threshold=2), got %d", ut.Level)
	}

	kp, _ := kpPool.Get(e)
	if kp.Points != 1 {
		t.Errorf("expected 1 remaining kill point, got %d", kp.Points)
	}

	hp, _ := healthPool.Get(e)
	if hp.MaxHP <= 100 {
		t.Errorf("expected MaxHP > 100 after level up, got %d", hp.MaxHP)
	}
}

func TestLevelingCombatUnitMaxLevel6(t *testing.T) {
	w, em, healthPool, _, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	e := em.Create()
	healthPool.Add(e, component.HealthComponent{HP: 100, MaxHP: 100})
	kpPool.Add(e, component.KillPointsComponent{Points: 100}) // way more than needed
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleMelee})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 6 {
		t.Errorf("combat unit should cap at level 6, got %d", ut.Level)
	}
}

func TestLevelingCombatUnitThresholds(t *testing.T) {
	w, em, _, _, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	// Level 5->6 needs 32 points
	e := em.Create()
	kpPool.Add(e, component.KillPointsComponent{Points: 31})
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 5})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleMelee})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 5 {
		t.Errorf("should not level up with 31 points (need 32), got level %d", ut.Level)
	}
}

func TestLevelingCommanderLevelUp(t *testing.T) {
	w, em, healthPool, attackPool, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	e := em.Create()
	healthPool.Add(e, component.HealthComponent{HP: 200, MaxHP: 200})
	attackPool.Add(e, component.AttackComponent{Damage: 10})
	kpPool.Add(e, component.KillPointsComponent{Points: 6}) // threshold for lv2 is 4
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleCommander})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 2 {
		t.Errorf("commander should reach level 2, got %d", ut.Level)
	}

	// Commander HP multiplier at level 2 = 140%
	typeStats := component.CombatUnitTypeTable[component.UnitLightInfantry]
	expectedHP := typeStats.HP * 140 / 100
	hp, _ := healthPool.Get(e)
	if hp.MaxHP != expectedHP {
		t.Errorf("commander MaxHP = %d, want %d", hp.MaxHP, expectedHP)
	}

	expectedDmg := typeStats.Damage * 120 / 100
	ac, _ := attackPool.Get(e)
	if ac.Damage != expectedDmg {
		t.Errorf("commander Damage = %d, want %d", ac.Damage, expectedDmg)
	}
}

func TestLevelingCommanderMaxLevel10(t *testing.T) {
	w, em, healthPool, attackPool, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	e := em.Create()
	healthPool.Add(e, component.HealthComponent{HP: 200, MaxHP: 200})
	attackPool.Add(e, component.AttackComponent{Damage: 10})
	kpPool.Add(e, component.KillPointsComponent{Points: 500}) // way more than needed
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleCommander})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 10 {
		t.Errorf("commander should cap at level 10, got %d", ut.Level)
	}

	// Commander HP multiplier at level 10 = 500%
	typeStats := component.CombatUnitTypeTable[component.UnitLightInfantry]
	expectedHP := typeStats.HP * 500 / 100
	hp, _ := healthPool.Get(e)
	if hp.MaxHP != expectedHP {
		t.Errorf("commander MaxHP at lv10 = %d, want %d", hp.MaxHP, expectedHP)
	}
}

func TestLevelingNoPointsNoLevel(t *testing.T) {
	w, em, _, _, kpPool, unitTypePool, boidPool := setupLevelingWorld()

	e := em.Create()
	kpPool.Add(e, component.KillPointsComponent{Points: 0})
	unitTypePool.Add(e, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})
	boidPool.Add(e, component.BoidComponent{Role: component.RoleMelee})

	w.Tick(1)

	ut, _ := unitTypePool.Get(e)
	if ut.Level != 1 {
		t.Errorf("should not level up with 0 points, got level %d", ut.Level)
	}
}
