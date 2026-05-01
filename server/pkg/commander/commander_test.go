package commander

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

func TestCommanderAura(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(10.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)

	cs := &CommanderSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	// Commander at (0,0)
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: 0, Y: 0})
	healthPool.Add(cmd, component.HealthComponent{HP: 100, MaxHP: 100})
	cmdPool.Add(cmd, component.CommanderComponent{
		SquadID: 1, AuraRadius: fixed.FromFloat(5.0),
		AuraMoraleBonus: 20, IsAlive: true,
	})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// Squad member nearby
	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{X: fixed.FromFloat(2.0), Y: 0})
	healthPool.Add(unit, component.HealthComponent{HP: 100, MaxHP: 100, Morale: 50})
	boidPool.Add(unit, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})

	sh.Clear()
	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		sh.Insert(uint64(e), pos.X, pos.Y)
	})

	w.Tick(1)

	hp, _ := healthPool.Get(unit)
	if hp.Morale != 120 {
		t.Errorf("squad morale = %d, want 120 (100 base + 20 aura bonus)", hp.Morale)
	}
}

func TestCommanderDeath(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(10.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)

	cs := &CommanderSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: 0, Y: 0})
	healthPool.Add(cmd, component.HealthComponent{HP: 0, MaxHP: 100}) // dead
	cmdPool.Add(cmd, component.CommanderComponent{
		SquadID: 1, AuraRadius: fixed.FromFloat(5.0), IsAlive: true,
	})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{X: fixed.FromFloat(2.0), Y: 0})
	boidPool.Add(unit, component.BoidComponent{
		SquadID: 1, Role: component.RoleMelee,
		SeparationW: 100, CohesionW: 100, FormationW: 200,
	})

	sh.Clear()
	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		sh.Insert(uint64(e), pos.X, pos.Y)
	})

	w.Tick(1)

	c, _ := cmdPool.Get(cmd)
	if c.IsAlive {
		t.Error("commander should be marked dead")
	}

	b, _ := boidPool.Get(unit)
	if b.FormationW >= 200 {
		t.Errorf("formation weight should decrease after commander death, got %d", b.FormationW)
	}
}
