package game

import (
	"math/rand"
	"testing"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestClashModeBalance verifies that clash mode (AI vs AI mirror match)
// does not have a deterministic faction bias. With MoveDisabled and
// symmetric formations, matches should be balanced.
func TestClashModeBalance(t *testing.T) {
	// Seed the RNG so each match varies.  Without this, Go defaults to
	// seed=1 and every match is a byte-identical replay, which makes any
	// tiny entity-order asymmetry look like a 100% deterministic bias.
	rand.Seed(time.Now().UnixNano())

	wins := [3]int{} // 0=Faction0(Blue), 1=Faction1(Red), 2=draw
	const runs = 40

	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0

		mw, mh := gs.MapSize()
		cx1 := fixed.FromFloat(float64(mw)/2 - 2)
		cx2 := fixed.FromFloat(float64(mw)/2 + 2)
		cy := fixed.FromFloat(float64(mh) / 2)

		gs.SpawnSquadWithType(1, 1, cx1, cy, 10, component.UnitLightInfantry)
		gs.SpawnSquadWithType(2, 2, cx2, cy, 10, component.UnitLightInfantry)

		healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

		winner := uint8(2)
		for tick := uint32(1); tick <= 500; tick++ {
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

	// No single faction should win > 70% of matches.  At 40 runs this
	// gives ~3% false-positive rate for a fair coin, vs ~20% at 20 runs.
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
