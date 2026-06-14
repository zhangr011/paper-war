package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestDeathSystemRemovesDeadEntity(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Create entity with 0 HP
	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(1.0), Y: fixed.FromFloat(1.0)})
	velPool.Add(e, component.VelocityComponent{})
	healthPool.Add(e, component.HealthComponent{HP: 0, MaxHP: 100})
	attackPool.Add(e, component.AttackComponent{Damage: 10})
	boidPool.Add(e, component.BoidComponent{SquadID: 1})
	movePool.Add(e, component.MovementComponent{})
	pathPool.Add(e, component.PathfindingComponent{})

	w.Tick(1)

	// Entity should be gone from all pools
	if _, ok := healthPool.Get(e); ok {
		t.Error("dead entity should be removed from health pool")
	}
	if _, ok := posPool.Get(e); ok {
		t.Error("dead entity should be removed from position pool")
	}
	if em.Alive(e) {
		t.Error("dead entity should be destroyed")
	}
}

func TestDeathSystemSparesLivingEntity(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	e := em.Create()
	healthPool.Add(e, component.HealthComponent{HP: 50, MaxHP: 100})
	posPool.Add(e, component.PositionComponent{})
	velPool.Add(e, component.VelocityComponent{})
	boidPool.Add(e, component.BoidComponent{})
	attackPool.Add(e, component.AttackComponent{})
	movePool.Add(e, component.MovementComponent{})
	pathPool.Add(e, component.PathfindingComponent{})

	w.Tick(1)

	if _, ok := healthPool.Get(e); !ok {
		t.Error("living entity should remain in health pool")
	}
	if !em.Alive(e) {
		t.Error("living entity should still be alive")
	}
}

func TestDeathSystemClearsAttackTarget(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Victim: dead
	victim := em.Create()
	posPool.Add(victim, component.PositionComponent{})
	velPool.Add(victim, component.VelocityComponent{})
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 100})
	boidPool.Add(victim, component.BoidComponent{})
	attackPool.Add(victim, component.AttackComponent{})
	movePool.Add(victim, component.MovementComponent{})
	pathPool.Add(victim, component.PathfindingComponent{})

	// Attacker targeting the victim
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{})
	velPool.Add(attacker, component.VelocityComponent{})
	healthPool.Add(attacker, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(attacker, component.AttackComponent{TargetID: uint32(victim)})
	boidPool.Add(attacker, component.BoidComponent{})
	movePool.Add(attacker, component.MovementComponent{})
	pathPool.Add(attacker, component.PathfindingComponent{})

	w.Tick(1)

	ac, _ := attackPool.Get(attacker)
	if ac.TargetID != 0 {
		t.Errorf("attacker target should be cleared after victim death, got %d", ac.TargetID)
	}
}

func TestDeathSystemCommanderPromotion(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Commander with 0 HP
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{})
	velPool.Add(cmd, component.VelocityComponent{})
	healthPool.Add(cmd, component.HealthComponent{HP: 0, MaxHP: 200})
	attackPool.Add(cmd, component.AttackComponent{})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 5, Role: component.RoleCommander, NeighborRange: fixed.FromFloat(5.0)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 5, IsAlive: true})
	movePool.Add(cmd, component.MovementComponent{})
	pathPool.Add(cmd, component.PathfindingComponent{})
	unitTypePool.Add(cmd, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 3})

	// Low-level squad member
	lowUnit := em.Create()
	posPool.Add(lowUnit, component.PositionComponent{})
	velPool.Add(lowUnit, component.VelocityComponent{})
	healthPool.Add(lowUnit, component.HealthComponent{HP: 50, MaxHP: 80})
	attackPool.Add(lowUnit, component.AttackComponent{})
	boidPool.Add(lowUnit, component.BoidComponent{SquadID: 5, Role: component.RoleMelee})
	movePool.Add(lowUnit, component.MovementComponent{})
	pathPool.Add(lowUnit, component.PathfindingComponent{})
	unitTypePool.Add(lowUnit, component.UnitTypeComponent{Type: component.UnitLightInfantry, Level: 1})

	// High-level squad member — should be promoted
	highUnit := em.Create()
	posPool.Add(highUnit, component.PositionComponent{})
	velPool.Add(highUnit, component.VelocityComponent{})
	healthPool.Add(highUnit, component.HealthComponent{HP: 80, MaxHP: 80})
	attackPool.Add(highUnit, component.AttackComponent{})
	boidPool.Add(highUnit, component.BoidComponent{SquadID: 5, Role: component.RoleRanged})
	movePool.Add(highUnit, component.MovementComponent{})
	pathPool.Add(highUnit, component.PathfindingComponent{})
	unitTypePool.Add(highUnit, component.UnitTypeComponent{Type: component.UnitHeavyInfantry, Level: 4})

	w.Tick(1)

	// Commander should be removed
	if em.Alive(cmd) {
		t.Error("dead commander should be destroyed")
	}

	// Low unit should NOT be promoted
	lowBC, _ := boidPool.Get(lowUnit)
	if lowBC.Role == component.RoleCommander {
		t.Error("low-level unit should not be promoted")
	}

	// High-level unit should be promoted to Commander
	highBC, ok := boidPool.Get(highUnit)
	if !ok {
		t.Fatal("promoted unit should still exist")
	}
	if highBC.Role != component.RoleCommander {
		t.Errorf("high-level unit should be promoted to Commander, got role %d", highBC.Role)
	}

	// Should have CommanderComponent
	promotedCmd, ok := cmdPool.Get(highUnit)
	if !ok {
		t.Fatal("promoted unit should have CommanderComponent")
	}
	if !promotedCmd.IsAlive {
		t.Error("promoted commander should be alive")
	}
	if promotedCmd.SquadID != 5 {
		t.Errorf("promoted commander squad ID = %d, want 5", promotedCmd.SquadID)
	}

	// CombatUnitType should be preserved (types never convert)
	ut, _ := unitTypePool.Get(highUnit)
	if ut.Type != component.UnitHeavyInfantry {
		t.Errorf("promoted unit type should be preserved, got %d", ut.Type)
	}
}

func TestDeathSystemKillPointsAwarded(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	kpPool := ecs.NewComponentPool[component.KillPointsComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.KillPointsComponent{}, kpPool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Killer
	killer := em.Create()
	posPool.Add(killer, component.PositionComponent{})
	velPool.Add(killer, component.VelocityComponent{})
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(killer, component.AttackComponent{})
	boidPool.Add(killer, component.BoidComponent{})
	movePool.Add(killer, component.MovementComponent{})
	pathPool.Add(killer, component.PathfindingComponent{})
	kpPool.Add(killer, component.KillPointsComponent{Points: 0})

	// Victim killed by killer
	victim := em.Create()
	posPool.Add(victim, component.PositionComponent{})
	velPool.Add(victim, component.VelocityComponent{})
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: uint32(killer)})
	attackPool.Add(victim, component.AttackComponent{})
	boidPool.Add(victim, component.BoidComponent{})
	movePool.Add(victim, component.MovementComponent{})
	pathPool.Add(victim, component.PathfindingComponent{})

	w.Tick(1)

	// Killer should have kill points
	kp, ok := kpPool.Get(killer)
	if !ok {
		t.Fatal("killer should still exist")
	}
	if kp.Points < 1 {
		t.Errorf("killer should have kill points >= 1, got %d", kp.Points)
	}

	// Victim should be removed
	if em.Alive(victim) {
		t.Error("victim should be destroyed")
	}
}

func TestDeathSystemCommanderKillBonus(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	kpPool := ecs.NewComponentPool[component.KillPointsComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.KillPointsComponent{}, kpPool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Killer
	killer := em.Create()
	posPool.Add(killer, component.PositionComponent{})
	velPool.Add(killer, component.VelocityComponent{})
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(killer, component.AttackComponent{})
	boidPool.Add(killer, component.BoidComponent{})
	movePool.Add(killer, component.MovementComponent{})
	pathPool.Add(killer, component.PathfindingComponent{})
	kpPool.Add(killer, component.KillPointsComponent{Points: 0})

	// Commander victim
	cmdVictim := em.Create()
	posPool.Add(cmdVictim, component.PositionComponent{})
	velPool.Add(cmdVictim, component.VelocityComponent{})
	healthPool.Add(cmdVictim, component.HealthComponent{HP: 0, MaxHP: 200, LastAttacker: uint32(killer)})
	attackPool.Add(cmdVictim, component.AttackComponent{})
	boidPool.Add(cmdVictim, component.BoidComponent{SquadID: 3})
	cmdPool.Add(cmdVictim, component.CommanderComponent{SquadID: 3, IsAlive: true})
	movePool.Add(cmdVictim, component.MovementComponent{})
	pathPool.Add(cmdVictim, component.PathfindingComponent{})

	w.Tick(1)

	kp, _ := kpPool.Get(killer)
	if kp.Points != 5 {
		t.Errorf("commander kill bonus should be 5 points, got %d", kp.Points)
	}
}
