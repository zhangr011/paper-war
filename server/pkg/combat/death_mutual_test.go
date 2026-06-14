package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// helper to set up a death system world for kill event tests
func setupDeathTestWorld(t *testing.T) (*ecs.EntityManager, *ecs.World, *DeathSystem,
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
	*ecs.ComponentPool[component.KillPointsComponent],
	*ecs.ComponentPool[component.CommanderComponent],
	*ecs.ComponentPool[component.BoidComponent]) {
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

	return em, w, ds, healthPool, ownerPool, unitTypePool, kpPool, cmdPool, boidPool
}

// TestMutualKillAttribution verifies that when two units kill each other in the
// same tick (both have HP <= 0 and each is the other's LastAttacker), both
// kills are correctly attributed to the attacker's faction. This was Bug 1 in
// issue #18: kills were only credited if the attacker was still alive, so
// mutual kills produced phantom unattributed deaths.
func TestMutualKillAttribution(t *testing.T) {
	em, w, ds, healthPool, ownerPool, unitTypePool, kpPool, _, _ := setupDeathTestWorld(t)

	// Unit A: faction 0 (player), killed by B
	unitA := em.Create()
	healthPool.Add(unitA, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: 0}) // set below
	ownerPool.Add(unitA, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(unitA, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	kpPool.Add(unitA, component.KillPointsComponent{})

	// Unit B: faction 1 (enemy), killed by A
	unitB := em.Create()
	healthPool.Add(unitB, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: 0}) // set below
	ownerPool.Add(unitB, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	unitTypePool.Add(unitB, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	kpPool.Add(unitB, component.KillPointsComponent{})

	// Cross-link: each unit's LastAttacker is the other (requires GetPtr
	// since Get returns a value copy)
	hpA, _ := healthPool.GetPtr(unitA)
	hpA.LastAttacker = uint32(unitB)
	hpB, _ := healthPool.GetPtr(unitB)
	hpB.LastAttacker = uint32(unitA)

	w.Tick(1)

	if len(ds.KillEvents) != 2 {
		t.Fatalf("KillEvents length = %d, want 2", len(ds.KillEvents))
	}

	// Both kills should be attributed (neither should be 0xFF)
	for i, ke := range ds.KillEvents {
		if ke.KillerFaction == 0xFF {
			t.Errorf("KillEvent[%d] KillerFaction = 0xFF (unattributed), want a faction — mutual kill not credited", i)
		}
	}

	// Verify one event credits faction 0 for killing faction 1, and the other
	// credits faction 1 for killing faction 0
	var f0Kills, f1Kills int
	for _, ke := range ds.KillEvents {
		if ke.KillerFaction == component.FactionPlayer && ke.DeadFaction == component.FactionEnemy {
			f0Kills++
		}
		if ke.KillerFaction == component.FactionEnemy && ke.DeadFaction == component.FactionPlayer {
			f1Kills++
		}
	}
	if f0Kills != 1 {
		t.Errorf("Faction 0 kills = %d, want 1", f0Kills)
	}
	if f1Kills != 1 {
		t.Errorf("Faction 1 kills = %d, want 1", f1Kills)
	}
}

// TestPromotedCommanderKillNotCounted verifies that when a promoted commander
// (a combat unit that was promoted after the original commander died) is killed,
// it does NOT count as a commander kill in the KillEvent. This was Bug 2 in
// issue #18: every promoted commander death inflated the Commander Kills stat.
func TestPromotedCommanderKillNotCounted(t *testing.T) {
	em, w, ds, healthPool, ownerPool, unitTypePool, kpPool, cmdPool, _ := setupDeathTestWorld(t)

	// Killer: faction 1 (enemy), alive
	killer := em.Create()
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(killer, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	unitTypePool.Add(killer, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	kpPool.Add(killer, component.KillPointsComponent{})

	// Victim: faction 0, promoted commander (Promoted=true), dead
	victim := em.Create()
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: uint32(killer)})
	ownerPool.Add(victim, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(victim, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	cmdPool.Add(victim, component.CommanderComponent{SquadID: 1, IsAlive: true, Promoted: true})

	w.Tick(1)

	if len(ds.KillEvents) != 1 {
		t.Fatalf("KillEvents length = %d, want 1", len(ds.KillEvents))
	}
	ke := ds.KillEvents[0]
	if ke.IsCommander {
		t.Errorf("IsCommander = true, want false (promoted commanders should not count as commander kills)")
	}
	// Kill should still be attributed
	if ke.KillerFaction != component.FactionEnemy {
		t.Errorf("KillerFaction = %d, want %d", ke.KillerFaction, component.FactionEnemy)
	}
}

// TestOriginalCommanderKillStillCounted verifies that an original (non-promoted)
// commander death still counts as a commander kill — a regression guard for the
// Promoted flag fix.
func TestOriginalCommanderKillStillCounted(t *testing.T) {
	em, w, ds, healthPool, ownerPool, unitTypePool, kpPool, cmdPool, _ := setupDeathTestWorld(t)

	// Killer: faction 1 (enemy), alive
	killer := em.Create()
	healthPool.Add(killer, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(killer, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	unitTypePool.Add(killer, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	kpPool.Add(killer, component.KillPointsComponent{})

	// Victim: faction 0, original commander (Promoted=false), dead
	victim := em.Create()
	healthPool.Add(victim, component.HealthComponent{HP: 0, MaxHP: 50, LastAttacker: uint32(killer)})
	ownerPool.Add(victim, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	unitTypePool.Add(victim, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	cmdPool.Add(victim, component.CommanderComponent{SquadID: 1, IsAlive: true, Promoted: false})

	w.Tick(1)

	if len(ds.KillEvents) != 1 {
		t.Fatalf("KillEvents length = %d, want 1", len(ds.KillEvents))
	}
	ke := ds.KillEvents[0]
	if !ke.IsCommander {
		t.Errorf("IsCommander = false, want true (original commander kills should still count)")
	}
}
