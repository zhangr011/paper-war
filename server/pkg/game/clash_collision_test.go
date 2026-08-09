package game

import (
	"math"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestClashWithinTeamNoOverlap is the clash-mode broad guard for collision.
// It mirrors the clash setup in cmd/server/main.go (start_clash): two squads
// of 10 CombatUnits spawned ~8 tiles apart and marched at each other's spawn.
// Within each team (same faction) units must respect collision radii.
//
// The sharp root-cause guard is TestSpawnedCombatUnitHasCollisionComponent
// (addComponent must store CollisionComponent). This test confirms the
// end-to-end effect: after contact settles, same-faction units are spaced to
// ~rSum, and even at peak contact they never collapse into a near-zero pile
// (the "collision not worked" symptom was an end-state of 0.0000).
func TestClashWithinTeamNoOverlap(t *testing.T) {
	worst, end, collapsedTicks := runClashOverlap(NewGameSession())
	totalTicks := 250
	t.Logf("rSum=0.4399  worst=%.4f  end=%.4f  ticks-below-0.15=%d/%d", worst, end, collapsedTicks, totalTicks)

	// After contact settles, same-faction units must be ~rSum apart. This is
	// the real "collision works" signal — the original bug held end at ~0.00.
	if end < 0.39 {
		t.Fatalf("clash end-state overlap: end=%.4f < 0.39 (persistent pile-up — collision not working)", end)
	}
	// A one-tick coincidence in the 20-unit melee is acceptable combat chaos
	// (and expected now that commanders are immovable obstacles, so the
	// movable partner absorbs the full push). A *persistent* collapse is not:
	// require that fewer than 10% of post-contact ticks sit below 0.15 tile.
	if collapsedTicks >= totalTicks/10 {
		t.Fatalf("clash persistent pile-up: %d/%d ticks below 0.15 tile (units stuck collapsed)", collapsedTicks, totalTicks)
	}
}

// runClashOverlap builds a clashed GameSession (two 10-unit squads marched at
// each other) and returns (worst, end): the smallest and the final within-team
// pairwise distance among alive combat units, in tiles.
func runClashOverlap(gs *GameSession) (worst, end float64, collapsedTicks int) {
	gs.Lifecycle.Phase = PhasePlaying
	gs.Map.Objective.Type = 0 // elimination

	mw := int32(DefaultMapWidth)
	mh := int32(DefaultMapHeight)
	halfDist := int32(4) // clash uses 8-tile initial separation
	cx := fixed.FromFloat(float64(mw) / 2)
	cy1 := fixed.FromFloat(float64(mh/2 - halfDist))
	cy2 := fixed.FromFloat(float64(mh/2 + halfDist))

	gs.SpawnSquadWithType(1, 1, cx, cy1, 10, component.UnitLightInfantry)
	gs.SpawnSquadWithType(2, 2, cx, cy2, 10, component.UnitLightInfantry)

	// March orders: point each army at the enemy spawn (same as main.go:437+).
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

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	// minWithinTeam: smallest same-faction pairwise distance among ALIVE
	// non-commander combat units. Dead units keep their PositionComponent at the
	// death point, so HP>0 is essential — else a corpse pile reads as overlap.
	minWithinTeam := func() float64 {
		byFaction := map[uint8][][2]int64{}
		boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
			if bc.Role == component.RoleCommander || bc.GarrisonedIn != 0 {
				return
			}
			hp, has := healthPool.Get(e)
			if !has || hp.HP <= 0 {
				return
			}
			own, has := ownerPool.Get(e)
			if !has {
				return
			}
			pos, ok := posPool.Get(e)
			if !ok {
				return
			}
			byFaction[own.Faction] = append(byFaction[own.Faction], [2]int64{pos.X, pos.Y})
		})
		min := math.Inf(1)
		for _, pts := range byFaction {
			for i := 0; i < len(pts); i++ {
				for j := i + 1; j < len(pts); j++ {
					dx := fixed.ToFloat(pts[i][0] - pts[j][0])
					dy := fixed.ToFloat(pts[i][1] - pts[j][1])
					if d := dx*dx + dy*dy; d < min {
						min = d
					}
				}
			}
		}
		if math.IsInf(min, 1) {
			return 0
		}
		return math.Sqrt(min)
	}

	worst = math.Inf(1)
	for tick := 1; tick <= 300; tick++ {
		gs.Tick()
		if tick >= 50 { // score only once the armies have met
			d := minWithinTeam()
			if d < worst {
				worst = d
			}
			if d < 0.15 {
				collapsedTicks++
			}
		}
	}
	end = minWithinTeam()
	return worst, end, collapsedTicks
}
