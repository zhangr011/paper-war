package game

import (
	"context"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/persist"
)

// TestHandleCommandRecruit verifies the CmdRecruit case in HandleCommand
// end-to-end: it should locate the player's commander, queue a recruit
// request, and on the next Tick the new unit should be spawned and gold
// deducted.
//
// Regression test for the bug where HandleCommand's switch had no
// `case network.CmdRecruit` — recruit commands were silently dropped
// for human players (only AI recruits worked).
func TestHandleCommandRecruit(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()
	player, err := store.FindOrCreatePlayer(context.Background(), "test-token-recruit", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer: %v", err)
	}
	playerID := player.ID
	gs.PlayerGold[playerID] = 100

	squadID := uint32(1)
	gs.SpawnTeamWithType(playerID, squadID,
		fixed.FromFloat(10), fixed.FromFloat(10),
		1, component.UnitHeavyInfantry)

	// Find the player's commander.
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.Role == component.RoleCommander {
			if owner, ok := ownerPool.Get(e); ok && owner.PlayerID == playerID {
				cmdEntity = e
			}
		}
	})
	if cmdEntity == 0 {
		t.Fatal("setup: no commander found for player")
	}

	goldBefore := gs.PlayerGold[playerID]
	recruitCost := component.CombatUnitTypeTable[component.UnitLightInfantry].RecruitCost

	// Dispatch via HandleCommand — this is the path that was broken.
	gs.HandleCommand(playerID, &network.Command{
		Type:        network.CmdRecruit,
		RecruitType: uint8(component.UnitLightInfantry),
	})

	// Tick to process the queued recruit.
	gs.Tick()

	if gs.PlayerGold[playerID] != goldBefore-recruitCost {
		t.Fatalf("HandleCommand CmdRecruit: gold after = %d, want %d (before %d - cost %d)",
			gs.PlayerGold[playerID], goldBefore-recruitCost, goldBefore, recruitCost)
	}

	// Regression: a recruited unit MUST get a nonzero Speed. recruit.go used to
	// create VelocityComponent{} (Speed=0); the movement system clamps velocity
	// to [-Speed,Speed], so a zero Speed permanently immobilised the unit — and
	// when such a unit was promoted to commander the whole AI squad froze,
	// causing the solo-match stalemate.
	velPool := gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	gotSpeed := false
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander {
			return
		}
		vel, ok := velPool.Get(e)
		if ok && vel.Speed > 0 {
			gotSpeed = true
		}
	})
	if !gotSpeed {
		t.Fatalf("HandleCommand CmdRecruit: recruited unit has no nonzero Speed (stalemate bug regression)")
	}
}
