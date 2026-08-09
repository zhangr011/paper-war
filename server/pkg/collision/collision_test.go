package collision

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// setupWorld builds a minimal ECS world with the four component pools the
// CollisionSystem touches, registers + inits the system, and returns the
// pools so each test can stage entities. Mirrors the seam the movement tests
// use (they drive MovementSystem.Tick directly with hand-built pools).
func setupWorld(t *testing.T) (
	w *ecs.World,
	cs *CollisionSystem,
	posPool *ecs.ComponentPool[component.PositionComponent],
	boidPool *ecs.ComponentPool[component.BoidComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	colPool *ecs.ComponentPool[component.CollisionComponent],
	em *ecs.EntityManager,
) {
	t.Helper()
	em = ecs.NewEntityManager()
	w = ecs.NewWorld(em)

	posPool = ecs.NewComponentPool[component.PositionComponent]()
	boidPool = ecs.NewComponentPool[component.BoidComponent]()
	ownerPool = ecs.NewComponentPool[component.OwnerComponent]()
	colPool = ecs.NewComponentPool[component.CollisionComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.CollisionComponent{}, colPool)

	cs = &CollisionSystem{Sh: spatial.NewHash(fixed.FromFloat(2.0))}
	w.AddSystem(cs)
	w.Init()
	return
}

// spawnUnit creates a combat unit with the given faction, position, radius,
// and optional freeze/garrison flags. Returns its entity ID.
func spawnUnit(
	em *ecs.EntityManager,
	posPool *ecs.ComponentPool[component.PositionComponent],
	boidPool *ecs.ComponentPool[component.BoidComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	colPool *ecs.ComponentPool[component.CollisionComponent],
	faction uint8, x, y, radius int64, freezeUntil uint32, garrisonedIn uint32,
) ecs.Entity {
	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: x, Y: y})
	boidPool.Add(e, component.BoidComponent{
		FreezeUntilTick: freezeUntil,
		GarrisonedIn:    garrisonedIn,
	})
	ownerPool.Add(e, component.OwnerComponent{Faction: faction})
	colPool.Add(e, component.CollisionComponent{Radius: radius})
	return e
}

// dist returns the fixed-point distance between two position components.
func dist(a, b *component.PositionComponent) int64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return fixed.ISqrt(fixed.DistSq(dx, dy))
}

// TestFriendlyOverlapPushedApart: two same-faction units whose circles overlap
// must end the tick exactly touching (dist == r1+r2 within fixed-point noise).
func TestFriendlyOverlapPushedApart(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r1 := fixed.FromFloat(0.25)
	r2 := fixed.FromFloat(0.22)
	// Place 0.2 tile apart along X — well inside r1+r2 = 0.47.
	a := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.0), fixed.FromFloat(5.0), r1, 0, 0)
	b := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.2), fixed.FromFloat(5.0), r2, 0, 0)

	cs.Tick(w, 1)

	pa, _ := posPool.GetPtr(a)
	pb, _ := posPool.GetPtr(b)
	got := dist(pa, pb)
	want := r1 + r2
	// The push-out magnitude is computed from fixed.ISqrt(distSq), which
	// truncates the distance to a few LSBs below the true value; the resulting
	// unit normal is then very slightly longer than 1, so the resolved pair
	// ends up a hair FURTHER apart than r1+r2 (never closer). Bound the
	// over-separation to rSum/32 (≈3%); under-separation (got < want) would
	// mean the circles still overlap, which is the bug this system fixes.
	if got < want-2 || got > want+(r1+r2)/32 {
		t.Fatalf("dist after push = %d (=%.4f tile), want r1+r2 = %d (=%.4f tile) ±fixed-point",
			got, fixed.ToFloat(got), want, fixed.ToFloat(want))
	}
	// A was to the left of B; push-out must move A further left, B further right.
	if pa.X >= fixed.FromFloat(5.0) {
		t.Fatalf("A should have moved left, got X=%.4f", fixed.ToFloat(pa.X))
	}
	if pb.X <= fixed.FromFloat(5.2) {
		t.Fatalf("B should have moved right, got X=%.4f", fixed.ToFloat(pb.X))
	}
}

// TestEnemyOverlapNotPushed: two units of different factions overlap — no
// collision (combat is ranged, ADR-0030). Distance must be unchanged.
func TestEnemyOverlapNotPushed(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r := fixed.FromFloat(0.25)
	ax := fixed.FromFloat(5.0)
	bx := fixed.FromFloat(5.1)
	a := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, ax, fixed.FromFloat(5.0), r, 0, 0)
	b := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionEnemy, bx, fixed.FromFloat(5.0), r, 0, 0)

	beforeA, _ := posPool.GetPtr(a)
	beforeB, _ := posPool.GetPtr(b)
	want := dist(beforeA, beforeB)

	cs.Tick(w, 1)

	afterA, _ := posPool.GetPtr(a)
	afterB, _ := posPool.GetPtr(b)
	got := dist(afterA, afterB)
	if got != want {
		t.Fatalf("enemy distance changed: before=%d after=%d (enemy units must not collide)", want, got)
	}
}

// TestGarrisonedExcluded: a garrisoned unit overlapping a regular unit is
// neither pushed nor an obstacle — the regular unit passes through unmoved.
func TestGarrisonedExcluded(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r := fixed.FromFloat(0.25)
	garrisonEntity := uint32(999)
	regular := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.0), fixed.FromFloat(5.0), r, 0, 0)
	garrisoned := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.0), fixed.FromFloat(5.0), r, 0, garrisonEntity)

	regPos, _ := posPool.GetPtr(regular)
	garPos, _ := posPool.GetPtr(garrisoned)
	wantRegX, wantRegY := regPos.X, regPos.Y
	wantGarX, wantGarY := garPos.X, garPos.Y

	cs.Tick(w, 1)

	if regPos.X != wantRegX || regPos.Y != wantRegY {
		t.Fatalf("regular unit moved by garrisoned 'obstacle': X=%d Y=%d (expected unmoved)",
			regPos.X, regPos.Y)
	}
	if garPos.X != wantGarX || garPos.Y != wantGarY {
		t.Fatalf("garrisoned unit moved: X=%d Y=%d (expected unmoved)", garPos.X, garPos.Y)
	}
}

// TestFrozenIsImmovableObstacle: a frozen unit overlapping a movable unit
// stays put; the movable unit absorbs the full penetration.
func TestFrozenIsImmovableObstacle(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r := fixed.FromFloat(0.25)
	// Frozen unit on the left, movable on the right, 0.1 tile apart.
	frozenX := fixed.FromFloat(5.0)
	movableX := fixed.FromFloat(5.1)
	frozen := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, frozenX, fixed.FromFloat(5.0), r, 100, 0)
	movable := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, movableX, fixed.FromFloat(5.0), r, 0, 0)

	// frozen has the smaller entity ID so it's processed first; ensure that
	// even so it is never displaced.
	cs.Tick(w, 1)

	fPos, _ := posPool.GetPtr(frozen)
	mPos, _ := posPool.GetPtr(movable)
	if fPos.X != frozenX || fPos.Y != fixed.FromFloat(5.0) {
		t.Fatalf("frozen unit moved: X=%.4f (must be immovable)", fixed.ToFloat(fPos.X))
	}
	// Movable takes the full penetration. Because the unit normal's magnitude
	// is fixed.ISqrt-truncated, the resolved distance is r1+r2 plus a few LSBs
	// (over-separation, never under). Bound to rSum/32 — see friendly test.
	got := dist(fPos, mPos)
	want := r + r
	if got < want-2 || got > want+(r+r)/32 {
		t.Fatalf("movable did not absorb full penetration: dist=%d want=%d ±fixed-point", got, want)
	}
	// And the movable unit should have been pushed further right (+X).
	if mPos.X <= movableX {
		t.Fatalf("movable unit should have moved right, got X=%.4f", fixed.ToFloat(mPos.X))
	}
}

// TestBothFrozenOverlapSeparates: two same-faction units that froze WHILE
// overlapping (the contact/firefight pile-up) must still be pushed apart.
//
// Once a squad makes contact, CombatSystem sets FreezeUntilTick=tick+5 on every
// attacker each shot; for most infantry cooldown (2–5) ≤ freeze (5), so the
// whole firing line is permanently frozen for the fight. Previously collision
// skipped any pair where BOTH were frozen, so an overlapping firing line stayed
// overlapped for the entire firefight — the "collision not worked in contact"
// symptom. The frozen-vs-movable contract (TestFrozenIsImmovableObstacle) is
// unchanged: a lone frozen unit is still an immovable obstacle. Only the
// both-frozen subcase now resolves, splitting the penetration evenly.
func TestBothFrozenOverlapSeparates(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r := fixed.FromFloat(0.25)
	// 0.1 tile apart along X — well inside r1+r2 = 0.5. Both frozen for the
	// whole fight (freezeUntil=100, tick=1).
	a := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.0), fixed.FromFloat(5.0), r, 100, 0)
	b := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, fixed.FromFloat(5.1), fixed.FromFloat(5.0), r, 100, 0)

	cs.Tick(w, 1)

	pa, _ := posPool.GetPtr(a)
	pb, _ := posPool.GetPtr(b)
	got := dist(pa, pb)
	want := r + r
	// Same fixed-point over-separation bound as the friendly test (rSum/32).
	// The previous code skipped this pair entirely, leaving got ≈ 0.1 tile.
	if got < want-2 {
		t.Fatalf("both-frozen overlap not resolved: dist=%d (=%.4f tile), want >= rSum=%d (=%.4f tile)",
			got, fixed.ToFloat(got), want, fixed.ToFloat(want))
	}
	// And they must have split apart symmetrically: A left, B right.
	if pa.X >= fixed.FromFloat(5.0) {
		t.Fatalf("frozen A should have moved left, got X=%.4f", fixed.ToFloat(pa.X))
	}
	if pb.X <= fixed.FromFloat(5.1) {
		t.Fatalf("frozen B should have moved right, got X=%.4f", fixed.ToFloat(pb.X))
	}
}

// TestJustTouchingNoJitter: two units already exactly touching (dist == r1+r2)
// must not move — the >= in the narrow phase skips the equality case, so the
// cluster doesn't jitter at rest.
func TestJustTouchingNoJitter(t *testing.T) {
	w, cs, posPool, boidPool, ownerPool, colPool, em := setupWorld(t)

	r := fixed.FromFloat(0.25)
	// Place exactly r1+r2 = 0.5 tile apart along X.
	ax := fixed.FromFloat(5.0)
	bx := fixed.FromFloat(5.5)
	a := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, ax, fixed.FromFloat(5.0), r, 0, 0)
	b := spawnUnit(em, posPool, boidPool, ownerPool, colPool,
		component.FactionPlayer, bx, fixed.FromFloat(5.0), r, 0, 0)

	cs.Tick(w, 1)

	pa, _ := posPool.GetPtr(a)
	pb, _ := posPool.GetPtr(b)
	if pa.X != ax || pa.Y != fixed.FromFloat(5.0) {
		t.Fatalf("just-touching unit A moved: X=%.4f (jitter at rest)", fixed.ToFloat(pa.X))
	}
	if pb.X != bx || pb.Y != fixed.FromFloat(5.0) {
		t.Fatalf("just-touching unit B moved: X=%.4f (jitter at rest)", fixed.ToFloat(pb.X))
	}
}
