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
	*ecs.ComponentPool[component.BoidComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)
	w.RegisterPool(component.BoidComponent{}, boidPool)

	return em, w, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool
}

func TestAIPatrolNoEnemy(t *testing.T) {
	_, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()
	em := ecs.NewEntityManager()
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
	// Should get patrol or stronghold move command
	if len(cmds) == 0 {
		t.Fatal("expected at least 1 command, got 0")
	}
	if cmds[len(cmds)-1].Type != CmdMove {
		t.Error("expected move command for patrol/strategic behavior")
	}
}

func TestAIRetreat(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander with critically low HP (1/200 = 0.005 < 0.10 threshold)
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(50), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 1, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.States[1].State = StateApproach

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// v2: Critically low HP should trigger retreat
	if sys.States[1].State != StateRetreat {
		t.Errorf("expected StateRetreat with critically low HP, got %d", sys.States[1].State)
	}
	// Should have issued a retreat move command
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Error("expected retreat move command")
	}
}

func TestAICaptureDefense(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(10), Y: fixed.FromFloat(10)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(cmd))
	sys.SetObjective(&tilemap.Objective{Type: tilemap.ObjectiveCapture, TargetX: 40, TargetY: 40})

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command for capture defense")
	}
}

// === RECRUITMENT TESTS ===

func TestAIRecruit(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(cmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(cmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 48, 96)
	sys.RegisterSquad(1, uint32(cmd))
	sys.PlayerGold = map[uint32]int32{2: 50}

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
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
	cmds := sys.recruitDecisions(1)
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruit commands with 10 gold, got %d", len(cmds))
	}
}

func TestAIRecruitRoleBalance(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 200}

	cmds := sys.recruitDecisions(1)
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
	cmds := sys.recruitDecisions(1)
	if len(cmds) > 3 {
		t.Errorf("AI recruited %d units in one tick, max is 3", len(cmds))
	}
}

func TestAIRecruitNoGoldMap(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	cmds := sys.recruitDecisions(1)
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
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander near base
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(40), Y: fixed.FromFloat(40)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	// Two enemies near AI base (threshold is 2 for scaled response)
	enemy1 := em.Create()
	posPool.Add(enemy1, component.PositionComponent{X: fixed.FromFloat(42), Y: fixed.FromFloat(42)})
	cmdPool.Add(enemy1, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemy1, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy1, component.HealthComponent{HP: 200, MaxHP: 200})

	enemy2 := em.Create()
	posPool.Add(enemy2, component.PositionComponent{X: fixed.FromFloat(41), Y: fixed.FromFloat(41)})
	ownerPool.Add(enemy2, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy2, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetBasePosition(fixed.FromFloat(40), fixed.FromFloat(40))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

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
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander with no enemies visible
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetStrongholds([][2]int32{{50, 30}, {10, 50}})

	cmds := sys.Update(100, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

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
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

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

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

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
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

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

	sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// AI should have recorded the enemy unit type (persistent intel)
	if sys.EnemyUnits[component.UnitSniper] == 0 {
		t.Error("expected AI to track enemy Sniper unit, got 0")
	}
}

// ============================================================================
// v2 TESTS — New AI behaviors
// ============================================================================

// Test that the AI engages at the squad's actual weapon range, not hardcoded 5.0.
// A squad with Snipers (range 8) should hold at range 8 and fire.
func TestAIRangeAwareEngagement(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})
	boidPool.Add(aiCmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// AI sniper unit (range 8) in the squad
	sniper := em.Create()
	posPool.Add(sniper, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	ownerPool.Add(sniper, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(sniper, component.HealthComponent{HP: 30, MaxHP: 30})
	unitTypePool.Add(sniper, component.UnitTypeComponent{Type: component.UnitSniper})
	boidPool.Add(sniper, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	// Enemy at distance 7 tiles — within sniper range (8) but beyond old hardcoded 5
	enemy := em.Create()
	posPool.Add(enemy, component.PositionComponent{X: fixed.FromFloat(27), Y: fixed.FromFloat(30)})
	ownerPool.Add(enemy, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Should have issued an attack command (enemy is within squad max range 8)
	attackCmds := filterCmds(cmds, CmdAttack)
	if len(attackCmds) == 0 {
		t.Fatal("expected attack command when enemy within squad max range (8 tiles for sniper)")
	}
	t.Logf("sniper squad engaged at distance 7 tiles (range 8) — v1 would have approached instead")
}

// Test target prioritization: AI prefers enemy commander over a closer combat unit.
func TestAITargetPrioritization(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})
	boidPool.Add(aiCmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// Close enemy combat unit at distance 4 (cheap bait)
	closeUnit := em.Create()
	posPool.Add(closeUnit, component.PositionComponent{X: fixed.FromFloat(24), Y: fixed.FromFloat(30)})
	ownerPool.Add(closeUnit, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(closeUnit, component.HealthComponent{HP: 100, MaxHP: 100})
	unitTypePool.Add(closeUnit, component.UnitTypeComponent{Type: component.UnitLightInfantry})

	// Enemy commander at distance 6 — farther but higher priority (3x score multiplier)
	enemyCmd := em.Create()
	posPool.Add(enemyCmd, component.PositionComponent{X: fixed.FromFloat(26), Y: fixed.FromFloat(30)})
	cmdPool.Add(enemyCmd, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemyCmd, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemyCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))

	// Test the scoring function directly
	closeDist := int64(fixed.FromFloat(4)) * int64(fixed.FromFloat(4)) // distSq at 4 tiles
	cmdDist := int64(fixed.FromFloat(6)) * int64(fixed.FromFloat(6))   // distSq at 6 tiles

	closeScore := scoreTarget(closeDist, false, 1.0, component.ArmorLight)
	cmdScore := scoreTarget(cmdDist, true, 1.0, component.ArmorLight)

	t.Logf("close unit score: %.3f, commander score: %.3f", closeScore, cmdScore)
	if cmdScore <= closeScore {
		t.Errorf("commander should score higher than close unit: cmd=%.3f unit=%.3f",
			cmdScore, closeScore)
	}
}

// Test force-ratio retreat: AI retreats when badly outnumbered.
func TestAIForceRatioRetreat(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander — only unit in squad, moderate HP (below 60% threshold)
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 80, MaxHP: 200}) // 40% HP
	boidPool.Add(aiCmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// 5 enemy units visible — overwhelming force vs 1 AI unit
	for i := 0; i < 5; i++ {
		enemy := em.Create()
		px := fixed.FromFloat(23 + float64(i))
		posPool.Add(enemy, component.PositionComponent{X: px, Y: fixed.FromFloat(30)})
		ownerPool.Add(enemy, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
		healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})
	}

	sys := NewAISystem(2, nil, 64, 64)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetBasePosition(fixed.FromFloat(2), fixed.FromFloat(30))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// AI should retreat (outnumbered 5:1 with <60% HP)
	if sys.States[1].State != StateRetreat {
		t.Errorf("expected StateRetreat when outnumbered with low HP, got state %d",
			sys.States[1].State)
	}
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Error("expected retreat move command when outnumbered")
	}
	// Should be moving toward base, not toward enemies
	if len(moveCmds) > 0 {
		targetX := fixed.ToFloat(moveCmds[0].TargetX)
		if targetX > 10 {
			t.Errorf("retreat should move toward base (x~2), got x=%.1f", targetX)
		}
	}
}

// Test offensive push: AI advances toward enemy base for elimination objective.
func TestAIOffensivePush(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander at spawn
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(5), Y: fixed.FromFloat(48)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 48, 96)
	sys.RegisterSquad(1, uint32(aiCmd))
	sys.SetObjective(&tilemap.Objective{Type: tilemap.ObjectiveElimination})
	sys.SetEnemyBasePosition(fixed.FromFloat(43), fixed.FromFloat(48)) // enemy spawn at other end

	// Use tick > ExploreDuration so exploration doesn't interfere
	cmds := sys.Update(200, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command for offensive push")
	}

	// AI should be moving toward enemy base direction (x increasing from 5 toward 43)
	target := moveCmds[len(moveCmds)-1]
	targetX := fixed.ToFloat(target.TargetX)
	if targetX <= 10 {
		t.Errorf("offensive push should move toward enemy base (x~43), got target x=%.1f", targetX)
	}
	t.Logf("offensive push: moving toward x=%.1f (enemy base at x=43)", targetX)
}

// Test wave-based recruitment: doesn't recruit during cooldown.
func TestAIRecruitWaveTiming(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 30}

	// First wave (tick 1, lastRecruitWave=0) should recruit immediately
	cmds1 := sys.recruitDecisions(1)
	if len(cmds1) == 0 {
		t.Error("first wave should recruit immediately with 30 gold")
	}

	// Second call shortly after — should be in cooldown (30 < 30*3=90 gold)
	sys.PlayerGold[2] = 30 // reset gold
	cmds2 := sys.recruitDecisions(10) // only 9 ticks later (< 60 interval)
	if len(cmds2) != 0 {
		t.Errorf("expected 0 recruits during wave cooldown, got %d", len(cmds2))
	}

	// After cooldown expires — should recruit again
	sys.PlayerGold[2] = 30
	cmds3 := sys.recruitDecisions(70) // 69 ticks later (> 60 interval)
	if len(cmds3) == 0 {
		t.Error("expected recruits after wave cooldown expires")
	}
}

// Test wave recruitment: excessive gold bypasses cooldown.
func TestAIRecruitWaveBypass(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.PlayerGold = map[uint32]int32{2: 30}

	// First wave
	sys.recruitDecisions(1)

	// Large gold accumulation should bypass cooldown
	sys.PlayerGold[2] = 200
	cmds := sys.recruitDecisions(10) // only 9 ticks later
	if len(cmds) == 0 {
		t.Error("expected recruits when gold is excessive (3x wave minimum)")
	}
}

// Test enemy intel persistence: intel survives beyond the sighting tick.
func TestAIIntelPersistence(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)

	// Simulate enemy sightings
	sys.EnemyUnits[component.UnitSniper] = 10
	sys.EnemyUnits[component.UnitHeavyInfantry] = 5

	// Intel should persist until decay
	ratio := sys.adaptiveRoleRatio()
	if ratio == roleTargetRatio {
		t.Error("expected adaptive ratio with intel present, got default")
	}

	// After decay cycle, intel should be reduced but still present
	sys.decayIntel()
	if sys.EnemyUnits[component.UnitSniper] == 0 {
		t.Error("intel should persist after one decay cycle")
	}
	t.Logf("after decay: Snipers=%d, HeavyInf=%d",
		sys.EnemyUnits[component.UnitSniper], sys.EnemyUnits[component.UnitHeavyInfantry])

	// After many decay cycles, intel should be gone
	for i := 0; i < 20; i++ {
		sys.decayIntel()
	}
	if len(sys.EnemyUnits) > 0 {
		t.Errorf("expected intel to fully decay, still have: %v", sys.EnemyUnits)
	}
}

// Test squad assessment computes correct stats.
func TestAISquadAssessment(t *testing.T) {
	em := ecs.NewEntityManager()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

	// AI commander (LightInfantry, range 5)
	cmd := em.Create()
	healthPool.Add(cmd, component.HealthComponent{HP: 100, MaxHP: 100})
	unitTypePool.Add(cmd, component.UnitTypeComponent{Type: component.UnitLightInfantry})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// Squad member: Sniper (range 8)
	sniper := em.Create()
	healthPool.Add(sniper, component.HealthComponent{HP: 30, MaxHP: 30})
	unitTypePool.Add(sniper, component.UnitTypeComponent{Type: component.UnitSniper})
	boidPool.Add(sniper, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	// Squad member: MotorArtillery (range 7, heavy armor).
	// BoidComponent.Role only distinguishes commander from non-commander;
	// the tactical role is derived from UnitType via the unitRole map.
	arty := em.Create()
	healthPool.Add(arty, component.HealthComponent{HP: 150, MaxHP: 150})
	unitTypePool.Add(arty, component.UnitTypeComponent{
		Type:  component.UnitMotorArtillery,
		Armor: component.ArmorHeavy,
	})
	boidPool.Add(arty, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	sys := NewAISystem(2, nil, 64, 64)
	cmdHealth, _ := healthPool.Get(cmd)
	a := sys.assessSquad(1, boidPool, healthPool, unitTypePool, cmd, &cmdHealth)

	if a.UnitCount != 3 {
		t.Errorf("UnitCount = %d, want 3", a.UnitCount)
	}
	if a.TotalHP != 280 {
		t.Errorf("TotalHP = %d, want 280", a.TotalHP)
	}
	// MaxRange should be 8 (Sniper) = fixed.FromFloat(8.0)
	expectedMax := fixed.FromFloat(8.0)
	if a.MaxRange != expectedMax {
		t.Errorf("MaxRange = %d, want %d (Sniper range 8)", a.MaxRange, expectedMax)
	}
	if a.Strength != 4 { // 1 (LI) + 1 (Sniper) + 2 (Heavy armor Arty)
		t.Errorf("Strength = %d, want 4", a.Strength)
	}
	if !a.IsRangedDominant() {
		t.Errorf("expected ranged dominant (2 ranged vs 1 melee), got ranged=%d melee=%d",
			a.RangedCount, a.MeleeCount)
	}
	// CommitRange for ranged-dominant squad should be max range (8)
	commit := a.CommitRange()
	if commit != expectedMax {
		t.Errorf("CommitRange = %d, want %d for ranged-dominant squad", commit, expectedMax)
	}
}

// Test that SetEnemyBasePosition stores the values correctly.
func TestAISetEnemyBasePosition(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96)
	sys.SetEnemyBasePosition(fixed.FromFloat(43), fixed.FromFloat(48))
	if fixed.ToFloat(sys.EnemyBaseX) != 43 {
		t.Errorf("EnemyBaseX = %.1f, want 43", fixed.ToFloat(sys.EnemyBaseX))
	}
	if !sys.hasEnemyBase() {
		t.Error("hasEnemyBase should return true after SetEnemyBasePosition")
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
