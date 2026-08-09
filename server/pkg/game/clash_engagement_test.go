package game

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestClashProducesCombatOnAllMaps is the regression guard for "the clash is
// failed — they do not clash to each other." A clash match (production setup:
// EnableClashMode, two squads marched at each other) must produce combat on
// every clash map across deterministic seeds.
//
// Root cause of the stall it guards against: collision was displacing the
// commander — the squad cohesion anchor. When the anchor got shoved backward
// (away from its advancing squad), cohesion dragged the squad back and the two
// armies never closed to contact, so on unlucky seeds a clash dealt ~0 damage
// (forest was the worst). Fix: commanders are immovable obstacles in collision
// (collision.go). This test pins that the march now reliably reaches contact.
func TestClashProducesCombatOnAllMaps(t *testing.T) {
	const hpLostFloor = 0.40 // every clash must deal substantial damage
	for _, mapName := range []string{"plains", "road", "river", "hills", "forest"} {
		for _, seed := range []int64{0, 7, 18, 31, 42} {
			gs := NewGameSession()
			m := tilemap.LoadClashMap(mapName)
			if m == nil {
				continue
			}
			gs.ResetWithMap(m)
			gs.EnableClashMode()
			gs.Map.Objective.Type = 0
			gs.SetSessionRNG(rand.New(rand.NewSource(seed)))

			mw, mh := gs.MapSize()
			cx := fixed.FromFloat(float64(mw) / 2)
			cy1 := fixed.FromFloat(float64(mh/2 - 4))
			cy2 := fixed.FromFloat(float64(mh/2 + 4))
			gs.SpawnSquadWithType(1, 1, cx, cy1, 10, component.UnitLightInfantry)
			gs.SpawnSquadWithType(2, 2, cx, cy2, 10, component.UnitLightInfantry)

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
					path.TargetY = cy1
				} else {
					path.TargetY = cy2
				}
			})

			healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
			startHP := 0
			healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) { startHP += int(h.HP) })
			for tick := 1; tick <= 500; tick++ {
				gs.Tick()
			}
			endHP := 0
			healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) { endHP += int(h.HP) })
			lost := float64(startHP-endHP) / float64(startHP)
			t.Logf("[%-7s seed=%-2d] HP lost %.0f%%", mapName, seed, lost*100)
			if lost < hpLostFloor {
				t.Errorf("map %s seed %d: clash stalled — only %.0f%% HP lost; armies did not engage (commander-anchor march regression)",
					mapName, seed, lost*100)
			}
		}
	}
}
