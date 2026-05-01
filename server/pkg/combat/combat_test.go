package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

func TestCombatAutoAttack(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(10.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)

	cs := &CombatSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	// Attacker: squad 1, position (0,0)
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(5.0), Damage: 10, Cooldown: 1, AttackType: component.AttackMelee,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})

	// Target: squad 2, position (3,0), HP=100
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100, Armor: 2})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})

	// Rebuild spatial hash
	sh.Clear()
	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		sh.Insert(uint64(e), pos.X, pos.Y)
	})

	w.Tick(1)

	// Target should have taken 8 damage (10 - 2 armor)
	hp, _ := healthPool.Get(target)
	if hp.HP != 92 {
		t.Errorf("target HP = %d, want 92 (100 - 10 + 2 armor = 92)", hp.HP)
	}
}

func TestCombatOutOfRange(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(10.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)

	cs := &CombatSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(2.0), Damage: 10, AttackType: component.AttackRanged,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})

	// Target far away (10,0) — out of range
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(10.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2})

	sh.Clear()
	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		sh.Insert(uint64(e), pos.X, pos.Y)
	})

	w.Tick(1)

	hp, _ := healthPool.Get(target)
	if hp.HP != 100 {
		t.Errorf("out of range target HP = %d, want 100 (undamaged)", hp.HP)
	}
}
