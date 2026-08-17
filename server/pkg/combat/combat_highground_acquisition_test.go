package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestFindTargetHighGroundAcquisitionGate — the extended acquisition band
// (range_ > baseRange, granted to attackers on raised ground) may only
// acquire candidates the attacker outranks in elevation, mirroring the fire
// check's attacker>target rule (ADR-0029).
//
// Regression guard for "the team on the hill lost the clash" (2026-08): the
// old blanket extension let any attacker on raised ground acquire EQUAL- or
// HIGHER-elevation targets a tile beyond base range. On the hills clash map
// that made the hill army lock onto targets it could never hit, planting it
// at the edge of the engagement band while the valley army walked into
// contact — the hill side was out-shot ~2:1 and its elevation bonuses
// actively cost it the engagement.
//
// The gate is exercised via direct findTarget calls because the Tick flow's
// chase pass (2× range) legitimately re-acquires the same targets for
// pursuit — the observable symptom only emerges in the multi-unit standoff,
// covered by game.TestHillClashFireParity.
func TestFindTargetHighGroundAcquisitionGate(t *testing.T) {
	// Attacker: range 3 at (0,0) on Elevation 1. Candidates at distance 3.5 —
	// beyond base range (3), inside the +1 extended band (4).
	const base = 3.0
	const ext = 4.0
	const dist = 3.5

	run := func(targetElev uint8) uint32 {
		em, w, sh, posPool, healthPool, _, boidPool, utPool := setupCombatWorld()
		cs := w.SystemByName("CombatSystem").(*CombatSystem)
		cs.ElevationFn = func(x, y int32) uint8 {
			if x == 0 && y == 0 {
				return 1 // attacker stands on raised ground
			}
			return targetElev
		}

		attacker := em.Create()
		posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
		boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
		utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

		target := em.Create()
		posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(dist), Y: 0})
		healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
		boidPool.Add(target, component.BoidComponent{SquadID: 2})
		utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

		rebuildSpatialHash(sh, posPool)

		tp, _ := posPool.Get(attacker)
		return cs.findTarget(attacker, tp, fixed.FromFloat(ext), fixed.FromFloat(base), component.WeaponGun, 0)
	}

	// Equal elevation: target sits in the extended band but is NOT outranked →
	// must not be acquired.
	if id := run(1); id != 0 {
		t.Errorf("equal-elevation target at %.1f tiles acquired (id=%d), want 0 — extended band requires height advantage", dist, id)
	}
	// Higher target (shooting uphill): also not acquired.
	if id := run(2); id != 0 {
		t.Errorf("higher-elevation target at %.1f tiles acquired (id=%d), want 0 — no acquisition bonus shooting uphill", dist, id)
	}
	// Lower target: the designed case — acquired through the extension.
	if id := run(0); id == 0 {
		t.Errorf("lower-elevation target at %.1f tiles not acquired, want id — height advantage should extend acquisition", dist)
	}
}

// TestCombatRampNoRangeBonus — a Ramp is a transition strip, not a fighting
// platform: a climber standing on a Ramp tile never gains the high-ground
// +1, even when the Ramp's authored elevation tops the target's. Without
// this, authored chokepoint maps (ramp-carried cliffs) hand the advantage to
// the assault mid-crossing. ADR-0034.
func TestCombatRampNoRangeBonus(t *testing.T) {
	// Attacker: range 5 on a Ramp authored at Elevation 2; target 5.5 tiles
	// away on low ground — inside the would-be +1 band (6), outside base (5).
	const dist = 5.5

	run := func(attackerOnRamp bool) int32 {
		em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()
		cs := w.SystemByName("CombatSystem").(*CombatSystem)
		cs.ElevationFn = func(x, y int32) uint8 {
			if x == 0 && y == 0 {
				return 2 // ramp authored at the top band
			}
			return 0
		}
		cs.TerrainFn = func(x, y int32) component.TerrainType {
			if x == 0 && y == 0 && attackerOnRamp {
				return component.TerrainRamp
			}
			return component.TerrainHill
		}

		attacker := em.Create()
		posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
		attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(5.0), Damage: 10, Cooldown: 1})
		boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
		utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

		target := em.Create()
		posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(dist), Y: 0})
		healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
		boidPool.Add(target, component.BoidComponent{SquadID: 2})
		utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

		rebuildSpatialHash(sh, posPool)
		w.Tick(1)
		hp, _ := healthPool.Get(target)
		return hp.HP
	}

	// On Hill terrain: height advantage extends range 5→6 ≥ 5.5 → damage.
	if hp := run(false); hp == 100 {
		t.Errorf("hill attacker HP=%d, want <100 (height advantage should extend range)", hp)
	}
	// On Ramp: no bonus — 5.5 > 5 → no damage.
	if hp := run(true); hp != 100 {
		t.Errorf("ramp attacker HP=%d, want 100 (no high-ground bonus from a Ramp tile)", hp)
	}
}
