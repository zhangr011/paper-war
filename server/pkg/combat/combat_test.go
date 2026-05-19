package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

func setupCombatWorld() (*ecs.EntityManager, *ecs.World, *spatial.Hash,
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.AttackComponent],
	*ecs.ComponentPool[component.BoidComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	sh := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	utPool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.UnitTypeComponent{}, utPool)

	cs := &CombatSystem{Sh: sh}
	w.AddSystem(cs)
	w.Init()

	return em, w, sh, posPool, healthPool, attackPool, boidPool, utPool
}

func rebuildSpatialHash(sh *spatial.Hash, posPool *ecs.ComponentPool[component.PositionComponent]) {
	sh.Clear()
	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		sh.Insert(uint64(e), pos.X, pos.Y)
	})
}

func TestCombatGunVsLight(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	// Gun attacker (100% vs Light)
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(5.0), Damage: 10, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	// Light armor target
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// Gun vs Light = 100%, so 10 * 100/100 = 10 damage
	if hp.HP != 90 {
		t.Errorf("target HP = %d, want 90 (100 - 10*100/100)", hp.HP)
	}
}

func TestCombatGunVsHeavy(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(5.0), Damage: 10, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	// Heavy armor target
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitHeavyInfantry, Weapon: component.WeaponCannon, Armor: component.ArmorHeavy})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// Gun vs Heavy = 50%, so 10 * 50/100 = 5 damage
	if hp.HP != 95 {
		t.Errorf("target HP = %d, want 95 (100 - 10*50/100)", hp.HP)
	}
}

func TestCombatSniperVsLight(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(10.0), Damage: 10, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitSniper, Weapon: component.WeaponSniper, Armor: component.ArmorLight})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(8.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// Sniper vs Light = 150%, so 10 * 150/100 = 15 damage
	if hp.HP != 85 {
		t.Errorf("target HP = %d, want 85 (100 - 10*150/100)", hp.HP)
	}
}

func TestCombatMissileCannotDamageLight(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(8.0), Damage: 10, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitAntiArmorInfantry, Weapon: component.WeaponMissile, Armor: component.ArmorLight})

	// Light target - Missile does 25% to Light
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// Missile vs Light = 25%, so 10 * 25/100 = 2 (integer division gives 2)
	if hp.HP != 98 {
		t.Errorf("target HP = %d, want 98 (100 - 10*25/100 = 2)", hp.HP)
	}
}

func TestCombatSplash(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	// Cannon attacker
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(7.0), Damage: 20, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitMotorArtillery, Weapon: component.WeaponCannon, Armor: component.ArmorHeavy})

	// Primary target
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(5.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	// Adjacent enemy (should get splash)
	adjacent := em.Create()
	posPool.Add(adjacent, component.PositionComponent{X: fixed.FromFloat(5.5), Y: fixed.FromFloat(1.0)})
	healthPool.Add(adjacent, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(adjacent, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(adjacent, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	// Primary: Cannon vs Light = 50%, so 20 * 50/100 = 10 damage
	hp, _ := healthPool.Get(target)
	if hp.HP != 90 {
		t.Errorf("primary target HP = %d, want 90", hp.HP)
	}

	// Splash: 10/2 = 5 damage
	hp2, _ := healthPool.Get(adjacent)
	if hp2.HP != 95 {
		t.Errorf("splash target HP = %d, want 95", hp2.HP)
	}
}

func TestSmartTargetingPrioritizesCommander(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	// Gun attacker
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(10.0), Damage: 10, Cooldown: 1,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1, Role: component.RoleMelee})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	// Regular unit (closer)
	regular := em.Create()
	posPool.Add(regular, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(regular, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(regular, component.BoidComponent{SquadID: 2, Role: component.RoleMelee})
	utPool.Add(regular, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	// Commander (farther but priority 1)
	cmd := em.Create()
	posPool.Add(cmd, component.PositionComponent{X: fixed.FromFloat(5.0), Y: 0})
	healthPool.Add(cmd, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(cmd, component.BoidComponent{SquadID: 2, Role: component.RoleCommander})
	utPool.Add(cmd, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	ac, _ := attackPool.Get(attacker)
	if ac.TargetID != uint32(cmd) {
		t.Errorf("target = %d (regular), want %d (commander)", ac.TargetID, cmd)
	}
}

func TestCombatOutOfRange(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, utPool := setupCombatWorld()

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{
		Range: fixed.FromFloat(2.0), Damage: 10,
	})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
	utPool.Add(attacker, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(10.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2})
	utPool.Add(target, component.UnitTypeComponent{Type: component.UnitLightInfantry, Weapon: component.WeaponGun, Armor: component.ArmorLight})

	rebuildSpatialHash(sh, posPool)
	w.Tick(1)

	hp, _ := healthPool.Get(target)
	if hp.HP != 100 {
		t.Errorf("out of range target HP = %d, want 100 (undamaged)", hp.HP)
	}
}
