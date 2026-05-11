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

func TestDeathSystemCommanderDeath(t *testing.T) {
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
	formationRolePool := ecs.NewComponentPool[component.FormationRoleComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.FormationRoleComponent{}, formationRolePool)

	w.AddSystem(&DeathSystem{})
	w.Init()

	// Commander with 0 HP
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{})
	velPool.Add(cmd, component.VelocityComponent{})
	healthPool.Add(cmd, component.HealthComponent{HP: 0, MaxHP: 200})
	attackPool.Add(cmd, component.AttackComponent{})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 5, FormationW: fixed.FromFloat(2.0), SeparationW: fixed.FromFloat(1.5), CohesionW: fixed.FromFloat(0.8)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 5, IsAlive: true})
	movePool.Add(cmd, component.MovementComponent{})
	pathPool.Add(cmd, component.PathfindingComponent{})
	formationRolePool.Add(cmd, component.FormationRoleComponent{})

	// Squad member
	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{})
	velPool.Add(unit, component.VelocityComponent{})
	healthPool.Add(unit, component.HealthComponent{HP: 80, MaxHP: 80})
	attackPool.Add(unit, component.AttackComponent{})
	boidPool.Add(unit, component.BoidComponent{
		SquadID:       5,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(0.8),
		FormationW:    fixed.FromFloat(2.0),
	})
	movePool.Add(unit, component.MovementComponent{})
	pathPool.Add(unit, component.PathfindingComponent{})
	formationRolePool.Add(unit, component.FormationRoleComponent{})

	w.Tick(1)

	// Commander should be removed
	if em.Alive(cmd) {
		t.Error("dead commander should be destroyed")
	}

	// Squad member should still be alive but with adjusted weights
	bc, ok := boidPool.Get(unit)
	if !ok {
		t.Fatal("squad member should still exist")
	}
	if bc.FormationW >= fixed.FromFloat(2.0) {
		t.Errorf("formation weight should decrease after commander death, got %d", bc.FormationW)
	}
	if bc.SeparationW <= fixed.FromFloat(1.5) {
		t.Errorf("separation weight should increase after commander death, got %d", bc.SeparationW)
	}
}
