package movement

// Behavioral spec for the "follow commander" cohesion force: a combat unit
// steers toward its commander's position (NOT commander + formation slot —
// slots are gone). No separation. This is the only force, beyond flow/beacon,
// that keeps a combat unit tethered to its squad.
//
// Goes red on the current code (no attraction to commander) and green once a
// commander-position attraction is re-added to the combat-unit force sum.

import (
	"math"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestCombatUnitMovesTowardCommander: a combat unit placed 2 tiles east of its
// commander, with no flow (path target = own position) and no beacon, must
// move west — toward the commander. Before the fix it sits still (nothing
// pulls it to the commander); after the fix it closes the gap.
func TestCombatUnitMovesTowardCommander(t *testing.T) {
	world, em, posPool, boidPool := newMovementWorld(t, 10, 10)
	cmdPool := world.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	velPool := world.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	movePool := world.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	pathPool := world.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

	// Commander at (5,5), stationary.
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	velPool.Add(cmd, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 1, Role: component.RoleCommander, NeighborRange: fixed.FromFloat(1.0)})
	movePool.Add(cmd, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmd, component.PathfindingComponent{TargetX: fixed.FromFloat(5.0), TargetY: fixed.FromFloat(5.0)})
	cmdPool.Add(cmd, component.CommanderComponent{SquadID: 1, IsAlive: true})

	// Combat unit 2 tiles east of the commander, no flow (target = own pos).
	unit := em.Create()
	posPool.Add(unit, component.PositionComponent{X: fixed.FromFloat(7.0), Y: fixed.FromFloat(5.0)})
	velPool.Add(unit, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(unit, component.BoidComponent{
		SquadID: 1, Role: component.RoleRanged,
		AttractionW: fixed.FromFloat(6.0),
	})
	movePool.Add(unit, component.MovementComponent{ProfileID: 0})
	// No PathfindingComponent → flow force is zeroed (movement.go skips the
	// flow block when no path exists), isolating the cohesion force. Same
	// pattern as the equilibrium harness. A path target sitting ON the unit's
	// own tile returns a non-zero flow direction that would mask cohesion.
	_ = pathPool

	before, _ := posPool.Get(unit)
	for tick := uint32(1); tick <= 10; tick++ {
		world.Tick(tick)
	}
	after, _ := posPool.Get(unit)

	dx := fixed.ToFloat(after.X) - fixed.ToFloat(before.X)
	if dx >= 0 {
		t.Errorf("combat unit did not move toward commander: dx=%.4f (expected negative — west, toward commander at x=5)",
			dx)
	}
	// And it should have closed the gap meaningfully, not crawled.
	gapAfter := math.Abs(fixed.ToFloat(after.X) - 5.0)
	if gapAfter > 1.9 {
		t.Errorf("combat unit barely closed the gap: gap %.4f → %.4f over 10 ticks",
			2.0, gapAfter)
	}
}
