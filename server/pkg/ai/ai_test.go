package ai

import (
	"math"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestNewAISystem(t *testing.T) {
	sys := NewAISystem(2, nil, 64, 64, nil)
	if sys.AIPlayerID != 2 {
		t.Error("expected AI player ID 2")
	}
}

func TestRegisterSquad(t *testing.T) {
	sys := NewAISystem(2, nil, 64, 64, nil)
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
	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)
	sys.PlayerGold = map[uint32]int32{2: 10}
	cmds := sys.recruitDecisions(1)
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruit commands with 10 gold, got %d", len(cmds))
	}
}

func TestAIRecruitRoleBalance(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)
	sys.PlayerGold = map[uint32]int32{2: 1000}
	cmds := sys.recruitDecisions(1)
	if len(cmds) > 3 {
		t.Errorf("AI recruited %d units in one tick, max is 3", len(cmds))
	}
}

func TestAIRecruitNoGoldMap(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)
	cmds := sys.recruitDecisions(1)
	if len(cmds) != 0 {
		t.Errorf("expected 0 recruits with nil PlayerGold, got %d", len(cmds))
	}
}

func TestAICheapestAffordableUnit(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)

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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

// TestAIStrongholdCaptureSkipsOwned (#56 phase 2): with two strongholds — one
// owned by the AI, one neutral — the AI must target the neutral (capturable)
// one, even though the owned one is closer.
func TestAIStrongholdCaptureSkipsOwned(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmd))
	// [0] owned by this AI (enemy) and very close; [1] neutral and farther.
	sys.SetStrongholds([][2]int32{{22, 30}, {40, 30}})
	sys.SetStrongholdFactions([]uint8{component.FactionEnemy, component.FactionNeutral})
	sys.SetAIFaction(component.FactionEnemy)

	cmds := sys.Update(100, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected a move toward the capturable (neutral) stronghold")
	}
	tx := fixed.ToFloat(moveCmds[0].TargetX)
	ty := fixed.ToFloat(moveCmds[0].TargetY)
	if int(tx) != 40 || int(ty) != 30 {
		t.Errorf("targeted (%.0f,%.0f), want neutral (40,30); owned (22,30) should be skipped", tx, ty)
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

	sys := NewAISystem(2, fogSys, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Early game with fog — should move (exploring or patrolling)
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected move command during exploration phase")
	}
}

func TestAIAdaptiveRecruitment(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)
	sys.SetBasePosition(fixed.FromFloat(30), fixed.FromFloat(40))
	if fixed.ToFloat(sys.BaseX) != 30 {
		t.Errorf("BaseX = %.1f, want 30", fixed.ToFloat(sys.BaseX))
	}
	if fixed.ToFloat(sys.BaseY) != 40 {
		t.Errorf("BaseY = %.1f, want 40", fixed.ToFloat(sys.BaseY))
	}
}

func TestAISetStrongholds(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil) // no fog — can see everything
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
// A squad with Snipers (range 4) should hold at range 4 and fire.
func TestAIRangeAwareEngagement(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})
	boidPool.Add(aiCmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})

	// AI sniper unit (range 4) in the squad
	sniper := em.Create()
	posPool.Add(sniper, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	ownerPool.Add(sniper, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(sniper, component.HealthComponent{HP: 30, MaxHP: 30})
	unitTypePool.Add(sniper, component.UnitTypeComponent{Type: component.UnitSniper})
	boidPool.Add(sniper, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	// Second sniper so the squad is ranged-dominant (2 ranged > 1 melee cmd)
	// and CommitRange = MaxRange (4). ADR-0027 brought back commit-range
	// checks; without a second ranged unit, CommitRange falls back to 5.
	sniper2 := em.Create()
	posPool.Add(sniper2, component.PositionComponent{X: fixed.FromFloat(20), Y: fixed.FromFloat(30)})
	ownerPool.Add(sniper2, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(sniper2, component.HealthComponent{HP: 30, MaxHP: 30})
	unitTypePool.Add(sniper2, component.UnitTypeComponent{Type: component.UnitSniper})
	boidPool.Add(sniper2, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	// Enemy at distance 3.5 tiles — within sniper range (4) but beyond old hardcoded 5
	enemy := em.Create()
	posPool.Add(enemy, component.PositionComponent{X: fixed.FromFloat(23.5), Y: fixed.FromFloat(30)})
	ownerPool.Add(enemy, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy, component.HealthComponent{HP: 100, MaxHP: 100})

	sys := NewAISystem(2, nil, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Should have issued an attack command (enemy is within squad max range 4)
	attackCmds := filterCmds(cmds, CmdAttack)
	if len(attackCmds) == 0 {
		t.Fatal("expected attack command when enemy within squad max range (4 tiles for sniper)")
	}
	t.Logf("sniper squad engaged at distance 3.5 tiles (range 4) — v1 would have approached instead")
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 64, 64, nil)
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

	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)
	sys.PlayerGold = map[uint32]int32{2: 30}

	// First wave (tick 1, lastRecruitWave=0) should recruit immediately
	cmds1 := sys.recruitDecisions(1)
	if len(cmds1) == 0 {
		t.Error("first wave should recruit immediately with 30 gold")
	}

	// Second call shortly after — should be in cooldown (30 < 30*3=90 gold)
	sys.PlayerGold[2] = 30            // reset gold
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
	sys := NewAISystem(2, nil, 48, 96, nil)
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
	sys := NewAISystem(2, nil, 48, 96, nil)

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

	// Squad member: Sniper (range 4)
	sniper := em.Create()
	healthPool.Add(sniper, component.HealthComponent{HP: 30, MaxHP: 30})
	unitTypePool.Add(sniper, component.UnitTypeComponent{Type: component.UnitSniper})
	boidPool.Add(sniper, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	// Squad member: MotorArtillery (range 4, heavy armor).
	// BoidComponent.Role only distinguishes commander from non-commander;
	// the tactical role is derived from UnitType via the unitRole map.
	arty := em.Create()
	healthPool.Add(arty, component.HealthComponent{HP: 150, MaxHP: 150})
	unitTypePool.Add(arty, component.UnitTypeComponent{
		Type:  component.UnitMotorArtillery,
		Armor: component.ArmorHeavy,
	})
	boidPool.Add(arty, component.BoidComponent{SquadID: 1, Role: component.RoleRanged})

	sys := NewAISystem(2, nil, 64, 64, nil)
	cmdHealth, _ := healthPool.Get(cmd)
	a := sys.assessSquad(1, boidPool, healthPool, unitTypePool, cmd, &cmdHealth)

	if a.UnitCount != 3 {
		t.Errorf("UnitCount = %d, want 3", a.UnitCount)
	}
	if a.TotalHP != 280 {
		t.Errorf("TotalHP = %d, want 280", a.TotalHP)
	}
	// MaxRange should be 4 (Sniper) = fixed.FromFloat(4.0)
	expectedMax := fixed.FromFloat(4.0)
	if a.MaxRange != expectedMax {
		t.Errorf("MaxRange = %d, want %d (Sniper range 4)", a.MaxRange, expectedMax)
	}
	if a.Strength != 4 { // 1 (LI) + 1 (Sniper) + 2 (Heavy armor Arty)
		t.Errorf("Strength = %d, want 4", a.Strength)
	}
	if !a.IsRangedDominant() {
		t.Errorf("expected ranged dominant (2 ranged vs 1 melee), got ranged=%d melee=%d",
			a.RangedCount, a.MeleeCount)
	}
	// CommitRange for ranged-dominant squad should be max range (4)
	commit := a.CommitRange()
	if commit != expectedMax {
		t.Errorf("CommitRange = %d, want %d for ranged-dominant squad", commit, expectedMax)
	}
}

// Test that SetEnemyBasePosition stores the values correctly.
func TestAISetEnemyBasePosition(t *testing.T) {
	sys := NewAISystem(2, nil, 48, 96, nil)
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

// Issue #52 / ADR-0027 — Guard policy, revised. When a squad detects an
// enemy IN CommitRange, it holds ground (StateGuard) and fires (CmdAttack)
// with no CmdMove. (The v3 test asserted Guard for any detected enemy;
// ADR-0027 narrows Guard to in-range targets — out-of-range enemies now
// trigger StateApproach, see TestAIApproachOutOfRangeEnemy.)
func TestAIGuardOnEnemyDetected(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	// AI commander standing at (30, 30), full HP. No squad — melee-dominant,
	// so CommitRange is DefaultEngageRange (2.5 tiles).
	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	// Enemy commander within CommitRange (2 tiles, < 2.5).
	enemy := em.Create()
	posPool.Add(enemy, component.PositionComponent{X: fixed.FromFloat(32), Y: fixed.FromFloat(30)})
	cmdPool.Add(enemy, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemy, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmd))

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.States[1].State != StateGuard {
		t.Errorf("expected StateGuard for in-range enemy, got %d", sys.States[1].State)
	}
	attackCmds := filterCmds(cmds, CmdAttack)
	if len(attackCmds) == 0 {
		t.Error("expected a CmdAttack against the detected enemy")
	}
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) != 0 {
		t.Errorf("Guard must not move, got %d CmdMove(s)", len(moveCmds))
	}
}

// Issue #52 — when no enemies remain, the squad exits Guard and returns
// to a non-combat state (Patrol/Idle/strategic). The no-enemy branch
// already handles this; the test guards against a regression that would
// leave squads stuck in Guard forever.
func TestAIGuardExitsWhenNoEnemies(t *testing.T) {
	em, _, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool := setupTestWorld()

	aiCmd := em.Create()
	posPool.Add(aiCmd, component.PositionComponent{X: fixed.FromFloat(30), Y: fixed.FromFloat(30)})
	cmdPool.Add(aiCmd, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmd, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmd, component.HealthComponent{HP: 200, MaxHP: 200})

	sys := NewAISystem(2, nil, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmd))
	// Force the squad into Guard to start; no enemies in the world.
	sys.States[1].State = StateGuard

	_ = sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.States[1].State == StateGuard {
		t.Error("squad must leave Guard when no enemies are detected")
	}
}

// ============================================================================
// ADR-0027 TESTS — bounded engagement
// ============================================================================

// setupSquadWithCommander is a helper for the ADR-0027 tests: an AI
// commander at (x,y), full HP, no squad members (melee-dominant →
// CommitRange = DefaultEngageRange = 5 tiles).
func setupSquadWithCommander(t *testing.T, x, y float64) (
	em *ecs.EntityManager,
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	boidPool *ecs.ComponentPool[component.BoidComponent],
	sys *AISystem,
	aiCmdEntity ecs.Entity,
) {
	t.Helper()
	em2, _, cmdPool2, posPool2, ownerPool2, healthPool2, unitTypePool2, boidPool2 := setupTestWorld()
	em = em2
	cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool =
		cmdPool2, posPool2, ownerPool2, healthPool2, unitTypePool2, boidPool2
	aiCmdEntity = em.Create()
	posPool.Add(aiCmdEntity, component.PositionComponent{X: fixed.FromFloat(x), Y: fixed.FromFloat(y)})
	cmdPool.Add(aiCmdEntity, component.CommanderComponent{SquadID: 1, IsAlive: true})
	ownerPool.Add(aiCmdEntity, component.OwnerComponent{PlayerID: 2, Faction: component.FactionEnemy})
	healthPool.Add(aiCmdEntity, component.HealthComponent{HP: 200, MaxHP: 200})
	sys = NewAISystem(2, nil, 64, 64, nil)
	sys.RegisterSquad(1, uint32(aiCmdEntity))
	return
}

// addEnemyAt adds an enemy commander entity at (x,y) and returns its entity ID.
func addEnemyAt(
	em *ecs.EntityManager,
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	x, y float64,
) ecs.Entity {
	enemy := em.Create()
	posPool.Add(enemy, component.PositionComponent{X: fixed.FromFloat(x), Y: fixed.FromFloat(y)})
	cmdPool.Add(enemy, component.CommanderComponent{SquadID: 2, IsAlive: true})
	ownerPool.Add(enemy, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	healthPool.Add(enemy, component.HealthComponent{HP: 200, MaxHP: 200})
	return enemy
}

// Test 1: Out-of-range enemy → StateApproach + CmdMove toward (not onto) enemy.
func TestAIApproachOutOfRangeEnemy(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 30, 30)

	// Enemy 15 tiles away — beyond CommitRange (2.5). Along the X axis for easy math.
	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 45, 30)

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.States[1].State != StateApproach {
		t.Fatalf("expected StateApproach for out-of-range enemy, got %d", sys.States[1].State)
	}
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected a CmdMove closing on the enemy")
	}
	// The move target must be one tile SHORT of CommitRange (2.5) from the
	// enemy — the flow field's tile quantization makes units stop up to a
	// full tile early, so the AI aims closer to keep the parked squad in
	// fire range (issue #74). Enemy at x=45, squad at x=30 → target ≈ x=43.5.
	tx := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetX)
	ty := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetY)
	if ty != 30 {
		t.Errorf("expected y=30 (axis-aligned), got y=%.2f", ty)
	}
	if tx <= 30 || tx >= 45 {
		t.Errorf("expected target between squad and enemy (30<x<45), got x=%.2f", tx)
	}
	if math.Abs(tx-43.5) > 0.5 {
		t.Errorf("expected target ≈ 43.5 (enemy 45 − CommitRange 2.5 + 1-tile quantization slack), got x=%.2f", tx)
	}
	// Anchor must be recorded at the squad's current position on first detection.
	if sys.States[1].EngageEnemyID != uint32(enemy) {
		t.Errorf("EngageEnemyID = %d, want %d", sys.States[1].EngageEnemyID, uint32(enemy))
	}
	if sys.States[1].EngageAnchorX != fixed.FromFloat(30) ||
		sys.States[1].EngageAnchorY != fixed.FromFloat(30) {
		t.Errorf("EngageAnchor = (%d,%d), want squad pos (30,30)",
			sys.States[1].EngageAnchorX, sys.States[1].EngageAnchorY)
	}
}

// Test 2: In-range enemy → StateGuard + CmdAttack (today's behavior preserved).
func TestAIGuardInRangeEnemy(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 30, 30)

	addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 32, 30) // 2 tiles, within CommitRange 2.5

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.States[1].State != StateGuard {
		t.Fatalf("expected StateGuard for in-range enemy, got %d", sys.States[1].State)
	}
	if len(filterCmds(cmds, CmdAttack)) == 0 {
		t.Error("expected a CmdAttack against the in-range enemy")
	}
	if len(filterCmds(cmds, CmdMove)) != 0 {
		t.Error("in-range Guard must not emit CmdMove")
	}
}

// Test 3: Break-off on kite — squad pulled beyond MaxPursuitDist from anchor
// → AvoidUnitID/AvoidUntilTick set, EngageEnemyID cleared, CmdMove toward
// anchor, StateGuard.
func TestAIBreakOffOnKite(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 30, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 45, 30)

	// Simulate the squad having already chased 10 tiles from an anchor at
	// (20, 30) toward this same enemy — beyond MaxPursuitDist (8). Pre-seed
	// the state so the next Update sees the kite condition.
	st := sys.States[1]
	st.EngageEnemyID = uint32(enemy)
	st.EngageAnchorX = fixed.FromFloat(20)
	st.EngageAnchorY = fixed.FromFloat(30)
	st.AvoidUnitID = 0
	st.AvoidUntilTick = 0

	cmds := sys.Update(5, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if st.AvoidUnitID != uint32(enemy) {
		t.Errorf("AvoidUnitID = %d, want enemy %d", st.AvoidUnitID, uint32(enemy))
	}
	if st.AvoidUntilTick <= 5 {
		t.Errorf("AvoidUntilTick = %d, want > current tick 5", st.AvoidUntilTick)
	}
	if st.EngageEnemyID != 0 {
		t.Errorf("EngageEnemyID = %d, want 0 (cleared on break-off)", st.EngageEnemyID)
	}
	if st.State != StateGuard {
		t.Errorf("State = %d, want StateGuard (%d)", st.State, StateGuard)
	}
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected a CmdMove back toward the anchor")
	}
	tx := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetX)
	ty := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetY)
	if math.Abs(tx-20) > 0.5 || math.Abs(ty-30) > 0.5 {
		t.Errorf("break-off should move toward anchor (20,30), got (%.1f,%.1f)", tx, ty)
	}
}

// Test 4: Avoid-cooldown respected — same enemy re-appears as best target
// within AvoidUntilTick → squad skips combat (no Approach/Attack) and falls
// through to strategic behavior.
func TestAIAvoidCooldownSkipsCombat(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 30, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 45, 30)

	// Enemy is on avoid-cooldown until tick 100. Current tick is 5.
	st := sys.States[1]
	st.AvoidUnitID = uint32(enemy)
	st.AvoidUntilTick = 100
	// Force a fresh eval at tick 5.
	st.NextEvalTick = 5

	cmds := sys.Update(5, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if st.State == StateApproach || st.State == StateGuard {
		t.Errorf("expected combat to be skipped (cooldown active), got State %d", st.State)
	}
	for _, c := range cmds {
		if c.Type == CmdAttack {
			t.Errorf("must not issue CmdAttack during avoid-cooldown; got %+v", c)
		}
	}
}

// Test 5: Cooldown expiry → normal combat resumes.
func TestAIAvoidCooldownExpiry(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 30, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 45, 30)

	// Cooldown expired at tick 100; we're now at tick 200.
	st := sys.States[1]
	st.AvoidUnitID = uint32(enemy)
	st.AvoidUntilTick = 100
	st.NextEvalTick = 200

	sys.Update(200, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if st.State != StateApproach {
		t.Errorf("expected combat to resume (Approach) after cooldown expiry, got State %d", st.State)
	}
	if st.EngageEnemyID != uint32(enemy) {
		t.Errorf("expected fresh engagement on the same enemy after expiry, got EngageEnemyID %d",
			st.EngageEnemyID)
	}
}

// Test 6: Emergency retreat still wins at CriticallyLowHP, even with an
// out-of-range enemy present (not Approach).
func TestAIRetreatBeatsApproach(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, aiCmd :=
		setupSquadWithCommander(t, 30, 30)
	// Drop HP below CriticallyLowHP (0.10). 1/200 = 0.005.
	hp, _ := healthPool.GetPtr(aiCmd)
	hp.HP = 1

	addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 45, 30) // out of range

	cmds := sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.States[1].State != StateRetreat {
		t.Fatalf("expected StateRetreat at CriticallyLowHP, got %d", sys.States[1].State)
	}
	if len(filterCmds(cmds, CmdMove)) == 0 {
		t.Error("expected a retreat CmdMove")
	}
}

// ============================================================================
// HUNT/SEEK — last-known position (LKP) tests. Plan: ai-hunt-seek.
// ============================================================================

// TestAI_HuntsLastKnownPosition: an enemy is in AI vision for a few ticks
// (LKP recorded), then is removed (out of vision). On the next eval the AI
// must issue a CmdMove toward the LKP within EvalInterval, NOT fall through
// to push/patrol. aiFog is nil in these tests, so scanEnemiesScored treats
// any alive enemy as visible; removing the enemy entity simulates losing
// contact.
func TestAI_HuntsLastKnownPosition(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 20, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 35, 30)

	// Tick once with the enemy visible → LKP should be recorded at (35,30).
	sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
	if sys.lastEnemySightingX != fixed.FromFloat(35) ||
		sys.lastEnemySightingY != fixed.FromFloat(30) {
		t.Fatalf("LKP = (%.1f,%.1f), want (35,30) after sighting",
			fixed.ToFloat(sys.lastEnemySightingX), fixed.ToFloat(sys.lastEnemySightingY))
	}
	if sys.lastEnemySightingTick == 0 {
		t.Fatal("lastEnemySightingTick should be set after a sighting")
	}

	// Enemy slips out of vision: kill it and drop HP to 0 so scanEnemiesScored
	// skips it (healthPool.Each returns early on hp.HP <= 0).
	ehp, _ := healthPool.GetPtr(enemy)
	ehp.HP = 0

	// Force a fresh eval at a tick within HuntMemoryTicks of the sighting.
	st := sys.States[1]
	st.NextEvalTick = 50

	cmds := sys.Update(50, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Squad should be hunting: StateScout, CmdMove targeting the LKP.
	if st.State != StateScout {
		t.Fatalf("expected StateScout (hunting LKP), got %d", st.State)
	}
	moveCmds := filterCmds(cmds, CmdMove)
	if len(moveCmds) == 0 {
		t.Fatal("expected a CmdMove toward the LKP")
	}
	tx := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetX)
	ty := fixed.ToFloat(moveCmds[len(moveCmds)-1].TargetY)
	if math.Abs(tx-35) > 0.5 || math.Abs(ty-30) > 0.5 {
		t.Errorf("hunt target = (%.1f,%.1f), want LKP (35,30)", tx, ty)
	}
}

// TestAI_LKPStaleFallsThrough: when the LKP is older than HuntMemoryTicks,
// huntCommand returns nil and the squad falls through to push/patrol (NOT
// StateScout toward the LKP).
func TestAI_LKPStaleFallsThrough(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 20, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 35, 30)

	// Record a sighting at tick 10.
	sys.Update(10, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	ehp, _ := healthPool.GetPtr(enemy)
	ehp.HP = 0

	// Eval at tick 10 + HuntMemoryTicks + 1 → stale.
	staleTick := uint32(11 + HuntMemoryTicks)
	st := sys.States[1]
	st.NextEvalTick = staleTick

	cmds := sys.Update(staleTick, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Must NOT have hunted: state must not be StateScout, and no CmdMove may
	// target the stale LKP.
	if st.State == StateScout {
		t.Fatalf("LKP stale (%d ticks old) should not trigger hunt (StateScout)",
			staleTick-10)
	}
	for _, c := range cmds {
		if c.Type == CmdMove {
			tx := fixed.ToFloat(c.TargetX)
			ty := fixed.ToFloat(c.TargetY)
			if math.Abs(tx-35) < 1.0 && math.Abs(ty-30) < 1.0 {
				t.Errorf("stale LKP hunt: CmdMove to (%.1f,%.1f) (the stale LKP); should fall through", tx, ty)
			}
		}
	}
}

// TestAI_LKPRefreshesOnReSight: after losing contact and hunting to the LKP,
// a fresh sighting at a new position must update the LKP to that new spot.
func TestAI_LKPRefreshesOnReSight(t *testing.T) {
	em, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 20, 30)

	enemy := addEnemyAt(em, cmdPool, posPool, ownerPool, healthPool, 35, 30)

	// First sighting at (35,30).
	sys.Update(1, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
	if sys.lastEnemySightingX != fixed.FromFloat(35) {
		t.Fatalf("initial LKP X = %.1f, want 35", fixed.ToFloat(sys.lastEnemySightingX))
	}

	// Lose contact briefly.
	ehp, _ := healthPool.GetPtr(enemy)
	ehp.HP = 0
	st := sys.States[1]
	st.NextEvalTick = 50
	sys.Update(50, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	// Re-sight at a new position: revive + move the enemy.
	ehp.HP = 200
	posPool.Add(ecs.Entity(enemy), component.PositionComponent{X: fixed.FromFloat(25), Y: fixed.FromFloat(40)})

	st.NextEvalTick = 100
	sys.Update(100, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.lastEnemySightingX != fixed.FromFloat(25) ||
		sys.lastEnemySightingY != fixed.FromFloat(40) {
		t.Errorf("LKP = (%.1f,%.1f) after re-sight, want (25,40)",
			fixed.ToFloat(sys.lastEnemySightingX), fixed.ToFloat(sys.lastEnemySightingY))
	}
	if sys.lastEnemySightingTick != 100 {
		t.Errorf("lastEnemySightingTick = %d, want 100 (refreshed)", sys.lastEnemySightingTick)
	}
}

// TestAI_NoSightingNoHunt: a squad that has never seen an enemy must not hunt
// (huntCommand returns nil, falls through to other behaviors).
func TestAI_NoSightingNoHunt(t *testing.T) {
	_, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool, sys, _ :=
		setupSquadWithCommander(t, 20, 30)

	st := sys.States[1]
	st.NextEvalTick = 100

	// No enemy in the world — no sighting should ever be recorded.
	cmds := sys.Update(100, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)

	if sys.lastEnemySightingTick != 0 {
		t.Errorf("lastEnemySightingTick = %d, want 0 (never sighted)", sys.lastEnemySightingTick)
	}
	if st.State == StateScout {
		t.Errorf("State = StateScout, want non-hunt; no LKP should ever hunt")
	}
	// Should still produce some strategic command (patrol/etc).
	if len(cmds) == 0 {
		t.Error("expected a non-hunt strategic command, got none")
	}
}
