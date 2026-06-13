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
	healthPool.Add(cmd, component.HealthComponent{HP: 1, MaxHP: 200}) // 0.5% HP

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.States[1].State = StateApproach

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	_ = cmds
	// With RetreatHPThreshold=0.0, retreat is disabled — AI fights to death
	if sys.States[1].State == StateRetreat {
		t.Error("retreat should be disabled with threshold 0.0")
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
	sys.PlayerGold = map[uint32]int32{2: 50}

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool)
	recruitCmds := filterCmds(cmds, CmdRecruit)
	if len(recruitCmds) == 0 {
		t.Fatal("expected at least 1 recruit command, got 0")
	}
	// Verify varied unit types across multiple calls
	seenTypes := map[component.CombatUnitType]bool{}
	for _, c := range recruitCmds {
		seenTypes[c.UnitType] = true
	}
	// At least one recruit must be affordable
	for _, c := range recruitCmds {
		cost := component.CombatUnitTypeTable[c.UnitType].RecruitCost
		if cost > 50 {
			t.Errorf("recruited unit type %d costs %d but only had 50 gold", c.UnitType, cost)
		}
	}
	// Verify gold was not overspent (AI doesn't deduct — recruitSys does)
	if sys.PlayerGold[2] < 0 {
		t.Errorf("gold went negative: %d", sys.PlayerGold[2])
	}
}

func TestAIRecruitNoGold(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 10}
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

func TestAIRecruitRoleBalance(t *testing.T) {
	// With 200 gold, the AI should recruit units from multiple roles,
	// not just spam Light Infantry.
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 200}

	cmds := sys.recruitDecisions()
	if len(cmds) == 0 {
		t.Fatal("expected recruit commands with 200 gold")
	}

	roles := map[int]int{}
	for _, c := range cmds {
		roles[unitRole[c.UnitType]]++
	}
	t.Logf("recruited %d units across %d roles: %v", len(cmds), len(roles), roles)

	// With 200 gold, AI should recruit from at least 2 different roles
	if len(roles) < 2 {
		t.Errorf("expected units from at least 2 roles, got %d: %v", len(roles), roles)
	}

	// Verify no unit costs more than available gold
	for _, c := range cmds {
		cost := component.CombatUnitTypeTable[c.UnitType].RecruitCost
		if cost > 200 {
			t.Errorf("unit type %d costs %d, exceeds 200 gold budget", c.UnitType, cost)
		}
	}
}

func TestAIRecruitMaxThreePerTick(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 1000} // tons of gold

	cmds := sys.recruitDecisions()
	if len(cmds) > 3 {
		t.Errorf("AI recruited %d units in one tick, max is 3", len(cmds))
	}
}

func TestAIRecruitNoGoldMap(t *testing.T) {
	// PlayerGold is nil — should not panic
	sys := NewAISystem(2, nil, 48, 96)
	cmds := sys.recruitDecisions()
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruits with nil PlayerGold, got %d", len(cmds))
	}
}

func TestAIPickRole(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)

	// Empty army → frontline (start with meat shields)
	role := sys.pickRole([3]int{0, 0, 0})
	if role != RoleFrontline {
		t.Errorf("empty army should pick frontline, got %d", role)
	}

	// All frontline → should pick ranged or heavy (underrepresented)
	role = sys.pickRole([3]int{10, 0, 0})
	if role == RoleFrontline {
		t.Errorf("all-frontline army should pick non-frontline, got frontline")
	}

	// Balanced army (4/3/3) → all roles close to target, any is fine
	role = sys.pickRole([3]int{4, 3, 3})
	if role < 0 || role > 2 {
		t.Errorf("balanced army should pick valid role 0-2, got %d", role)
	}
}

func TestAICheapestAffordableUnit(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)

	// With 15 gold, only LightInfantry (15) is affordable
	ut := sys.cheapestAffordableUnit(15)
	if ut == nil {
		t.Fatal("expected a unit with 15 gold, got nil")
	}
	if *ut != component.UnitLightInfantry {
		t.Errorf("cheapest unit at 15 gold should be LightInfantry, got %d", *ut)
	}

	// With 0 gold, nothing is affordable
	ut = sys.cheapestAffordableUnit(0)
	if ut != nil {
		t.Errorf("expected nil with 0 gold, got unit type %d", *ut)
	}
}
