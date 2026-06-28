package game

import (
	"context"
	"testing"

	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/persist"
)

// TestGameLoopBasicHappyPath tests the full game loop:
// start → spawn → tick → kill → gold → recruit → end → flush
func TestGameLoopBasicHappyPath(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()

	// Seed a player in the store
	player, err := store.FindOrCreatePlayer(context.Background(), "test-token", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer: %v", err)
	}
	playerID := player.ID
	gs.PlayerGold[playerID] = 50 // starting gold

	// Step 1: Spawn a team with HeavyInfantry commander
	squadID := uint32(1)
	gs.SpawnTeamWithType(playerID, squadID,
		fixed.FromFloat(10), fixed.FromFloat(10),
		1, component.UnitHeavyInfantry)

	cmdCount, unitCount := countSquadRoles(t, gs, squadID)
	if cmdCount != 1 {
		t.Fatalf("step1: commander count = %d, want 1", cmdCount)
	}
	if unitCount != InitialTeamCombatUnits {
		t.Fatalf("step1: combat units = %d, want %d", unitCount, InitialTeamCombatUnits)
	}

	// Verify commander type is HeavyInfantry
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	unitTypePool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	cmdTypeFound := false
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID && b.Role == component.RoleCommander {
			ut, ok := unitTypePool.Get(e)
			if !ok {
				t.Errorf("commander missing UnitTypeComponent")
				return
			}
			if ut.Type != component.UnitHeavyInfantry {
				t.Errorf("commander type = %v, want HeavyInfantry", ut.Type)
			}
			cmdTypeFound = true
		}
	})
	if !cmdTypeFound {
		t.Fatal("step1: no commander with HeavyInfantry type found")
	}

	// Step 2: Spawn an enemy unit (player 2)
	enemySquadID := uint32(2)
	gs.SpawnTeamWithType(2, enemySquadID,
		fixed.FromFloat(12), fixed.FromFloat(12),
		1, component.UnitLightInfantry)

	// Step 3: Simulate a kill — find enemy unit, set HP to 0, set LastAttacker to player1's unit
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	var player1Entity ecs.Entity
	var enemyEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.Role == component.RoleCommander {
			return // skip commanders
		}
		owner, ok := ownerPool.Get(e)
		if !ok {
			return
		}
		if owner.PlayerID == playerID {
			player1Entity = e
		} else if owner.PlayerID == 2 {
			enemyEntity = e
		}
	})

	if player1Entity == 0 || enemyEntity == 0 {
		t.Fatal("step3: could not find player1 and enemy combat units")
	}

	// Debug: verify OwnerComponent exists on killer
	killerOwner, ok := ownerPool.Get(player1Entity)
	if !ok {
		t.Fatal("step3: killer missing OwnerComponent")
	}
	t.Logf("killer playerID=%d, expected playerID=%d", killerOwner.PlayerID, playerID)

	// Set enemy HP to 0 with LastAttacker = player1's unit
	enemyHP, ok := healthPool.GetPtr(enemyEntity)
	if !ok {
		t.Fatal("step3: enemy missing health component")
	}
	t.Logf("enemy entity=%d, hp before=%d", enemyEntity, enemyHP.HP)
	enemyHP.HP = 0
	enemyHP.LastAttacker = uint32(player1Entity)
	t.Logf("enemy entity=%d, hp after=%d, lastAttacker=%d", enemyEntity, enemyHP.HP, enemyHP.LastAttacker)

	// Tick to process death
	t.Logf("before tick: lifecycle phase=%d, gold=%d", gs.Lifecycle.Phase, gs.PlayerGold[playerID])
	
	// Verify enemy HP is 0 before tick
	ehp, _ := healthPool.Get(enemyEntity)
	t.Logf("enemy HP before tick: %d, entity=%d", ehp.HP, enemyEntity)
	
	gs.Tick()
	t.Logf("after tick: gold=%d, deathSys bounties=%v", gs.PlayerGold[playerID], gs.deathSys.GoldBounties)

	// Step 4: Verify gold bounty awarded
	if gs.PlayerGold[playerID] <= 50 {
		t.Fatalf("step4: gold after kill = %d, want > 50 (bounty awarded)", gs.PlayerGold[playerID])
	}

	// LI KillBounty = 80% of 15g recruit cost = 12
	expectedBounty := component.CombatUnitTypeTable[component.UnitLightInfantry].KillBounty
	if gs.PlayerGold[playerID] != 50+expectedBounty {
		t.Fatalf("step4: gold = %d, want %d (50 + %d bounty)",
			gs.PlayerGold[playerID], 50+expectedBounty, expectedBounty)
	}

	// Verify GetGoldUpdates returns the current gold (not delta)
	updates := gs.GetGoldUpdates()
	if updates[playerID] != 50+expectedBounty {
		t.Fatalf("step4: GoldUpdate = %d, want %d (current gold)", updates[playerID], 50+expectedBounty)
	}

	// Step 5: Recruit a unit
	goldBefore := gs.PlayerGold[playerID]
	recruitCost := component.CombatUnitTypeTable[component.UnitLightInfantry].RecruitCost

	// Find the commander entity for recruit
	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID && b.Role == component.RoleCommander {
			cmdEntity = e
		}
	})
	if cmdEntity == 0 {
		t.Fatal("step5: commander entity not found")
	}

	gs.recruitSys.Recruit(combat.RecruitRequest{
		CommanderEntity: cmdEntity,
		UnitType:        component.UnitLightInfantry,
	})
	// Tick to process recruit
	gs.Tick()

	if gs.PlayerGold[playerID] != goldBefore-recruitCost {
		t.Fatalf("step5: gold after recruit = %d, want %d (gold %d - cost %d)",
			gs.PlayerGold[playerID], goldBefore-recruitCost, goldBefore, recruitCost)
	}

	// Verify new unit was spawned
	_, unitCountAfter := countSquadRoles(t, gs, squadID)
	if unitCountAfter <= unitCount {
		t.Fatalf("step5: combat units after recruit = %d, want > %d", unitCountAfter, unitCount)
	}

	// Step 6: Flush roster to store
	gs.Store = store
	gs.FlushRoster()

	roster, err := store.LoadRoster(context.Background(), playerID)
	if err != nil {
		t.Fatalf("step6: LoadRoster: %v", err)
	}
	if len(roster) == 0 {
		t.Fatal("step6: roster is empty after flush, expected at least 1 commander")
	}

	cmd := roster[0]
	if cmd.Type != "HeavyInfantry" {
		t.Fatalf("step6: flushed commander type = %s, want HeavyInfantry", cmd.Type)
	}
	if len(cmd.Units) == 0 {
		t.Fatal("step6: flushed commander has no units, expected survivors")
	}
}

// TestGameLoopAllDeadGrantsStarterRoster tests that when all commanders
// die, the player gets a starter roster via CreateStarterRoster.
func TestGameLoopAllDeadGrantsStarterRoster(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()

	player, err := store.FindOrCreatePlayer(context.Background(), "dead-player", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer: %v", err)
	}
	playerID := player.ID

	// Spawn a team
	squadID := uint32(1)
	gs.SpawnTeam(playerID, squadID,
		fixed.FromFloat(10), fixed.FromFloat(10), 1)

	// Kill the commander
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID && b.Role == component.RoleCommander {
			cmdEntity = e
		}
	})
	if cmdEntity == 0 {
		t.Fatal("commander not found")
	}

	cmdHP, ok := healthPool.GetPtr(cmdEntity)
	if !ok {
		t.Fatal("commander missing health")
	}
	cmdHP.HP = 0

	// Also kill all combat units (they will be promoted if alive)
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID && b.Role != component.RoleCommander {
			if hp, ok := healthPool.GetPtr(e); ok {
				hp.HP = 0
			}
		}
	})

	gs.Tick()

	// Verify all entities are gone
	aliveCount := 0
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID {
			aliveCount++
		}
	})
	if aliveCount != 0 {
		t.Fatalf("alive units after all-dead tick = %d, want 0", aliveCount)
	}

	// Flush roster — should grant starter roster
	gs.Store = store
	gs.FlushRoster()

	roster, err := store.LoadRoster(context.Background(), playerID)
	if err != nil {
		t.Fatalf("LoadRoster after all-dead: %v", err)
	}
	if len(roster) == 0 {
		t.Fatal("roster empty after all-dead flush — expected starter roster")
	}
	if roster[0].Type != "LightInfantry" {
		t.Fatalf("starter commander type = %s, want LightInfantry", roster[0].Type)
	}
}

// TestGameLoopRecruitNoGoldFails verifies recruiting with 0 gold fails.
func TestGameLoopRecruitNoGoldFails(t *testing.T) {
	gs := NewGameSession()
	gs.PlayerGold[1] = 0

	squadID := uint32(1)
	gs.SpawnTeam(1, squadID, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	_, unitCountBefore := countSquadRoles(t, gs, squadID)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadID && b.Role == component.RoleCommander {
			cmdEntity = e
		}
	})

	gs.recruitSys.Recruit(combat.RecruitRequest{
		CommanderEntity: cmdEntity,
		UnitType:        component.UnitLightInfantry,
	})
	gs.Tick()

	_, unitCountAfter := countSquadRoles(t, gs, squadID)
	if unitCountAfter != unitCountBefore {
		t.Fatalf("recruit with 0 gold: units = %d, want %d (no change)",
			unitCountAfter, unitCountBefore)
	}
	if gs.PlayerGold[1] != 0 {
		t.Fatalf("gold after failed recruit = %d, want 0", gs.PlayerGold[1])
	}
}

// TestGameLoopMatchEndTriggersFlush tests the lifecycle integration:
// Start → End → ShouldFlush → FlushRoster
func TestGameLoopMatchEndTriggersFlush(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()
	gs.Store = store

	player, _ := store.FindOrCreatePlayer(context.Background(), "lifecycle-player", "")
	playerID := player.ID

	flushed := false
	gs.Lifecycle = NewMatchLifecycle(
		func() {}, // onStart
		func(winner uint8, reason string) {
			// On match end, flush roster
			gs.FlushRoster()
			flushed = true
		},
	)
	gs.Lifecycle.FlushSec = 0 // immediate for test

	// Spawn and start match
	gs.SpawnTeam(playerID, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)
	gs.Lifecycle.Start()

	if gs.Lifecycle.Phase != PhasePlaying {
		t.Fatalf("phase after start = %d, want PhasePlaying", gs.Lifecycle.Phase)
	}

	// End the match
	gs.Lifecycle.End(1, "test_end")

	if gs.Lifecycle.Phase != PhaseEnded {
		t.Fatalf("phase after end = %d, want PhaseEnded", gs.Lifecycle.Phase)
	}

	if !flushed {
		t.Fatal("onEnd callback did not trigger FlushRoster")
	}

	roster, err := store.LoadRoster(context.Background(), playerID)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	if len(roster) == 0 {
		t.Fatal("roster empty after lifecycle end — expected flushed survivors")
	}
}

// TestGameLoopGoldUpdateAfterKill verifies GetGoldUpdates returns
// correct values and resets after read.
func TestGameLoopGoldUpdateAfterKill(t *testing.T) {
	gs := NewGameSession()
	gs.PlayerGold[1] = 50
	gs.PlayerGold[2] = 50

	// Spawn two teams close together
	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(12), fixed.FromFloat(12), 1, component.UnitLightInfantry)

	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	// Find a player1 combat unit and a player2 combat unit
	var p1Unit, p2Unit ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.Role == component.RoleCommander {
			return
		}
		owner, ok := ownerPool.Get(e)
		if !ok {
			return
		}
		if owner.PlayerID == 1 && p1Unit == 0 {
			p1Unit = e
		}
		if owner.PlayerID == 2 && p2Unit == 0 {
			p2Unit = e
		}
	})

	// Kill player2's unit with player1's unit as attacker
	hp, _ := healthPool.GetPtr(p2Unit)
	hp.HP = 0
	hp.LastAttacker = uint32(p1Unit)

	gs.Tick()

	// Player1 should have bounty
	expectedBounty := component.CombatUnitTypeTable[component.UnitLightInfantry].KillBounty
	if gs.PlayerGold[1] != 50+expectedBounty {
		t.Fatalf("player1 gold = %d, want %d", gs.PlayerGold[1], 50+expectedBounty)
	}

	// Player2 gold unchanged
	if gs.PlayerGold[2] != 50 {
		t.Fatalf("player2 gold = %d, want 50", gs.PlayerGold[2])
	}

	// GoldUpdates should reflect the change (returns current gold, not delta)
	updates := gs.GetGoldUpdates()
	if updates[1] != 50+expectedBounty {
		t.Fatalf("GoldUpdate[1] = %d, want %d", updates[1], 50+expectedBounty)
	}

	// Second call should return empty (already consumed)
	updates2 := gs.GetGoldUpdates()
	if len(updates2) != 0 {
		t.Fatalf("second GetGoldUpdates = %v, want empty", updates2)
	}
}
