package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// fixedFromInt converts an integer to fixed-point. One unit = 1 << 12.
func fixedFromInt(v int64) int64 { return v << 12 }

// TestRecruitmentSystemCountsSuccessfulRecruits verifies that
// SuccessfulRecruits tracks the count per player after a tick.
func TestRecruitmentSystemCountsSuccessfulRecruits(t *testing.T) {
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
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)

	rs := &RecruitmentSystem{
		PlayerGold: map[uint32]int32{1: 1000},
	}
	w.AddSystem(rs)
	w.Init()

	// Commander for player 1
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixedFromInt(10), Y: fixedFromInt(10)})
	velPool.Add(cmd, component.VelocityComponent{})
	healthPool.Add(cmd, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1})
	movePool.Add(cmd, component.MovementComponent{})
	pathPool.Add(cmd, component.PathfindingComponent{})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})

	// Enqueue 2 recruits
	rs.Recruit(RecruitRequest{CommanderEntity: cmd, UnitType: component.UnitLightInfantry})
	rs.Recruit(RecruitRequest{CommanderEntity: cmd, UnitType: component.UnitLightInfantry})

	w.Tick(1)

	if rs.SuccessfulRecruits[1] != 2 {
		t.Errorf("SuccessfulRecruits[1] = %d, want 2", rs.SuccessfulRecruits[1])
	}
}
