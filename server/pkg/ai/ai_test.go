package ai

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestNewAISystem(t *testing.T) {
	sys := NewAISystem(2, nil, 64, 64)
	if sys.AIPlayerID != 2 {
		t.Error("expected AI player ID 2")
	}
}

func TestRegisterSquad(t *testing.T) {
	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, 100)
	state, ok := sys.States[1]
	if !ok {
		t.Fatal("squad 1 should be registered")
	}
	if state.CommanderID != 100 {
		t.Error("commander ID mismatch")
	}
	if state.State != StateIdle {
		t.Error("initial state should be Idle")
	}
	if state.PatrolX == 0 && state.PatrolY == 0 {
		t.Error("patrol target should be set")
	}
}

func TestPickPatrolTarget(t *testing.T) {
	sys := NewAISystem(2, nil, 64, 64)
	state := &AIState{}
	sys.pickPatrolTarget(state)
	px := fixed.ToFloat(state.PatrolX)
	py := fixed.ToFloat(state.PatrolY)
	if px < 5 || px > 59 {
		t.Errorf("patrol X out of bounds: %.1f", px)
	}
	if py < 5 || py > 59 {
		t.Errorf("patrol Y out of bounds: %.1f", py)
	}
}

func TestAIPatrolNoEnemy(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	_ = em

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	// Create AI commander
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Type != CmdMove {
		t.Error("expected move command for patrol")
	}
}

func TestAIRetreat(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	// AI commander with low HP
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(50), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 30, MaxHP: 200}) // 15% HP

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.States[1].State = StateApproach

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 retreat command, got %d", len(cmds))
	}
	if cmds[0].Type != CmdMove {
		t.Error("expected move command for retreat")
	}
	if sys.States[1].State != StateRetreat {
		t.Error("state should be retreat")
	}
}

func TestAICaptureDefense(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(10), Y: fixed.FromFloat(10)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 48, 96)
	sys.RegisterSquad(1, uint32(cmd))
	sys.SetObjective(&tilemap.Objective{
		Type:    tilemap.ObjectiveCapture,
		TargetX: 30,
		TargetY: 48,
	})

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	// Should get a move command toward the capture target
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) != 1 {
		t.Fatalf("expected 1 move command for capture defense, got %d", len(moveCmds))
	}
	if sys.States[1].State != StateDefend {
		t.Error("state should be Defend for capture objective")
	}
}

func TestAIRecruit(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 48, 96)
	sys.RegisterSquad(1, uint32(cmd))
	sys.AIRecruitGold = 50

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	recruitCmds := filterCmds(cmds, CmdRecruit)
	if len(recruitCmds) != 3 {
		t.Fatalf("expected 3 recruit commands, got %d", len(recruitCmds))
	}
	for i, c := range recruitCmds {
		if c.UnitType != component.UnitLightInfantry {
			t.Errorf("recruit cmd %d: unit type = %d, want LightInfantry", i, c.UnitType)
		}
	}
	// Gold should be spent (50 - 3*15 = 5)
	if sys.AIRecruitGold != 5 {
		t.Errorf("remaining gold = %d, want 5", sys.AIRecruitGold)
	}
}

func TestAIRecruitNoGold(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.AIRecruitGold = 10
	cmds := sys.recruitDecisions()
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruit commands with 10 gold, got %d", len(cmds))
	}
}

func filterCmds(cmds []AICommand, cmdType uint8) []AICommand {
	var result []AICommand
	for _, c := range cmds {
		if c.Type == cmdType {
			result = append(result, c)
		}
	}
	return result
}
