package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestLiveHighGroundRangeBonus verifies the ADR-0029 high-ground range bonus
// fires through a real gs.Tick() loop with ElevationFn wired to a real map —
// not the per-coordinate stub the isolated combat test uses
// (combat_test.go:306). This is the Phase 0 gate for the behaviour the Phase 1
// range-ring cue will visualise: if the bonus doesn't fire live, the cue lies.
//
// Setup: a LightInfantry commander (base Range 5) on a peak platform vs a
// target commander 6 tiles away on low ground. Distance 6 > base range 5, so
// the shot connects ONLY via the +2 peak-over-low bonus. The control run
// places the attacker on low ground at the same distance — no bonus, no hit.
func TestLiveHighGroundRangeBonus(t *testing.T) {
	// 16x16 map with a 3x3 peak (Elevation 2) platform at (2..4, 2..4).
	// Authored directly (no DeriveElevation) so the peak is exact.
	m := tilemap.NewGameMap(16, 16)
	for dy := int32(0); dy < 3; dy++ {
		for dx := int32(0); dx < 3; dx++ {
			tl := m.TileAt(2+dx, 2+dy)
			tl.TerrainType = component.TerrainHill
			tl.Elevation = 2
		}
	}

	// run spawns an attacker commander + a target commander 6 tiles apart and
	// returns the target commander's HP after a short live tick window.
	// attackerHigh selects whether the attacker stands on the peak (3,3) or
	// on low ground (15,3); the target is always at (9,3) on low ground, so
	// the pair is 6 tiles apart in both runs.
	run := func(attackerHigh bool) (targetHP int32, found bool) {
		gs := NewGameSession()
		gs.EnableClashMode()
		gs.ResetWithMap(m)
		gs.Lifecycle.Phase = PhasePlaying

		// Integration assertion #1: the real session wired CombatSystem's
		// ElevationFn to the map. A nil/flat ElevationFn here is itself a
		// Phase-0 regression (the bonus would never fire in production).
		cs := gs.World.SystemByName("CombatSystem").(*combat.CombatSystem)
		if cs.ElevationFn == nil {
			t.Fatalf("CombatSystem.ElevationFn not wired after ResetWithMap")
		}
		if got := cs.ElevationFn(3, 3); got != 2 {
			t.Fatalf("ElevationFn(3,3)=%d, want 2 (peak not read from map)", got)
		}
		if got := cs.ElevationFn(9, 3); got != 0 {
			t.Fatalf("ElevationFn(9,3)=%d, want 0 (low ground)", got)
		}

		ax := fixed.FromFloat(15.0) // low ground (control)
		if attackerHigh {
			ax = fixed.FromFloat(3.0) // peak platform
		}
		ay := fixed.FromFloat(3.0)
		// unitCount=0 → commanders only, so the only fire is the controlled
		// pair (no scattered squadmates muddying the range math).
		gs.SpawnSquadWithType(1, 1, ax, ay, 0, component.UnitLightInfantry)
		gs.SpawnSquadWithType(2, 2, fixed.FromFloat(9.0), ay, 0, component.UnitLightInfantry)

		// Short window: enough to fire a few shots (LI cmd cooldown 3), short
		// enough that the 600-HP target survives (~3 shots ≈ 90 dmg).
		for i := 0; i < 10; i++ {
			gs.Tick()
		}

		ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		ownerPool.Each(func(e ecs.Entity, oc *component.OwnerComponent) {
			if oc.Faction != component.FactionEnemy { // player 2 = target
				return
			}
			bc, ok := boidPool.Get(e)
			if !ok || bc.Role != component.RoleCommander {
				return
			}
			if hp, ok := healthPool.Get(e); ok {
				targetHP = hp.HP
				found = true
			}
		})
		return targetHP, found
	}

	const cmdFullHP = int32(100 * 6) // LI commander: base HP 100 × 6 (SpawnSquadWithType)

	// Peak over low: +2 range → effective 7 ≥ distance 6 → target takes damage.
	hp, found := run(true)
	if !found {
		t.Fatal("high-ground run: target commander not found after ticks")
	}
	if hp <= 0 || hp >= cmdFullHP {
		t.Errorf("high-ground target HP=%d, want 0 < HP < %d (bonus should extend range and damage target)", hp, cmdFullHP)
	}

	// Control — both on low ground at distance 6: base range 5 < 6 → no hit.
	hp, found = run(false)
	if !found {
		t.Fatal("control run: target commander not found after ticks")
	}
	if hp != cmdFullHP {
		t.Errorf("control target HP=%d, want %d (no high ground → out of range → undamaged)", hp, cmdFullHP)
	}
}
