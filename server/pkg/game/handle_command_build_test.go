package game

import (
	"context"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/persist"
)

// TestBuildGoldDeduction verifies that building a structure actually
// deducts gold from the player's balance. The BuildSystem deducts
// directly inside Build() (build.go:98 — s.PlayerGold[req.PlayerID]
// -= stats.Cost), NOT via a post-tick reconciliation loop like
// RecruitmentSystem. This test confirms that direct path works.
//
// Note: buildSys.Init() must have run for s.em to be non-nil. The
// Init runs in NewGameSession via gs.World.Init(). This test relies
// on the full NewGameSession setup, which is heavyweight but matches
// the production code path.
func TestBuildGoldDeduction(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()
	player, err := store.FindOrCreatePlayer(context.Background(), "test-token-build", "")
	if err != nil {
		t.Fatalf("FindOrCreatePlayer: %v", err)
	}
	playerID := player.ID
	gs.PlayerGold[playerID] = 200

	// Spawn a squad at a known position.  Build requires the player's
	// spawn to be registered in buildSys.PlayerSpawns; SpawnTeamWithType
	// doesn't do that, so register one manually at the same coords.
	const spawnX = 10.0
	const spawnY = 10.0
	squadID := uint32(1)
	gs.SpawnTeamWithType(playerID, squadID,
		fixed.FromFloat(spawnX), fixed.FromFloat(spawnY),
		1, component.UnitHeavyInfantry)
	if gs.buildSys != nil && gs.buildSys.PlayerSpawns != nil {
		gs.buildSys.PlayerSpawns[playerID] = [2]int64{
			fixed.FromFloat(spawnX), fixed.FromFloat(spawnY),
		}
	}
	// Wire the build system's PlayerGold view to the session map.
	if gs.buildSys != nil {
		gs.buildSys.PlayerGold = gs.PlayerGold
	}

	goldBefore := gs.PlayerGold[playerID]
	towerCost := component.StructureTypeTable[component.StructureWatchtower].Cost

	// Issue a CmdBuild via HandleCommand at the spawn position (in-range
	// — within BuildRange=10 tiles).
	gs.HandleCommand(playerID, &network.Command{
		Type:        network.CmdBuild,
		RecruitType: uint8(component.StructureWatchtower),
		TargetX:     int32(fixed.FromFloat(spawnX)),
		TargetY:     int32(fixed.FromFloat(spawnY)),
	})

	// Build() deducts gold synchronously inside HandleCommand — no Tick
	// required (unlike recruit, which queues for the next tick).
	if gs.PlayerGold[playerID] != goldBefore-towerCost {
		t.Fatalf("HandleCommand CmdBuild: gold after = %d, want %d (before %d - cost %d)",
			gs.PlayerGold[playerID], goldBefore-towerCost, goldBefore, towerCost)
	}
}
