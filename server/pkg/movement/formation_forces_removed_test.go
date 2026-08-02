package movement

// Behavioral spec for the removal of the formation attraction and spread
// (separation) forces from MovementSystem. Combat-unit movement is now driven
// by the flow field (and the player beacon) only — units no longer repel each
// other and no longer steer toward a formation slot.
//
// These tests go red on the current code (the forces still fire) and green
// once the sepFX*SepW and formation-slot attrFX*FormW terms are dropped from
// the combat-unit force sum in movement.go.

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
	formationRolePool := ecs.NewComponentPool[component.FormationRoleComponent]()

	world.RegisterPool(component.PositionComponent{}, posPool)
	world.RegisterPool(component.VelocityComponent{}, velPool)
	world.RegisterPool(component.BoidComponent{}, boidPool)
	world.RegisterPool(component.MovementComponent{}, movePool)
	world.RegisterPool(component.PathfindingComponent{}, pathPool)
	world.RegisterPool(component.CommanderComponent{}, cmdPool)
	world.RegisterPool(component.FormationRoleComponent{}, formationRolePool)

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
		SeparationW:   fixed.FromFloat(1.5), // large so the red failure is unambiguous
		FormationW:    fixed.FromFloat(2.0),
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

// TestCombatUnitDoesNotSteerToFormationSlot: a combat unit sitting on the
// commander with a nonzero FormationRoleComponent offset, no flow, must not
// steer toward its (commander+offset) slot. Before the fix the formation
// attraction pulls it toward the slot; after the fix it stays put.
func TestCombatUnitDoesNotSteerToFormationSlot(t *testing.T) {
	world, em, posPool, boidPool := newMovementWorld(t, 10, 10)
	cmdPool := world.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	velPool := world.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	movePool := world.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	pathPool := world.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	formationRolePool := world.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])

	// Commander at (5,5), stationary (path target = own position).
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	velPool.Add(cmd, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander, NeighborRange: fixed.FromFloat(1.0)})
	movePool.Add(cmd, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmd, component.PathfindingComponent{TargetX: fixed.FromFloat(5.0), TargetY: fixed.FromFloat(5.0)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})

	// Combat unit on the commander, with a slot offset 3 tiles east.
	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	velPool.Add(unit, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(unit, component.BoidComponent{
		SquadID: 1, Role: component.RoleRanged,
		FormationW: fixed.FromFloat(6.0), // large so the red failure is unambiguous
	})
	movePool.Add(unit, component.MovementComponent{ProfileID: 0})
	// No flow: path target = own position.
	pathPool.Add(unit, component.PathfindingComponent{TargetX: fixed.FromFloat(5.0), TargetY: fixed.FromFloat(5.0)})
	formationRolePool.Add(unit, component.FormationRoleComponent{OffsetX: fixed.FromFloat(3.0)})

	before, _ := posPool.Get(unit)
	for tick := uint32(1); tick <= 10; tick++ {
		world.Tick(tick)
	}
	after, _ := posPool.Get(unit)

	dx := fixed.ToFloat(after.X) - fixed.ToFloat(before.X)
	dy := fixed.ToFloat(after.Y) - fixed.ToFloat(before.Y)
	if math.Abs(dx) > 1e-3 || math.Abs(dy) > 1e-3 {
		t.Errorf("combat unit steered toward its formation slot despite no flow: moved (%.4f, %.4f) (formation force should be gone)",
			dx, dy)
	}
}
