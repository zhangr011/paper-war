package ai

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
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

// setupTestWorld creates a minimal ECS world with all pools needed for Update().
func setupTestWorld() (*ecs.EntityManager, *ecs.World,
	*ecs.ComponentPool[component.CommanderComponent],
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)

	return em, w, cmdPool, posPool, ownerPool, healthPool, unitTypePool
}

func TestAIPatrolNoEnemy(t *testing.T) {
	_, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()
	cmd := ecs.NewEntityManager().Create()
	// Use the em from setup
	em := ecs.NewEntityManager()
	cmd = em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)
	// Should get patrol or stronghold move command
	if len(cmds) == 0 {
		t.Fatal("expected at least 1 command, got 0")
	}
	if cmds[len(cmds)-1].Type != CmdMove {
		t.Error("expected move command for patrol/strategic behavior")
	}
}

func TestAIRetreat(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	// AI commander with low HP
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(50), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 1, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.States[1].State = StateApproach

	sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)
	// With RetreatHPThreshold=0.0, retreat is disabled — AI fights to death
	if sys.States[1].State == StateRetreat {
		t.Error("retreat should be disabled with threshold 0.0")
	}
}

func TestAICaptureDefense(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(10), Y: fixed.FromFloat(10)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.SetObjective(&tilemap.Objective{Type: tilemap.ObjectiveCapture, TargetX: 40, TargetY: 40})

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command for capture defense")
	}
}

// === RECRUITMENT TESTS ===

func TestAIRecruit(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 48, 96)
	sys.RegisterSquad(1, uint32(cmd))
	sys.PlayerGold = map[uint32]int32{2: 50}

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)
	recruitCmds := filterCmds(cmds, CmdRecruit)
	if len(recruitCmds) == 0 {
		t.Fatal("expected at least 1 recruit command")
	}
	for _, c := range recruitCmds {
		cost := component.CombatUnitTypeTable[c.UnitType].RecruitCost
		if cost > 50 {
			t.Errorf("recruited unit type %d costs %d but only had 50 gold", c.UnitType, cost)
		}
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

func TestAIRecruitRoleBalance(t *testing.T) {
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

	if len(roles) < 2 {
		t.Errorf("expected units from at least 2 roles, got %d: %v", len(roles), roles)
	}
}

func TestAIRecruitMaxThreePerTick(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 1000}
	cmds := sys.recruitDecisions()
	if len(cmds) > 3 {
		t.Errorf("AI recruited %d units in one tick, max is 3", len(cmds))
	}
}

func TestAIRecruitNoGoldMap(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	cmds := sys.recruitDecisions()
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruits with nil PlayerGold, got %d", len(cmds))
	}
}

func TestAICheapestAffordableUnit(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)

	ut := sys.cheapestAffordableUnit(15)
	if ut == nil {
		t.Fatal("expected a unit with 15 gold, got nil")
	}
	if *ut != component.UnitLightInfantry {
		t.Errorf("cheapest unit at 15 gold should be LightInfantry, got %d", *ut)
	}

	ut = sys.cheapestAffordableUnit(0)
	if ut != nil {
		t.Errorf("expected nil with 0 gold, got unit type %d", *ut)
	}
}

// === STRATEGIC BEHAVIOR TESTS ===

func TestAIBaseDefense(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	// AI commander near base
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(40), Y: fixed.FromFloat(40)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	// Enemy near AI base
	enemyCmd := em.Create()
	posPool.Add(enemyCmd, component.PositionComponent{X: fixed.FromFloat(42), Y: fixed.FromFloat(42)})
	cmdPool.Add(enemyCmd, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemyCmd, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemyCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetBasePosition(fixed.FromFloat(40), fixed.FromFloat(40))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)

	// AI should respond — either attack the nearby enemy or defend base
	if len(cmds) == 0 {
		t.Fatal("expected command when enemy near base, got 0")
	}

	// Should have an attack or move-to-base command
	found := false
	for _, c := range cmds {
		if c.Type == CmdAttack || c.Type == CmdMove {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected attack or move command when enemy near base")
	}
}

func TestAIStrongholdCapture(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	// AI commander with no enemies visible
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetStrongholds([][2]int32{{50, 30}, {10, 50}})

	cmds := sys.Update(100, cmdPool, posPool, ownerPool, healthPool, unitTypePool)

	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command toward stronghold")
	}

	// The move target should be toward a stronghold
	target := moveCmds[len(moveCmds)-1]
	targetX := fixed.ToFloat(target.TargetX)
	targetY := fixed.ToFloat(target.TargetY)

	// Should be heading to one of the strongholds (50,30) or (10,50)
	d1 := (targetX-50)*(targetX-50) + (targetY-30)*(targetY-30)
	d2 := (targetX-10)*(targetX-10) + (targetY-50)*(targetY-50)
	if d1 > 25 && d2 > 25 {
		t.Errorf("AI should move toward a stronghold, got (%.1f,%.1f)", targetX, targetY)
	}
}

func TestAIExploration(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	// AI commander — early game, no enemies
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	// Create fog system — all tiles unexplored for AI player
	fogSys := fog.NewFogSystem(64, 64)

	sys := NewAISystem(2, fogSys, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)

	// Early game with fog — should move (exploring or patrolling)
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command during exploration phase")
	}
}

func TestAIAdaptiveRecruitment(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 200}

	// Simulate seeing lots of enemy Snipers (ranged)
	sys.EnemyUnits = map[component.CombatUnitType]int{
		component.UnitSniper: 5,
	}

	ratio := sys.adaptiveRoleRatio()
	t.Logf("adaptive ratio vs Snipers: frontline=%.2f ranged=%.2f heavy=%.2f",
		ratio[RoleFrontline], ratio[RoleRanged], ratio[RoleHeavy])

	// When enemy has many ranged, boost frontline (close distance)
	if ratio[RoleFrontline] <= roleTargetRatio[RoleFrontline] {
		t.Errorf("expected boosted frontline ratio vs Snipers, got %.2f (base %.2f)",
			ratio[RoleFrontline], roleTargetRatio[RoleFrontline])
	}

	// Test vs heavy enemy
	sys.EnemyUnits = map[component.CombatUnitType]int{
		component.UnitMotorGun: 5,
	}
	ratio2 := sys.adaptiveRoleRatio()
	t.Logf("adaptive ratio vs Heavy: frontline=%.2f ranged=%.2f heavy=%.2f",
		ratio2[RoleFrontline], ratio2[RoleRanged], ratio2[RoleHeavy])

	// When enemy has many heavy, boost ranged (Anti-Armor)
	if ratio2[RoleRanged] <= roleTargetRatio[RoleRanged] {
		t.Errorf("expected boosted ranged ratio vs Heavy, got %.2f (base %.2f)",
			ratio2[RoleRanged], roleTargetRatio[RoleRanged])
	}

	// Test with no intel — should return default ratio
	sys.EnemyUnits = map[component.CombatUnitType]int{}
	ratio3 := sys.adaptiveRoleRatio()
	if ratio3 != roleTargetRatio {
		t.Errorf("expected default ratio with no intel, got %v", ratio3)
	}
}

func TestAIAdaptiveRatioClamps(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 200}

	// Extreme enemy composition — all ranged
	sys.EnemyUnits = map[component.CombatUnitType]int{
		component.UnitSniper: 100,
	}
	ratio := sys.adaptiveRoleRatio()

	// No role should be below 10% or above 60%
	for i, r := range ratio {
		if r < 0.10 {
			t.Errorf("role %d ratio %.2f below 10%% floor", i, r)
		}
		if r > 0.60 {
			t.Errorf("role %d ratio %.2f above 60%% ceiling", i, r)
		}
	}

	// Ratios should sum to ~1.0
	sum := ratio[0] + ratio[1] + ratio[2]
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("ratio sum = %.3f, expected ~1.0", sum)
	}
}

func TestAISetBasePosition(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.SetBasePosition(fixed.FromFloat(30), fixed.FromFloat(40))
	if fixed.ToFloat(sys.BaseX) != 30 {
		t.Errorf("BaseX = %.1f, want 30", fixed.ToFloat(sys.BaseX))
	}
	if fixed.ToFloat(sys.BaseY) != 40 {
		t.Errorf("BaseY = %.1f, want 40", fixed.ToFloat(sys.BaseY))
	}
}

func TestAISetStrongholds(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.SetStrongholds([][2]int32{{10, 20}, {30, 40}})
	if len(sys.Strongholds) != 2 {
		t.Fatalf("expected 2 strongholds, got %d", len(sys.Strongholds))
	}
	if sys.Strongholds[0] != [2]int32{10, 20} {
		t.Errorf("stronghold[0] = %v, want [10,20]", sys.Strongholds[0])
	}
}

func TestAIEnemyCompositionTracking(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool := setupTestWorld()

	// AI commander
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	// Enemy commander (player)
	enemyCmd := em.Create()
	posPool.Add(enemyCmd, component.PositionComponent{X: fixed.FromFloat(31), Y: fixed.FromFloat(31)})
	cmdPool.Add(enemyCmd, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemyCmd, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemyCmd, component.HealthComponent{HP: 200, MaxHP: 200})
	unitTypePool.Add(enemyCmd, component.UnitTypeComponent{Type: component.UnitSniper})

	sys := NewAISystem(2, nil, 64, 64) // no fog — can see everything
	sys.RegisterSquad(1, uint32(aiCmd))

	sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool)

	// AI should have recorded the enemy unit type
	if sys.EnemyUnits[component.UnitSniper] == 0 {
		t.Error("expected AI to track enemy Sniper unit, got 0")
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
