package movement

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/formation"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func testProfile() *component.MovementProfile {
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	return p
}

func TestMovementSystemMovesTowardTarget(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(10, 10)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(1.0), Y: fixed.FromFloat(1.0)})
	velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(e, component.BoidComponent{
		SquadID: 1, Role: component.RoleMelee,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(1.0),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(e, component.MovementComponent{ProfileID: 0})
	pathPool.Add(e, component.PathfindingComponent{
		TargetX: fixed.FromFloat(8.0), TargetY: fixed.FromFloat(8.0),
	})

	for tick := uint32(1); tick <= 5; tick++ {
		w.Tick(tick)
	}

	pos, _ := posPool.Get(e)
	if pos.X <= fixed.FromFloat(1.0) || pos.Y <= fixed.FromFloat(1.0) {
		t.Errorf("unit didn't move: (%v, %v)", fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y))
	}
}

func TestSquadMovesTogether(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(20, 20)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	roles := []component.BoidRole{
		component.RoleMelee, component.RoleMelee,
		component.RoleRanged, component.RoleRanged,
	}
	offsets := formation.CalcOffsets(component.FormationLine, fixed.FromFloat(1.5), roles)
	squadCenterX := fixed.FromFloat(5.0)
	squadCenterY := fixed.FromFloat(5.0)

	for i := 0; i < 4; i++ {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{
			X: squadCenterX + offsets[i].DX,
			Y: squadCenterY + offsets[i].DY,
		})
		velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
		boidPool.Add(e, component.BoidComponent{
			SquadID:       1,
			Role:          roles[i],
			SeparationW:   fixed.FromFloat(1.5),
			CohesionW:     fixed.FromFloat(1.0),
			AlignmentW:    fixed.FromFloat(1.0),
			FormationW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(3.0),
		})
		movePool.Add(e, component.MovementComponent{ProfileID: 0})
		pathPool.Add(e, component.PathfindingComponent{
			TargetX: fixed.FromFloat(15.0),
			TargetY: fixed.FromFloat(15.0),
		})
	}

	for tick := uint32(1); tick <= 20; tick++ {
		w.Tick(tick)
	}

	for i := 0; i < 4; i++ {
		e := ecs.Entity(uint64(i + 1))
		pos, ok := posPool.Get(e)
		if !ok {
			t.Errorf("unit %d not found", i)
			continue
		}
		if pos.X <= fixed.FromFloat(5.0)+offsets[i].DX {
			t.Errorf("unit %d didn't move: (%v, %v)", i, fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y))
		}
	}

	positions := make([][2]int64, 4)
	for i := 0; i < 4; i++ {
		pos, _ := posPool.Get(ecs.Entity(uint64(i + 1)))
		positions[i] = [2]int64{pos.X, pos.Y}
	}
	maxDist := int64(0)
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			dx := positions[i][0] - positions[j][0]
			dy := positions[i][1] - positions[j][1]
			dist := fixed.ISqrt(fixed.DistSq(dx, dy))
			if dist > maxDist {
				maxDist = dist
			}
		}
	}
	if fixed.ToFloat(maxDist) > 8.0 {
		t.Errorf("squad spread too far: max distance = %v", fixed.ToFloat(maxDist))
	}
}
