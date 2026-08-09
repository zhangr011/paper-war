package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// spawnToleranceUnit creates a combat unit wired for Range Tolerance tests.
// squad/faction identify allegiance; rng is the unit's base attack Range in
// tiles; garrisonedIn is a nonzero Stronghold entity ID when the unit is
// garrisoned (0 = on the field).
func spawnToleranceUnit(em *ecs.EntityManager,
	posPool *ecs.ComponentPool[component.PositionComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	attackPool *ecs.ComponentPool[component.AttackComponent],
	boidPool *ecs.ComponentPool[component.BoidComponent],
	utPool *ecs.ComponentPool[component.UnitTypeComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	x, y float64, squad uint32, faction uint8, rng float64, garrisonedIn uint32,
) ecs.Entity {
	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(x), Y: fixed.FromFloat(y)})
	healthPool.Add(e, component.HealthComponent{HP: 100, MaxHP: 100})
	attackPool.Add(e, component.AttackComponent{Range: fixed.FromFloat(rng), Damage: 10, Cooldown: 1})
	boidPool.Add(e, component.BoidComponent{SquadID: squad, GarrisonedIn: garrisonedIn})
	utPool.Add(e, component.UnitTypeComponent{
		Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight,
	})
	ownerPool.Add(e, component.OwnerComponent{Faction: faction})
	return e
}

// TestRangeToleranceFires verifies the core ADR-0031 behavior: a buddy B just
// outside its base Range of an enemy E may fire when a same-Squad spotter A is
// within SpotterRadius and already engaging E. The opening fire-stagger gates
// the unlock on the spotter's tenure, so B fires 1-2 ticks AFTER A's first shot
// (per B's entity-derived threshold), not on the first tolerance-eligible tick.
//
// Entities are created B(idx=1, odd → threshold 2), A(idx=2), E(idx=3). A's
// first shot lands at tick 1 (T=1); A's tenure reaches 1 at tick 2 and 2 at
// tick 3. B's threshold is 2, so B first fires via tolerance at T+2 = tick 3.
func TestRangeToleranceFires(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	// B (short-range buddy) at origin, Range 3. Created first → entity idx 1
	// (odd) → stagger threshold 2: B fires 2 ticks after A's first shot.
	B := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// A (spotter) within SpotterRadius (2 tiles) of B, Range 5 — in range of E.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		1.5, 0, 1, component.FactionPlayer, 5.0, 0)
	// E (enemy) just outside B's base Range (3) but inside Range+Tolerance (4).
	// Also inside A's Range (5) so A engages and becomes a spotter next tick.
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(1) // A acquires E and fires (first shot, T=1); B chase-acquires but cannot fire.
	w.Tick(2) // A is now a spotter (tenure=1) — below B's threshold 2, so B still holds fire.

	// Stagger window: at tick 2 B must NOT have fired yet (tenure 1 < threshold 2).
	acB, _ := attackPool.Get(B)
	if acB.LastAttack != 0 {
		t.Errorf("tick 2: B.LastAttack = %d, want 0 (spotter tenure 1 < B threshold 2 — stagger holds)", acB.LastAttack)
	}

	w.Tick(3) // A tenure reaches 2 ≥ B threshold → B unlocks tolerance and fires.

	acB, _ = attackPool.Get(B)
	if acB.LastAttack != 3 {
		t.Errorf("B.LastAttack = %d, want 3 (B should fire at tick 3 = T+2 via tolerance)", acB.LastAttack)
	}
	// A fired ticks 1,2,3 (10 each = 30); B fired tick 3 (10) → E took 40 total.
	hpE, _ := healthPool.Get(E)
	if hpE.HP != 60 {
		t.Errorf("E HP = %d, want 60 (100 - A:10×3 - B:10)", hpE.HP)
	}
}

// TestRangeToleranceBeyondSpotterRadius verifies the proximity gate: same
// setup as Fires but the spotter A is farther than SpotterRadius from B, so B
// gets no tolerance and does not fire (E remains out of B's base Range).
func TestRangeToleranceBeyondSpotterRadius(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	B := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// A is a valid spotter (engages E within base Range) but 2.5 tiles from B —
	// just past SpotterRadius (2.0). Tolerance must NOT unlock.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		2.5, 0, 1, component.FactionPlayer, 5.0, 0)
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)
	w.Tick(2)

	acB, _ := attackPool.Get(B)
	if acB.LastAttack != 0 {
		t.Errorf("B.LastAttack = %d, want 0 (spotter out of SpotterRadius → no tolerance)", acB.LastAttack)
	}
	// Only A fires (10/tick × 2 = 20); B never fires.
	hpE, _ := healthPool.Get(E)
	if hpE.HP != 80 {
		t.Errorf("E HP = %d, want 80 (only A's damage; B must not fire)", hpE.HP)
	}
}

// TestRangeToleranceSquadGated verifies tolerance is same-Squad only: a spotter
// of a DIFFERENT Squad within SpotterRadius of B grants no overshoot.
func TestRangeToleranceSquadGated(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	B := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// A is same faction but a DIFFERENT squad (squad 3) — an allied squad, not B's.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		1.5, 0, 3, component.FactionPlayer, 5.0, 0)
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)
	w.Tick(2)

	acB, _ := attackPool.Get(B)
	if acB.LastAttack != 0 {
		t.Errorf("B.LastAttack = %d, want 0 (different-Squad spotter grants no tolerance)", acB.LastAttack)
	}
}

// TestRangeToleranceGarrisonedSpotterGrantsNothing verifies garrisoned units
// are excluded from the spotter system entirely: A is engaging E from inside a
// Stronghold but is NOT a spotter, so nearby B gets no tolerance.
func TestRangeToleranceGarrisonedSpotterGrantsNothing(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	B := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// A is same Squad and within SpotterRadius, engaging E, but garrisoned
	// (GarrisonedIn = 999) → buildSpotterSet skips it. B must not get tolerance.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		1.5, 0, 1, component.FactionPlayer, 5.0, 999)
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)
	w.Tick(2)

	acB, _ := attackPool.Get(B)
	if acB.LastAttack != 0 {
		t.Errorf("B.LastAttack = %d, want 0 (garrisoned spotter grants no tolerance)", acB.LastAttack)
	}
}

// TestRangeToleranceLoneUnit verifies a unit with no same-Squad spotter anywhere
// acquires at its base Range only — no overshoot — so an enemy just outside
// base Range is never fired upon.
func TestRangeToleranceLoneUnit(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	// B is the only friendly unit on the field — no possible spotter.
	B := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)
	w.Tick(2)

	acB, _ := attackPool.Get(B)
	if acB.LastAttack != 0 {
		t.Errorf("B.LastAttack = %d, want 0 (lone unit never overshoots base Range)", acB.LastAttack)
	}
	hpE, _ := healthPool.Get(E)
	if hpE.HP != 100 {
		t.Errorf("E HP = %d, want 100 (no one should damage E)", hpE.HP)
	}
}

// TestRangeToleranceStaggerTiming verifies the opening fire-stagger (ADR-0031):
// a spotter S first fires at tick T; a follower with threshold 1 (even entity)
// first fires via tolerance at T+1; a follower with threshold 2 (odd entity)
// first fires at T+2. The two followers fire on DIFFERENT ticks — the ripple.
//
// Entity creation order fixes the thresholds deterministically: S=idx1, F1=idx2
// (even → threshold 1), F2=idx3 (odd → threshold 2). S first fires at tick 1.
func TestRangeToleranceStaggerTiming(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	// S (spotter) at origin, Range 5 — engages E and becomes the spotter.
	S := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 5.0, 0)
	// F1 (follower, idx 2 → even → threshold 1) within SpotterRadius of S.
	// Range 3, so E (3.5 tiles away) is outside base Range but inside Range+Tolerance.
	F1 := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0.5, 1, component.FactionPlayer, 3.0, 0)
	// F2 (follower, idx 3 → odd → threshold 2) — fires one tick later than F1.
	F2 := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, -0.5, 1, component.FactionPlayer, 3.0, 0)
	// E enemy at (3.5, 0). Distances: S→E=3.5 (≤5 ✓), F1→E≈3.54 (>3, ≤4 ✓), F2→E≈3.54 (✓).
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	// Sanity: confirm the per-follower thresholds derived from entity IDs.
	if th := spotterThreshold(uint32(F1)); th != 1 {
		t.Fatalf("F1 threshold = %d, want 1 (entity %d is even)", th, uint32(F1))
	}
	if th := spotterThreshold(uint32(F2)); th != 2 {
		t.Fatalf("F2 threshold = %d, want 2 (entity %d is odd)", th, uint32(F2))
	}

	rebuildSpatialHash(sh, posPool)
	w.Tick(1) // S acquires E and fires (first shot, T=1).

	acS, _ := attackPool.Get(S)
	if acS.LastAttack != 1 {
		t.Fatalf("S.LastAttack = %d, want 1 (spotter's first shot T=1)", acS.LastAttack)
	}

	w.Tick(2) // S tenure=1. F1 threshold 1 → unlocks; F2 threshold 2 → still held.
	acF1, _ := attackPool.Get(F1)
	if acF1.LastAttack != 2 {
		t.Errorf("F1.LastAttack = %d, want 2 (threshold 1 → first fire at T+1)", acF1.LastAttack)
	}
	acF2, _ := attackPool.Get(F2)
	if acF2.LastAttack != 0 {
		t.Errorf("F2.LastAttack = %d, want 0 at tick 2 (threshold 2 → must wait until T+2)", acF2.LastAttack)
	}

	w.Tick(3) // S tenure=2. F2 threshold 2 → unlocks now.
	acF2, _ = attackPool.Get(F2)
	if acF2.LastAttack != 3 {
		t.Errorf("F2.LastAttack = %d, want 3 (threshold 2 → first fire at T+2)", acF2.LastAttack)
	}
	// F1 fired at tick 2; F2 fired at tick 3 — they fired on DIFFERENT ticks (the ripple).
	if acF1.LastAttack == acF2.LastAttack {
		t.Errorf("F1 and F2 both first fired at tick %d — stagger failed (want different ticks)", acF1.LastAttack)
	}
	// Sanity: E took damage. S fired t1,t2,t3 (30); F1 fired t2,t3 (20); F2 fired t3 (10) = 60.
	hpE, _ := healthPool.Get(E)
	if hpE.HP != 40 {
		t.Errorf("E HP = %d, want 40 (100 - S:30 - F1:20 - F2:10)", hpE.HP)
	}
}

// TestRangeToleranceNoPrematureFire verifies that before the nearby spotter's
// tenure reaches the follower's threshold, the follower does NOT fire via
// tolerance even though the target sits within Range+Tolerance. It must be
// pursuing (pathfinding destination set to the enemy), not firing.
func TestRangeToleranceNoPrematureFire(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, pathPool := setupChaseWorld()

	// F (follower) created first → idx 1 (odd) → threshold 2. Range 3.
	F := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// S (spotter, idx 2) within SpotterRadius of F, Range 5 — engages E.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0.5, 0, 1, component.FactionPlayer, 5.0, 0)
	// E at (3.5, 0): F→E=3.5 (>3 base, ≤4 tolerance), S→E=3.0 (≤5 base).
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)
	// F needs a PathfindingComponent so we can assert it is pursuing, not firing.
	pathPool.Add(F, component.PathfindingComponent{})

	if th := spotterThreshold(uint32(F)); th != 2 {
		t.Fatalf("F threshold = %d, want 2", th)
	}

	rebuildSpatialHash(sh, posPool)
	w.Tick(1) // S acquires E and fires (becomes spotter next tick); F chase-acquires E.
	w.Tick(2) // S tenure=1 < F threshold 2 → tolerance NOT unlocked for F.

	acF, _ := attackPool.Get(F)
	if acF.LastAttack != 0 {
		t.Errorf("tick 2: F.LastAttack = %d, want 0 (tenure 1 < threshold 2 → no tolerance fire)", acF.LastAttack)
	}
	// F should be pursuing the out-of-base-range enemy, not idle.
	path, ok := pathPool.Get(F)
	if !ok {
		t.Fatal("F has no PathfindingComponent")
	}
	if path.TargetX == 0 && path.TargetY == 0 {
		t.Error("F pathfinding target not set — expected pursuit of E while tolerance is stagger-locked")
	}
	ePos, _ := posPool.Get(E)
	if path.TargetX != ePos.X {
		t.Errorf("F pathfinding TargetX = %d, want %d (enemy position)", path.TargetX, ePos.X)
	}

	// Sanity: at tick 3 tenure reaches 2 ≥ threshold → F finally fires.
	w.Tick(3)
	acF, _ = attackPool.Get(F)
	if acF.LastAttack != 3 {
		t.Errorf("tick 3: F.LastAttack = %d, want 3 (tenure rebuilt to 2 → tolerance fires)", acF.LastAttack)
	}
}

// TestRangeToleranceReCascade verifies the stagger re-applies on fresh contact:
// because SpotterTenure resets to 0 when the spotter stops engaging, a follower
// that already fired via tolerance must wait for tenure to rebuild after the
// spotter loses and regains its target. No separate "opening" tracking — the
// tenure reset handles it (ADR-0031).
func TestRangeToleranceReCascade(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, _ := setupChaseWorld()

	// F (follower) idx 1 (odd) → threshold 2. Range 3 at origin.
	F := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0, 0, 1, component.FactionPlayer, 3.0, 0)
	// S (spotter) idx 2 within SpotterRadius of F, Range 5.
	_ = spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		0.5, 0, 1, component.FactionPlayer, 5.0, 0)
	// E enemy: F→E=3.5 (>3, ≤4), S→E=3.0 (≤5).
	E := spawnToleranceUnit(em, posPool, healthPool, attackPool, boidPool, utPool, ownerPool,
		3.5, 0, 2, component.FactionEnemy, 5.0, 0)

	if th := spotterThreshold(uint32(F)); th != 2 {
		t.Fatalf("F threshold = %d, want 2", th)
	}

	rebuildSpatialHash(sh, posPool)
	w.Tick(1) // S first shot (T=1).
	w.Tick(2) // S tenure=1 < 2 → F holds.
	w.Tick(3) // S tenure=2 ≥ 2 → F fires via tolerance for the first time.
	acF, _ := attackPool.Get(F)
	if acF.LastAttack != 3 {
		t.Fatalf("phase 1: F.LastAttack = %d, want 3 (first cascade)", acF.LastAttack)
	}

	// Phase 2 — spotter loses its target: march E far out of everyone's range.
	// S keeps TargetID=E (isTargetValid ignores range) but the base-range
	// re-check in buildSpotterSet fails → S is no longer a spotter → tenure resets.
	farX := fixed.FromFloat(50.0)
	epos, _ := posPool.GetPtr(E)
	epos.X = farX
	rebuildSpatialHash(sh, posPool)
	w.Tick(4) // S tenure resets to 0; F has no nearby spotter → no tolerance fire.

	acF, _ = attackPool.Get(F)
	if acF.LastAttack != 3 {
		t.Errorf("phase 2: F.LastAttack = %d, want 3 (no spotter — must not fire again)", acF.LastAttack)
	}
	acS, _ := attackPool.Get(ecs.Entity(2))
	if acS.SpotterTenure != 0 {
		t.Errorf("phase 2: S.SpotterTenure = %d, want 0 (E out of base Range → spotter dropped)", acS.SpotterTenure)
	}

	// Phase 3 — E returns to contact. The cascade must re-run: F cannot fire
	// via tolerance until S's tenure rebuilds to F's threshold (2).
	epos, _ = posPool.GetPtr(E)
	epos.X = fixed.FromFloat(3.5)
	rebuildSpatialHash(sh, posPool)

	w.Tick(5) // S re-qualifies → tenure=1 < F threshold 2 → F must NOT fire yet.
	acF, _ = attackPool.Get(F)
	if acF.LastAttack != 3 {
		t.Errorf("tick 5 (re-contact): F.LastAttack = %d, want 3 (re-cascade: tenure 1 < threshold 2)", acF.LastAttack)
	}

	w.Tick(6) // S tenure=2 ≥ threshold → F re-fires via tolerance.
	acF, _ = attackPool.Get(F)
	if acF.LastAttack != 6 {
		t.Errorf("tick 6: F.LastAttack = %d, want 6 (re-cascade: tenure rebuilt → F fires again)", acF.LastAttack)
	}
}
