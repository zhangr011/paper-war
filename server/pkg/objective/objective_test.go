package objective

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func setupObjectiveWorld(objType tilemap.ObjectiveType) (*ecs.World, *ecs.EntityManager, *ObjectiveSystem,
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	*ecs.ComponentPool[component.PositionComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := &tilemap.GameMap{Width: 48, Height: 96}
	gm.Objective = tilemap.Objective{Type: objType, HoldTarget: 300}

	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	sys := NewObjectiveSystem(gm)
	w.AddSystem(sys)
	w.Init()

	return w, em, sys, healthPool, ownerPool, posPool
}

func TestEliminationPlayerWins(t *testing.T) {
	w, em, sys, healthPool, ownerPool, _ := setupObjectiveWorld(tilemap.ObjectiveElimination)

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})

	w.Tick(1)

	r := sys.Result()
	if r == nil {
		t.Fatal("expected match result")
	}
	if r.Winner != component.FactionPlayer {
		t.Errorf("winner = %d, want player (0)", r.Winner)
	}
	if r.Reason != "elimination" {
		t.Errorf("reason = %q, want elimination", r.Reason)
	}
}

func TestEliminationEnemyWins(t *testing.T) {
	w, em, sys, healthPool, ownerPool, _ := setupObjectiveWorld(tilemap.ObjectiveElimination)

	enemy := em.Create()
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(enemy, component.OwnerComponent{Faction: component.FactionEnemy})

	w.Tick(1)

	r := sys.Result()
	if r == nil {
		t.Fatal("expected match result")
	}
	if r.Winner != component.FactionEnemy {
		t.Errorf("winner = %d, want enemy (1)", r.Winner)
	}
}

func TestEliminationNoResultYet(t *testing.T) {
	w, em, sys, healthPool, ownerPool, _ := setupObjectiveWorld(tilemap.ObjectiveElimination)

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})

	enemy := em.Create()
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(enemy, component.OwnerComponent{Faction: component.FactionEnemy})

	w.Tick(1)

	if sys.Result() != nil {
		t.Error("no result expected while both factions alive")
	}
}

func TestCapturePlayerWins(t *testing.T) {
	w, em, sys, healthPool, ownerPool, posPool := setupObjectiveWorld(tilemap.ObjectiveCapture)
	sys.gm.Objective.TargetX = 10
	sys.gm.Objective.TargetY = 10

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})
	posPool.Add(player, component.PositionComponent{X: fixed.FromFloat(10.5), Y: fixed.FromFloat(10.5)})

	for i := uint32(1); i <= 300; i++ {
		w.Tick(i)
		if sys.Result() != nil {
			break
		}
	}

	r := sys.Result()
	if r == nil {
		t.Fatal("expected capture result after 300 ticks")
	}
	if r.Winner != component.FactionPlayer {
		t.Errorf("winner = %d, want player", r.Winner)
	}
	if r.Reason != "capture" {
		t.Errorf("reason = %q, want capture", r.Reason)
	}
}

func TestCaptureResetsOnContest(t *testing.T) {
	w, em, sys, healthPool, ownerPool, posPool := setupObjectiveWorld(tilemap.ObjectiveCapture)
	sys.gm.Objective.TargetX = 10
	sys.gm.Objective.TargetY = 10

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})
	posPool.Add(player, component.PositionComponent{X: fixed.FromFloat(10.1), Y: fixed.FromFloat(10.1)})

	for i := uint32(1); i <= 100; i++ {
		w.Tick(i)
	}

	holder, counter := sys.CaptureState()
	if holder != 1 {
		t.Errorf("holder = %d, want 1 (player)", holder)
	}
	if counter != 100 {
		t.Errorf("counter = %d, want 100", counter)
	}

	// Enemy moves closer to capture point
	enemy := em.Create()
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(enemy, component.OwnerComponent{Faction: component.FactionEnemy})
	posPool.Add(enemy, component.PositionComponent{X: fixed.FromFloat(10.0), Y: fixed.FromFloat(10.0)})

	w.Tick(101)

	holder, counter = sys.CaptureState()
	if holder != 2 {
		t.Errorf("after contest, holder = %d, want 2 (enemy)", holder)
	}
	if counter != 1 {
		t.Errorf("after contest, counter should reset to 1, got %d", counter)
	}
}

func TestSurvivalTimerExpires(t *testing.T) {
	w, em, sys, healthPool, ownerPool, _ := setupObjectiveWorld(tilemap.ObjectiveSurvival)
	sys.gm.Objective.Duration = 500

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})

	enemy := em.Create()
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(enemy, component.OwnerComponent{Faction: component.FactionEnemy})

	for i := uint32(1); i <= 500; i++ {
		w.Tick(i)
		if sys.Result() != nil {
			break
		}
	}

	r := sys.Result()
	if r == nil {
		t.Fatal("expected survival result at tick 500")
	}
	if r.Winner != component.FactionPlayer {
		t.Errorf("survival winner = %d, want player", r.Winner)
	}
	if r.Reason != "survival" {
		t.Errorf("reason = %q, want survival", r.Reason)
	}
}

func TestSurvivalEnemyEliminatedBeforeTimer(t *testing.T) {
	w, em, sys, healthPool, ownerPool, _ := setupObjectiveWorld(tilemap.ObjectiveSurvival)
	sys.gm.Objective.Duration = 1000

	player := em.Create()
	healthPool.Add(player, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(player, component.OwnerComponent{Faction: component.FactionPlayer})

	w.Tick(1)

	r := sys.Result()
	if r == nil {
		t.Fatal("expected elimination result before timer")
	}
	if r.Reason != "elimination" {
		t.Errorf("reason = %q, want elimination (enemy dead before timer)", r.Reason)
	}
}
