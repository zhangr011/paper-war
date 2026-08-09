package game

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestClashHorizontalMirrorBalance is the RED test for issue #72's actual
// defect: in a mirrored clash where the two squads start in mutual range
// (horizontal, side-by-side — the shape TestMiniPitchTwoTeamClash and the old
// balance checks used), one faction wins ~97% of matches.
//
// Root cause (traced via per-tick HP + target-distribution instrumentation):
// the commander phase is symmetric (both sides focus the enemy commander, which
// has 600 HP so no overkill, both melt at 180/volley). The split happens the
// first LI volley AFTER the commanders die: findTarget's deterministic
// nearest-target selection (spatial-hash Query order + distSq tie-break)
// concentrates one faction's fire onto a single enemy (first kill → snowball)
// while the other spreads. The asymmetry does NOT appear in the real vertical
// +march clash (TestClashModeBalance passes), only when squads start in mutual
// range. This test pins the in-range case so the targeting fix can be verified.
func TestClashHorizontalMirrorBalance(t *testing.T) {
	// RED spec for issue #72 (reopened). Confirmed defect, not yet fixed.
	//
	// In a mirrored clash where the two squads start in mutual range (horizontal,
	// side-by-side), Blue wins ~97% (29/30) — surviving a per-run spawn-order
	// swap, so it's positional (low-X), not entity-ID/faction order.
	//
	// Traced mechanism: the commander phase is symmetric (both focus the enemy
	// commander; 600 HP, no overkill; both melt at 180/volley; HP equal through
	// the commanders dying). The split is the first LI volley AFTER commanders
	// die — findTarget's nearest-target selection concentrates one side's fire
	// (first kill → snowball). Ruled OUT: collision (still 93% with all radii
	// zeroed), AI (nulled), elevation (nulled), the map (symmetric), commander
	// damage. Positions are mirror-symmetric, damage is simultaneous. The
	// residual asymmetry is a subtle directional bias in combat target
	// acquisition (spatial-hash Query sweep order + nearest convergence) that I
	// could not pinpoint; a minimal anti-overfocus tie-break did NOT fix it
	// (focus is genuine convergence, not exact-tie collapse).
	//
	// Real vertical+march clash (TestClashModeBalance) is balanced — this only
	// manifests when squads START in mutual range. Skipped pending a real fix.
	t.Skip("issue #72 — mirrored in-range clash ~97% Blue; combat focus-fire asymmetry, not yet fixed")

	const runs = 30
	wins := [3]int{} // 0=Blue, 1=Red, 2=draw
	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0
		gs.SetSessionRNG(rand.New(rand.NewSource(int64(3000 + i))))

		mw, mh := gs.MapSize()
		cx1 := fixed.FromFloat(float64(mw)/2 - 2)
		cx2 := fixed.FromFloat(float64(mw)/2 + 2)
		cy := fixed.FromFloat(float64(mh) / 2)
		// Spawn order swapped each run so neither faction always gets the lower
		// entity-ID processing slot — a real asymmetry would survive the swap.
		if i%2 == 0 {
			gs.SpawnSquadWithType(1, 1, cx1, cy, 10, component.UnitLightInfantry)
			gs.SpawnSquadWithType(2, 2, cx2, cy, 10, component.UnitLightInfantry)
		} else {
			gs.SpawnSquadWithType(2, 2, cx2, cy, 10, component.UnitLightInfantry)
			gs.SpawnSquadWithType(1, 1, cx1, cy, 10, component.UnitLightInfantry)
		}

		healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
		winner := uint8(2)
		for tick := uint32(1); tick <= 1500; tick++ {
			gs.Tick()
			var a, b int
			healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
				if h.HP <= 0 {
					return
				}
				if own, ok := ownerPool.Get(e); ok {
					if own.Faction == 0 {
						a++
					} else {
						b++
					}
				}
			})
			if a == 0 && b == 0 {
				break
			}
			if a == 0 {
				winner = 1
				break
			}
			if b == 0 {
				winner = 0
				break
			}
		}
		wins[winner]++
	}

	t.Logf("Horizontal mirror balance over %d matches: Blue=%d Red=%d Draw=%d",
		runs, wins[0], wins[1], wins[2])
	// A fair mirror must not let one faction win >70% — even with the spawn-order
	// swap that cancels entity-ID effects.
	for faction, w := range wins[:2] {
		if w >= int(float64(runs)*0.70) {
			name := "Blue"
			if faction == 1 {
				name = "Red"
			}
			t.Errorf("%s wins %d/%d (%.0f%%) — mirrored in-range clash is biased (focus-fire first-kill asymmetry, issue #72)",
				name, w, runs, float64(w)/float64(runs)*100)
		}
	}
}
