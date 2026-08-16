package game

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestHillClashFireParity — regression guard for "the team on the hill lost
// the clash" (hill-map standoff investigation, 2026-08).
//
// On the production "hills" clash map, start_clash puts Team 1 on a Hill /
// Elevation-1 spawn and Team 2 on valley Plain. Both armies march at each
// other and lock into a fire standoff. The bug: the hill side was out-shot
// ~2:1 (64 vs 112 attack resolutions on seed 1000) because high-ground units
// acquired targets through the blanket elevation acquisition extension and
// planted at the edge of the engagement band, while the valley army walked
// into contact and fired continuously. Net effect: the hill spawn's elevation
// bonuses actively cost it the engagement — win rate fell to a coin flip
// instead of an advantage.
//
// Invariant: over a pinned-seed production-shaped clash, the hill team's
// total attack resolutions must stay within 20% of the valley team's. The
// high-ground bonuses (range +1, Hill cover) may shift WHERE units fire from,
// but may never halve the hill side's volume of fire.
func TestHillClashFireParity(t *testing.T) {
	const seed = int64(1000)
	gs := NewGameSession()
	gs.ResetWithMap(tilemap.LoadClashMap("hills"))
	gs.EnableClashMode()
	gs.Map.Objective.Type = 0
	gs.SetSessionRNG(rand.New(rand.NewSource(seed)))

	mw, mh := gs.MapSize()
	cx := fixed.FromFloat(float64(mw) / 2)
	cy1 := fixed.FromFloat(float64(mh/2 - 4)) // hill spawn (16,12): Hill, Elevation 1
	cy2 := fixed.FromFloat(float64(mh/2 + 4)) // valley spawn (16,20): Plain, Elevation 0
	gs.SpawnSquadWithType(1, 1, cx, cy1, 10, component.UnitLightInfantry)
	gs.SpawnSquadWithType(2, 2, cx, cy2, 10, component.UnitLightInfantry)

	// March each army at the enemy base (same as start_clash).
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

	cs := gs.World.SystemByName("CombatSystem").(*combat.CombatSystem)
	hpPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	var shots [2]int
	for tick := uint32(1); tick <= 400; tick++ {
		gs.Tick()
		for _, ar := range cs.AttackRecords {
			if own, ok := ownerPool.Get(ecs.Entity(ar.EntityID)); ok {
				shots[own.Faction]++
			}
		}
		var aAlive, bAlive int
		hpPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
			if h.HP > 0 {
				if o, ok := ownerPool.Get(e); ok {
					if o.Faction == 0 {
						aAlive++
					} else {
						bAlive++
					}
				}
			}
		})
		if aAlive == 0 || bAlive == 0 {
			break
		}
	}

	const minRatio = 0.8 // hill shots must be ≥ 80% of valley shots
	got := float64(shots[0]) / float64(shots[1])
	if got < minRatio {
		t.Errorf("hill team fired %d shots vs valley %d (ratio %.2f, want ≥ %.2f) — "+
			"high-ground acquisition is planting the hill army at the edge of the engagement band",
			shots[0], shots[1], got, minRatio)
	}
}
