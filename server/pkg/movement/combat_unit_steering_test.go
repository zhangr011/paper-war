package movement

// Behavioral spec for combat-unit steering in MovementSystem. With the
// formation system removed, combat-unit movement is driven by the flow field,
// the player beacon, and commander-attraction — units no longer steer toward
// assigned formation slots.

import (
	"math"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// newMovementWorld builds a minimal world with only the pools MovementSystem
// touches, plus a commander registered so the formation-attraction branch is
// reachable when a test wants it.
func newMovementWorld(t *testing.T, w, h int32) (*ecs.World, *ecs.EntityManager,
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.BoidComponent],
) {
	t.Helper()
	em := ecs.NewEntityManager()
	world := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(w, h)
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()

	world.RegisterPool(component.PositionComponent{}, posPool)
	world.RegisterPool(component.VelocityComponent{}, velPool)
	world.RegisterPool(component.BoidComponent{}, boidPool)
	world.RegisterPool(component.MovementComponent{}, movePool)
	world.RegisterPool(component.PathfindingComponent{}, pathPool)
	world.RegisterPool(component.CommanderComponent{}, cmdPool)

	profile := testProfile()
	ms := &MovementSystem{
		Gm:       gm,
		Cache:    cache,
		Sh:       sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	world.AddSystem(ms)
	world.Init()

	return world, em, posPool, boidPool
}

// TestCombatUnitsDoNotSeparate: two combat units placed well within
// NeighborRange, with no flow (path target = own position) and no commander,
// must not drift apart. Before the fix the separation force pushes them apart
// and the distance grows; after the fix it stays constant.
func TestCombatUnitsDoNotSeparate(t *testing.T) {
	world, em, posPool, boidPool := newMovementWorld(t, 10, 10)
	_ = world

	combat := component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleRanged,
		AttractionW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(1.0),
	}

	makeUnit := func(x, y float64) ecs.Entity {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(x), Y: fixed.FromFloat(y)})
		velPool := world.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
		velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
		b := combat
		boidPool.Add(e, b)
		movePool := world.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
		movePool.Add(e, component.MovementComponent{ProfileID: 0})
		// No flow: path target is the unit's own position.
		pathPool := world.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
		pathPool.Add(e, component.PathfindingComponent{TargetX: fixed.FromFloat(x), TargetY: fixed.FromFloat(y)})
		return e
	}

	a := makeUnit(5.0, 5.0)
	b := makeUnit(5.2, 5.0) // 0.2 tile away — well inside NeighborRange

	dist := func() float64 {
		pa, _ := posPool.Get(a)
		pb, _ := posPool.Get(b)
		dx := fixed.ToFloat(pa.X) - fixed.ToFloat(pb.X)
		dy := fixed.ToFloat(pa.Y) - fixed.ToFloat(pb.Y)
		return math.Sqrt(dx*dx + dy*dy)
	}
	before := dist()

	for tick := uint32(1); tick <= 10; tick++ {
		world.Tick(tick)
	}

	after := dist()
	if math.Abs(after-before) > 1e-3 {
		t.Errorf("combat units drifted apart despite no flow: dist %.4f → %.4f (separation force should be gone)",
			before, after)
	}
}

// TestFollowForceDominatesFlowWhileMoving: while a squad is moving, the
// commander-cohesion (follow) force must be the biggest force on a follower.
// The commander stands 3 tiles east; the unit's flow target is 10 tiles west,
// so the two forces pull in opposite directions. If follow dominates the unit
// closes on its commander (the squad moves as a team, anchored on the
// commander who leads via his own flow field); if the flow field dominates
// the unit streams west and the squad strings out.
func TestFollowForceDominatesFlowWhileMoving(t *testing.T) {
	world, em, posPool, boidPool := newMovementWorld(t, 24, 24)
	velPool := world.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	movePool := world.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	pathPool := world.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	cmdPool := world.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	// Commander: squad 1's anchor, 3 tiles east of the unit. No
	// PathfindingComponent → no flow force; Role=Commander → no attraction.
	// It stays put for the whole run.
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(13.0), Y: fixed.FromFloat(5.0)})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})

	// Follower: realistic spawn weights (AttractionW=6.0, speed≈0.2
	// tiles/tick), marching west toward a distant flow target.
	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{X: fixed.FromFloat(10.0), Y: fixed.FromFloat(5.0)})
	velPool.Add(unit, component.VelocityComponent{Speed: 820})
	boidPool.Add(unit, component.BoidComponent{
		SquadID:       1,
		Role:          component.RoleRanged,
		AttractionW:   fixed.FromFloat(6.0),
		NeighborRange: fixed.FromFloat(2.0),
	})
	movePool.Add(unit, component.MovementComponent{ProfileID: 0})
	pathPool.Add(unit, component.PathfindingComponent{TargetX: fixed.FromFloat(0.0), TargetY: fixed.FromFloat(5.0)})

	dist := func() float64 {
		pc, _ := posPool.Get(cmd)
		pu, _ := posPool.Get(unit)
		dx := fixed.ToFloat(pc.X) - fixed.ToFloat(pu.X)
		dy := fixed.ToFloat(pc.Y) - fixed.ToFloat(pu.Y)
		return math.Sqrt(dx*dx + dy*dy)
	}
	before := dist()

	for tick := uint32(1); tick <= 40; tick++ {
		world.Tick(tick)
	}

	after := dist()
	if after >= before-0.25 {
		t.Errorf("follow force should dominate while moving: unit should close on its commander, dist %.3f → %.3f",
			before, after)
	}
}
