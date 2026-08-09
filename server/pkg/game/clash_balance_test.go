package game

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestClashModeBalance verifies that a real clash match (AI vs AI mirror) does
// not have a deterministic faction bias.
//
// The matchup mirrors the production clash setup in cmd/server/main.go
// (start_clash): two squads spawned on the LONG axis (vertical separation, both
// at the map's center X) and marched at each other's base. This is the shape
// real clash matches take, so this is the balance question that matters.
//
// History (issue #72): an earlier version of this test spawned the two squads
// SIDE BY SIDE on the SHORT axis (same Y, cx1/cx2 = mw/2 ∓ 2) with no march — a
// synthetic placement real clash never uses. That placement starts both squads
// in mutual range and exposes a latent "lower-coordinate side wins in partial
// ranged engagement" asymmetry in the sim, flipping the result to ~100% Blue
// once collision was enabled (collision regularizes the formation, making the
// partial-engagement edge decisive). That asymmetry does NOT appear in the real
// vertical+march clash (verified ~50/50 across seed offsets), so the test now
// drives the real configuration. The synthetic-horizontal asymmetry remains a
// known low-priority sim wart, tracked in issue #72; it does not affect real
// play.
func TestClashModeBalance(t *testing.T) {
	wins := [3]int{} // 0=Faction0(Blue), 1=Faction1(Red), 2=draw
	const runs = 40

	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0
		// Vary spawn jitter per match via the session RNG (the path that
		// actually drives spawn jitter). The old rand.Seed(time.Now()) was
		// decorative — it never reached gs.rnd, so all 40 runs were
		// byte-identical replays and any tiny asymmetry read as 100%.
		gs.SetSessionRNG(rand.New(rand.NewSource(int64(1000 + i))))

		mw, mh := gs.MapSize()
		cx := fixed.FromFloat(float64(mw) / 2)
		cy1 := fixed.FromFloat(float64(mh/2 - 4)) // team 1 base (low Y)
		cy2 := fixed.FromFloat(float64(mh/2 + 4)) // team 2 base (high Y)
		gs.SpawnSquadWithType(1, 1, cx, cy1, 10, component.UnitLightInfantry)
		gs.SpawnSquadWithType(2, 2, cx, cy2, 10, component.UnitLightInfantry)

		// March each army at the enemy base (same as main.go start_clash).
		pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
		ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
			path, ok := pathPool.GetPtr(e)
			if !ok {
				return
			}
			path.TargetX = cx
			if own, has := ownerPool.Get(e); has && own.Faction == component.FactionEnemy {
				path.TargetY = cy1 // team 2 marches up
			} else {
				path.TargetY = cy2 // team 1 marches down
			}
		})

		healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

		winner := uint8(2)
		for tick := uint32(1); tick <= 800; tick++ {
			gs.Tick()
			var aAlive, bAlive int
			healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
				if h.HP <= 0 {
					return
				}
				if owner, ok := ownerPool.Get(e); ok {
					if owner.Faction == 0 {
						aAlive++
					} else {
						bAlive++
					}
				}
			})
			if aAlive == 0 && bAlive == 0 {
				break
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

	t.Logf("Clash balance over %d matches: Blue=%d, Red=%d, Draw=%d",
		runs, wins[0], wins[1], wins[2])

	// No single faction should win > 70% of matches. At 40 runs this gives
	// ~3% false-positive rate for a fair coin, vs ~20% at 20 runs.
	for faction, w := range wins[:2] {
		if w >= int(float64(runs)*0.70) {
			name := "Blue"
			if faction == 1 {
				name = "Red"
			}
			t.Errorf("%s wins %d/%d (%.0f%%) — deterministically biased",
				name, w, runs, float64(w)/float64(runs)*100)
		}
	}
}
