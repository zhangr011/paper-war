package movement

import (
	"testing"

	"github.com/user/paper-war/server/pkg/collision"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestCollisionSeparatesStackedSquadInIntegration is the feedback loop for
// ADR-0030's "collision not worked" report. The isolated CollisionSystem unit
// test passes, so the wiring + math are correct in isolation. This test drives
// the REAL symptom: every non-commander unit attracts to the commander's exact
// position (movement.go cohesion), so a fresh squad spawns stacked at one point.
// After N ticks of movement(60)+collision(65) together, the squad must spread
// to ~rSum spacing. It sweeps CollisionSystem.Iterations to show that a single
// pass plateaus BELOW rSum (units still overlap) while more passes converge.
func TestCollisionSeparatesStackedSquadInIntegration(t *testing.T) {
	// Regression for the "collision not worked" report. Production uses
	// AttractionW=6.0 and CollisionSystem.Iterations=8 (session.go). Under
	// those conditions a freshly stacked squad (6 units on the commander
	// point) must converge to near-non-overlap (>= 0.40 tile; rSum=0.44).
	const prodWeight, prodIters = 6.0, 8
	got := runStackTrial(t, prodIters, prodWeight)
	t.Logf("production (AttractionW=%.0f, Iterations=%d): min pairwise = %.4f tiles (rSum=0.44)",
		prodWeight, prodIters, got)
	if got < 0.40 {
		t.Fatalf("collision did not separate the stacked squad under production "+
			"settings: min pairwise=%.4f tiles, want >=0.40 (rSum=0.44; still overlapping)",
			got)
	}

	// Guard against regressing back to the under-resolving 1-pass behavior:
	// at the same production weight, 1 iteration must stay clearly below
	// rSum. If a future change makes 1 pass "good enough", this assertion
	// can be tightened — but it must not silently start passing on a
	// regression of the iteration count.
	if one := runStackTrial(t, 1, prodWeight); one >= 0.40 {
		t.Fatalf("1-iteration baseline unexpectedly reached non-overlap (%.4f); "+
			"the iteration-count fix may no longer be load-bearing — revisit", one)
	}
}

// runStackTrial builds movement+collision sharing one spatial hash, spawns a
// commander + 6 combat units stacked exactly on it, ticks 120×, and returns
// the minimum pairwise distance among the 6 units (in tiles).
func runStackTrial(t *testing.T, iters int, attrW float64) float64 {
	t.Helper()
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(64, 64)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	colPool := ecs.NewComponentPool[component.CollisionComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.CollisionComponent{}, colPool)

	ms := &MovementSystem{
		Gm: gm, Cache: cache, Sh: sh,
		Profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	cs := &collision.CollisionSystem{Sh: sh, Iterations: iters}
	w.AddSystem(ms)
	w.AddSystem(cs)
	w.Init()

	// Commander at (10,10) — the single attraction target every unit pulls to.
	cmdX, cmdY := fixed.FromFloat(10.0), fixed.FromFloat(10.0)
	cmdEntity := em.Create()
	posPool.Add(cmdEntity, component.PositionComponent{X: cmdX, Y: cmdY})
	velPool.Add(cmdEntity, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(cmdEntity, component.BoidComponent{
		SquadID: 1, Role: component.RoleCommander,
		AttractionW: fixed.FromFloat(attrW), NeighborRange: fixed.FromFloat(2.0),
	})
	movePool.Add(cmdEntity, component.MovementComponent{ProfileID: 0})
	pathPool.Add(cmdEntity, component.PathfindingComponent{TargetX: cmdX, TargetY: cmdY})
	cmdPool.Add(cmdEntity, component.CommanderComponent{SquadID: 1, IsAlive: true, AuraRadius: fixed.FromFloat(3.0)})
	ownerPool.Add(cmdEntity, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
	colPool.Add(cmdEntity, component.CollisionComponent{Radius: fixed.FromFloat(0.25)})

	// 6 combat units ALL stacked exactly on the commander point.
	r := fixed.FromFloat(0.22)
	var units []ecs.Entity
	for i := 0; i < 6; i++ {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{X: cmdX, Y: cmdY})
		velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
		boidPool.Add(e, component.BoidComponent{
			SquadID: 1, Role: component.RoleMelee,
			AttractionW: fixed.FromFloat(attrW), NeighborRange: fixed.FromFloat(2.0),
		})
		movePool.Add(e, component.MovementComponent{ProfileID: 0})
		// Target = own spawn so the flow-field contributes ~nothing; only
		// commander-cohesion attraction acts (the documented stacking cause).
		pathPool.Add(e, component.PathfindingComponent{TargetX: cmdX, TargetY: cmdY})
		ownerPool.Add(e, component.OwnerComponent{PlayerID: 1, Faction: component.FactionPlayer})
		colPool.Add(e, component.CollisionComponent{Radius: r})
		units = append(units, e)
	}

	for tick := uint32(1); tick <= 120; tick++ {
		w.Tick(tick)
	}

	min := 1e9
	for i := 0; i < len(units); i++ {
		pi, _ := posPool.Get(units[i])
		for j := i + 1; j < len(units); j++ {
			pj, _ := posPool.Get(units[j])
			dx := fixed.ToFloat(pi.X - pj.X)
			dy := fixed.ToFloat(pi.Y - pj.Y)
			d := dx*dx + dy*dy
			if d < min {
				min = d
			}
		}
	}
	if min == 1e9 {
		return 0
	}
	x := min
	for i := 0; i < 8; i++ {
		x = 0.5 * (x + min/x)
	}
	return x
}
