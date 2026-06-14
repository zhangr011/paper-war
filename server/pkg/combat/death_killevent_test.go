package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// TestDeathSystemEmitsKillEvent verifies that when a unit is killed, the
// DeathSystem populates KillEvents with the correct faction attribution,
// commander flag, and bounty.
func TestDeathSystemEmitsKillEvent(t *testing.T) {
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
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.KillPointsComponent{}, kpPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)

	ds := &DeathSystem{}
	w.AddSystem(ds)
	w.Init()

	// Killer: faction 0 (player), playerID 1
	killer := em.Create()
	posPool.Add(killer, component.PositionComponent{})
	velPool.Add(killer, component.VelocityComponent{})
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(killer, component.AttackComponent{})
	boidPool.Add(killer, component.BoidComponent{})
	movePool.Add(killer, component.MovementComponent{})
	pathPool.Add(killer, component.PathfindingComponent{})
	kpPool.Add(killer, component.KillPointsComponent{})
	ownerPool.Add(killer, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(killer, component.UnitTypeComponent{Type: component.UnitLightInfantry})

	// Victim: faction 1 (enemy), killed by killer
	victim := em.Create()
	posPool.Add(victim, component.PositionComponent{})
	velPool.Add(victim, component.VelocityComponent{})
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: uint32(killer)})
	attackPool.Add(victim, component.AttackComponent{})
	boidPool.Add(victim, component.BoidComponent{})
	movePool.Add(victim, component.MovementComponent{})
	pathPool.Add(victim, component.PathfindingComponent{})
	ownerPool.Add(victim, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	unitTypePool.Add(victim, component.UnitTypeComponent{Type: component.UnitLightInfantry})

	w.Tick(1)

	if len(ds.KillEvents) != 1 {
		t.Fatalf("KillEvents length = %d, want 1", len(ds.KillEvents))
	}
	ke := ds.KillEvents[0]
	if ke.KillerFaction != component.FactionPlayer {
		t.Errorf("KillerFaction = %d, want %d", ke.KillerFaction, component.FactionPlayer)
	}
	if ke.DeadFaction != component.FactionEnemy {
		t.Errorf("DeadFaction = %d, want %d", ke.DeadFaction, component.FactionEnemy)
	}
	if ke.IsCommander != false {
		t.Errorf("IsCommander = true, want false")
	}
	// Bounty for LightInfantry from CombatUnitTypeTable
	expectedBounty := component.CombatUnitTypeTable[component.UnitLightInfantry].KillBounty
	if ke.Bounty != expectedBounty {
		t.Errorf("Bounty = %d, want %d", ke.Bounty, expectedBounty)
	}
}

// TestDeathSystemKillEventCommander verifies that killing a commander sets
// IsCommander = true in the KillEvent.
func TestDeathSystemKillEventCommander(t *testing.T) {
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
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.KillPointsComponent{}, kpPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)

	ds := &DeathSystem{}
	w.AddSystem(ds)
	w.Init()

	// Killer: faction 1 (enemy)
	killer := em.Create()
	posPool.Add(killer, component.PositionComponent{})
	velPool.Add(killer, component.VelocityComponent{})
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(killer, component.AttackComponent{})
	boidPool.Add(killer, component.BoidComponent{})
	movePool.Add(killer, component.MovementComponent{})
	pathPool.Add(killer, component.PathfindingComponent{})
	kpPool.Add(killer, component.KillPointsComponent{})
	ownerPool.Add(killer, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	unitTypePool.Add(killer, component.UnitTypeComponent{Type: component.UnitLightInfantry})

	// Victim: faction 0 (player), is commander
	victim := em.Create()
	posPool.Add(victim, component.PositionComponent{})
	velPool.Add(victim, component.VelocityComponent{})
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: uint32(killer)})
	attackPool.Add(victim, component.AttackComponent{})
	boidPool.Add(victim, component.BoidComponent{})
	movePool.Add(victim, component.MovementComponent{})
	pathPool.Add(victim, component.PathfindingComponent{})
	ownerPool.Add(victim, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(victim, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	cmdPool.Add(victim, component.CommanderComponent{SquadID: 1, IsAlive: true})

	w.Tick(1)

	if len(ds.KillEvents) != 1 {
		t.Fatalf("KillEvents length = %d, want 1", len(ds.KillEvents))
	}
	ke := ds.KillEvents[0]
	if !ke.IsCommander {
		t.Errorf("IsCommander = false, want true (victim is a commander)")
	}
	if ke.KillerFaction != component.FactionEnemy {
		t.Errorf("KillerFaction = %d, want %d", ke.KillerFaction, component.FactionEnemy)
	}
	if ke.DeadFaction != component.FactionPlayer {
		t.Errorf("DeadFaction = %d, want %d", ke.DeadFaction, component.FactionPlayer)
	}
}

// TestDeathSystemKillEventNoKiller verifies that a death without a killer
// (LastAttacker = 0) still emits a KillEvent with KillerFaction = 0xFF.
func TestDeathSystemKillEventNoKiller(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)

	ds := &DeathSystem{}
	w.AddSystem(ds)
	w.Init()

	// Victim with no LastAttacker
	victim := em.Create()
	posPool.Add(victim, component.PositionComponent{})
	velPool.Add(victim, component.VelocityComponent{})
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: 0})
	attackPool.Add(victim, component.AttackComponent{})
	boidPool.Add(victim, component.BoidComponent{})
	movePool.Add(victim, component.MovementComponent{})
	pathPool.Add(victim, component.PathfindingComponent{})
	ownerPool.Add(victim, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(victim, component.UnitTypeComponent{Type: component.UnitLightInfantry})

	w.Tick(1)

	if len(ds.KillEvents) != 1 {
		t.Fatalf("KillEvents length = %d, want 1", len(ds.KillEvents))
	}
	ke := ds.KillEvents[0]
	if ke.KillerFaction != 0xFF {
		t.Errorf("KillerFaction = %d, want 0xFF (no killer)", ke.KillerFaction)
	}
	if ke.DeadFaction != component.FactionPlayer {
		t.Errorf("DeadFaction = %d, want %d", ke.DeadFaction, component.FactionPlayer)
	}
	if ke.Bounty != 0 {
		t.Errorf("Bounty = %d, want 0 (no killer)", ke.Bounty)
	}
}
