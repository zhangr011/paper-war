package game

import (
	"fmt"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestClashBalanceAfterFix verifies that mirror matchups in clash mode
// do not have a deterministic faction bias. Root cause was that
// SpawnSquadWithType only registered squads with AISys, not AISys2,
// leaving Blue team (playerID 1) with no AI.
func TestClashBalanceAfterFix(t *testing.T) {
	wins := [3]int{} // wins[faction] = count; faction 0 or 1, 2=draw/timeout
	const runs = 20

	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()

		mw, mh := gs.MapSize()
		cx1 := int64(mw/2 - 2)
		cx2 := int64(mw/2 + 2)
		cy := int64(mh / 2)

		gs.SpawnTeamWithType(1, 1, cx1, cy, 1, component.UnitLightInfantry)
		gs.SpawnTeamWithType(2, 2, cx2, cy, 1, component.UnitLightInfantry)

		healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

		winner := uint8(2) // default: draw/timeout
		for tick := uint32(1); tick <= 10000; tick++ {
			gs.Tick()
			var aAlive, bAlive int
			healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
				if h.HP <= 0 {
					return
				}
				owner, ok := ownerPool.Get(e)
				if !ok {
					return
				}
				if owner.Faction == 0 {
					aAlive++
				} else {
					bAlive++
				}
			})
			if aAlive == 0 && bAlive == 0 {
				break // mutual destruction
			}
			if aAlive == 0 {
				winner = 1
				break
			}
			if bAlive == 0 {
				winner = 0
				break
			}
		}
		wins[winner]++
	}

	t.Logf("Clash balance over %d mirror matches: Faction0=%d, Faction1=%d, Draw=%d",
		runs, wins[0], wins[1], wins[2])

	// Before fix: Faction1 won 100% (10/10).
	// After fix: we expect roughly even split. Allow up to 80% dominance
	// (combat has minor entity-ID first-strike bias, so it won't be exactly 50/50).
	maxFaction := wins[0]
	dominant := 0
	if wins[1] > maxFaction {
		maxFaction = wins[1]
		dominant = 1
	}
	if maxFaction >= int(float64(runs)*0.85) {
		t.Fatalf("Faction %d wins %d/%d (%.0f%%) — still deterministically biased after fix",
			dominant, maxFaction, runs, float64(maxFaction)/float64(runs)*100)
	}
}

// TestAISys2SquadRegistration verifies that squads spawned for playerID 1
// are registered with AISys2 in clash mode.
func TestAISys2SquadRegistration(t *testing.T) {
	gs := NewGameSession()
	gs.ResetWithMap(tilemap.LoadClashMap("plains"))
	gs.EnableClashMode()

	if gs.AISys2 == nil {
		t.Fatal("AISys2 should be created by EnableClashMode")
	}

	mw, mh := gs.MapSize()
	cx := int64(mw / 2)
	cy := int64(mh / 2)

	// Spawn a squad for player 1 (AISys2's playerID)
	gs.SpawnTeamWithType(1, 1, cx, cy, 1, component.UnitLightInfantry)

	if len(gs.AISys2.States) == 0 {
		t.Fatal("AISys2.States is empty — squad for playerID 1 was not registered with AISys2. " +
			"SpawnSquadWithType must register with AISys2 when playerID matches.")
	}

	if _, ok := gs.AISys2.States[1]; !ok {
		t.Fatalf("AISys2.States should contain squad 1, got: %v", gs.AISys2.States)
	}

	// Also verify player 2's squad is registered with AISys (not AISys2)
	gs.SpawnTeamWithType(2, 2, cx+5, cy, 1, component.UnitLightInfantry)
	if _, ok := gs.AISys.States[2]; !ok {
		t.Fatalf("AISys.States should contain squad 2, got: %v", gs.AISys.States)
	}
	if _, ok := gs.AISys2.States[2]; ok {
		t.Fatal("AISys2.States should NOT contain squad 2 (playerID 2 belongs to AISys)")
	}

	fmt.Printf("AISys states: %v, AISys2 states: %v\n", gs.AISys.States, gs.AISys2.States)
}
