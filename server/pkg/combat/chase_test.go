package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// setupChaseWorld creates a combat world with PathfindingComponent registered,
// needed to test the chase/pursue behavior.
func setupChaseWorld() (*ecs.EntityManager, *ecs.World, *spatial.Hash,
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.AttackComponent],
	*ecs.ComponentPool[component.BoidComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	*ecs.ComponentPool[component.PathfindingComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	utPool := ecs.NewComponentPool[component.UnitTypeComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.UnitTypeComponent{}, utPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	cs := &CombatSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	return em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, pathPool
}

// TestCombatChaseOutOfRange verifies that a unit with an enemy just outside
// attack range (but within chase range = 2x attack range) sets its
// pathfinding target to pursue the enemy.
func TestCombatChaseOutOfRange(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, pathPool := setupChaseWorld()

	// Attacker at (0,0), range 5 tiles, target at 7 tiles (out of attack
	// range but within chase range of 10 tiles)
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range:    fixed.FromFloat(5.0),
		Damage:   10,
		Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
	utPool.Add(attacker, component.UnitTypeComponent{
		Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight,
	})
	ownerPool.Add(attacker, component.OwnerComponent{PlayerID: 1, Faction: 0})
	pathPool.Add(attacker, component.PathfindingComponent{})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(7.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2})
	utPool.Add(target, component.UnitTypeComponent{
		Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight,
	})
	ownerPool.Add(target, component.OwnerComponent{PlayerID: 2, Faction: 1})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	// The attacker should NOT have dealt damage (target out of range)
	hp, _ := healthPool.Get(target)
	if hp.HP != 100 {
		t.Errorf("out-of-range target HP = %d, want 100 (no damage)", hp.HP)
	}

	// The attacker SHOULD have set its pathfinding target to pursue
	path, ok := pathPool.GetPtr(attacker)
	if !ok {
		t.Fatal("attacker has no PathfindingComponent")
	}
	targetX := fixed.ToFloat(path.TargetX)
	if path.TargetX == 0 {
		t.Error("pathfinding TargetX not set — unit is not pursuing out-of-range enemy")
	}
	if !approxEqual(targetX, 7.0, 0.01) {
		t.Errorf("pathfinding TargetX = %.2f, want ~7.0 (enemy position)", targetX)
	}
}

// TestCombatChaseClosesGapAndAttacks verifies that over multiple ticks (with
// a mock movement system), the attacker eventually gets in range and deals
// damage. Since we don't have a movement system in this test, we manually
// move the attacker toward the target to simulate the chase.
func TestCombatChaseClosesGapAndAttacks(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool, ownerPool, pathPool := setupChaseWorld()
	_ = pathPool

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range:    fixed.FromFloat(5.0),
		Damage:   10,
		Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
	utPool.Add(attacker, component.UnitTypeComponent{
		Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight,
	})
	ownerPool.Add(attacker, component.OwnerComponent{PlayerID: 1, Faction: 0})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(7.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2})
	utPool.Add(target, component.UnitTypeComponent{
		Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight,
	})
	ownerPool.Add(target, component.OwnerComponent{PlayerID: 2, Faction: 1})

	// Tick 1: attacker detects target in chase range, sets pathfinding
	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	if hp.HP != 100 {
		t.Errorf("tick 1: target HP = %d, want 100 (still out of range)", hp.HP)
	}

	// Simulate movement: move attacker 3 tiles closer (from 0 to 3)
	// Now distance is 7-3 = 4 tiles, within attack range of 5
	attackerPos, _ := posPool.GetPtr(attacker)
	attackerPos.X = fixed.FromFloat(3.0)

	rebuildSpatialHash(sh, posPool)
	w.Tick(2)

	// Now in range — should have dealt damage
	hp, _ = healthPool.Get(target)
	if hp.HP >= 100 {
		t.Errorf("tick 2: target HP = %d, want < 100 (should be damaged after closing gap)", hp.HP)
	}
}

func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}
