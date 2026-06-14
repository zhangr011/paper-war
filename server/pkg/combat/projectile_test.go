package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// ===================== ProjectileSystem =====================

func setupProjectileWorld() (*ecs.EntityManager, *ecs.World,
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.ProjectileComponent],
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	projPool := ecs.NewComponentPool[component.ProjectileComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.ProjectileComponent{}, projPool)

	w.AddSystem(&ProjectileSystem{})
	w.Init()

	return em, w, posPool, healthPool, projPool
}

func TestProjectileSystemName(t *testing.T) {
	ps := &ProjectileSystem{}
	if ps.Name() != "ProjectileSystem" {
		t.Errorf("Name() = %q, want ProjectileSystem", ps.Name())
	}
	if ps.Priority() != 85 {
		t.Errorf("Priority() = %d, want 85", ps.Priority())
	}
}

func TestProjectileSingleTargetDamage(t *testing.T) {
	em, w, posPool, healthPool, projPool := setupProjectileWorld()

	// Target at position (5, 5)
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})

	// Projectile heading toward (5, 5)
	proj := em.Create()
	posPool.Add(proj, component.PositionComponent{X: fixed.FromFloat(4.0), Y: fixed.FromFloat(5.0)})
	projPool.Add(proj, component.ProjectileComponent{
		DX:         fixed.FromFloat(1.0),
		DY:         0,
		TargetX:    fixed.FromFloat(5.0),
		TargetY:    fixed.FromFloat(5.0),
		Damage:     50,
		ImpactTick: 0, // arrives immediately
	})

	w.Tick(1)

	// Target should take 50 damage
	hp, ok := healthPool.Get(target)
	if !ok {
		t.Fatal("target health component missing")
	}
	if hp.HP != 50 {
		t.Errorf("target HP = %d, want 50 (100 - 50 damage)", hp.HP)
	}

	// Projectile should be removed
	if _, ok := projPool.Get(proj); ok {
		t.Error("projectile should have been removed after impact")
	}
}

func TestProjectileSplashDamage(t *testing.T) {
	em, w, posPool, healthPool, projPool := setupProjectileWorld()

	// Three targets near impact point (5, 5)
	target1 := em.Create()
	posPool.Add(target1, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	healthPool.Add(target1, component.HealthComponent{HP: 100, MaxHP: 100})

	target2 := em.Create()
	posPool.Add(target2, component.PositionComponent{X: fixed.FromFloat(5.5), Y: fixed.FromFloat(5.0)})
	healthPool.Add(target2, component.HealthComponent{HP: 100, MaxHP: 100})

	target3 := em.Create()
	posPool.Add(target3, component.PositionComponent{X: fixed.FromFloat(10.0), Y: fixed.FromFloat(10.0)}) // far away
	healthPool.Add(target3, component.HealthComponent{HP: 100, MaxHP: 100})

	// Splash projectile with radius 2.0
	proj := em.Create()
	posPool.Add(proj, component.PositionComponent{X: fixed.FromFloat(4.0), Y: fixed.FromFloat(5.0)})
	projPool.Add(proj, component.ProjectileComponent{
		DX:           fixed.FromFloat(1.0),
		DY:           0,
		TargetX:      fixed.FromFloat(5.0),
		TargetY:      fixed.FromFloat(5.0),
		Damage:       40,
		ImpactTick:   0,
		SplashRadius: fixed.FromFloat(2.0),
	})

	w.Tick(1)

	// target1 and target2 should be damaged, target3 should not
	hp1, _ := healthPool.Get(target1)
	if hp1.HP != 60 {
		t.Errorf("target1 HP = %d, want 60", hp1.HP)
	}
	hp2, _ := healthPool.Get(target2)
	if hp2.HP != 60 {
		t.Errorf("target2 HP = %d, want 60", hp2.HP)
	}
	hp3, _ := healthPool.Get(target3)
	if hp3.HP != 100 {
		t.Errorf("target3 HP = %d, want 100 (outside splash)", hp3.HP)
	}
}

func TestProjectileTickNilPool(t *testing.T) {
	// ProjectileSystem with nil projPool should not crash on Tick
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	// No ProjectileComponent pool registered!
	w.AddSystem(&ProjectileSystem{})
	w.Init()

	// Should not panic
	w.Tick(1)
}

func TestProjectileArmorReduction(t *testing.T) {
	em, w, posPool, healthPool, projPool := setupProjectileWorld()

	// Target with armor
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100, Armor: 10})

	proj := em.Create()
	posPool.Add(proj, component.PositionComponent{X: fixed.FromFloat(4.0), Y: fixed.FromFloat(5.0)})
	projPool.Add(proj, component.ProjectileComponent{
		DX:         fixed.FromFloat(1.0),
		DY:         0,
		TargetX:    fixed.FromFloat(5.0),
		TargetY:    fixed.FromFloat(5.0),
		Damage:     40,
		ImpactTick: 0,
	})

	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// dmg = 40 - 10 (armor) = 30; HP = 100 - 30 = 70
	if hp.HP != 70 {
		t.Errorf("HP = %d, want 70 (100 - (40-10) armor)", hp.HP)
	}
}

func TestProjectileMinDamage(t *testing.T) {
	em, w, posPool, healthPool, projPool := setupProjectileWorld()

	// Target with heavy armor
	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(5.0), Y: fixed.FromFloat(5.0)})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100, Armor: 100})

	proj := em.Create()
	posPool.Add(proj, component.PositionComponent{X: fixed.FromFloat(4.0), Y: fixed.FromFloat(5.0)})
	projPool.Add(proj, component.ProjectileComponent{
		DX:         fixed.FromFloat(1.0),
		DY:         0,
		TargetX:    fixed.FromFloat(5.0),
		TargetY:    fixed.FromFloat(5.0),
		Damage:     5,
		ImpactTick: 0,
	})

	w.Tick(1)

	hp, _ := healthPool.Get(target)
	// dmg = max(1, 5-100) = 1; HP = 100 - 1 = 99
	if hp.HP != 99 {
		t.Errorf("HP = %d, want 99 (min 1 damage through armor)", hp.HP)
	}
}

// ===================== isTargetValid =====================

func TestIsTargetValidInFaction(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, _ := setupCombatWorld()
	_ = sh

	// Register OwnerComponent pool and re-init
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	// Re-acquire the combat system (it was initialized without ownerPool)
	cs := w.SystemByName("CombatSystem").(*CombatSystem)
	cs.ownerPool = ownerPool // manually set since Init already ran

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(10.0)})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})
	ownerPool.Add(attacker, component.OwnerComponent{PlayerID: 1, Faction: 0})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(3.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	ownerPool.Add(target, component.OwnerComponent{PlayerID: 1, Faction: 0}) // same faction

	ac, _ := attackPool.GetPtr(attacker)
	pos, _ := posPool.Get(attacker)

	valid := cs.isTargetValid(attacker, ac, pos)
	if valid {
		t.Error("target in same faction should be invalid")
	}
}

func TestIsTargetValidOutOfRange(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, _ := setupCombatWorld()
	_ = sh
	cs := w.SystemByName("CombatSystem").(*CombatSystem)

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(3.0)})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(10.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})

	ac, _ := attackPool.GetPtr(attacker)
	pos, _ := posPool.Get(attacker)

	valid := cs.isTargetValid(attacker, ac, pos)
	if valid {
		t.Error("target out of range should be invalid")
	}
}

func TestIsTargetValidDeadTarget(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, _ := setupCombatWorld()
	_ = sh
	cs := w.SystemByName("CombatSystem").(*CombatSystem)

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(10.0)})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(2.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 0, MaxHP: 100}) // dead

	ac, _ := attackPool.GetPtr(attacker)
	pos, _ := posPool.Get(attacker)

	valid := cs.isTargetValid(attacker, ac, pos)
	if valid {
		t.Error("dead target should be invalid")
	}
}

func TestIsTargetValidInRange(t *testing.T) {
	em, w, sh, posPool, healthPool, attackPool, boidPool, _ := setupCombatWorld()
	_ = sh
	cs := w.SystemByName("CombatSystem").(*CombatSystem)

	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: 0, Y: 0})
	boidPool.Add(attacker, component.BoidComponent{SquadID: 1})

	target := em.Create()
	posPool.Add(target, component.PositionComponent{X: fixed.FromFloat(5.0), Y: 0})
	healthPool.Add(target, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(target, component.BoidComponent{SquadID: 2})

	attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(10.0), TargetID: uint32(target)})

	ac, _ := attackPool.GetPtr(attacker)
	pos, _ := posPool.Get(attacker)

	valid := cs.isTargetValid(attacker, ac, pos)
	if !valid {
		t.Error("alive target in range should be valid")
	}
}

// ===================== strongholdLevelFromTerrain =====================

func TestStrongholdLevelFromTerrain(t *testing.T) {
	tests := []struct {
		terrain component.TerrainType
		want    int
	}{
		{component.TerrainStronghold1, 1},
		{component.TerrainStronghold2, 2},
		{component.TerrainStronghold3, 3},
		{component.TerrainStronghold4, 4},
		{component.TerrainStronghold5, 5},
		{component.TerrainPlain, 0},
		{component.TerrainForest, 0},
		{component.TerrainDeep, 0},
		{component.TerrainHill, 0},
		{component.TerrainDesert, 0},
		{component.TerrainSnow, 0},
		{component.TerrainWall, 0},
	}
	for _, tt := range tests {
		got := strongholdLevelFromTerrain(tt.terrain)
		if got != tt.want {
			t.Errorf("strongholdLevelFromTerrain(%d) = %d, want %d", tt.terrain, got, tt.want)
		}
	}
}

// ===================== ValidateRecruit =====================

func TestValidateRecruitUnknownType(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	cmdr := em.Create()
	healthPool.Add(cmdr, component.HealthComponent{HP: 100, MaxHP: 100})

	rs := &RecruitmentSystem{}
	rs.healthPool = healthPool

	err := rs.ValidateRecruit(RecruitRequest{
		CommanderEntity: cmdr,
		UnitType:        component.CombatUnitType(99),
	})
	if err == nil {
		t.Error("ValidateRecruit should fail for unknown unit type")
	}
}

func TestValidateRecruitDeadCommander(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	w.RegisterPool(component.HealthComponent{}, healthPool)

	cmdr := em.Create()
	healthPool.Add(cmdr, component.HealthComponent{HP: 0, MaxHP: 100})

	rs := &RecruitmentSystem{}
	rs.healthPool = healthPool

	err := rs.ValidateRecruit(RecruitRequest{
		CommanderEntity: cmdr,
		UnitType:        component.UnitLightInfantry,
	})
	if err == nil {
		t.Error("ValidateRecruit should fail for dead commander")
	}
}

func TestValidateRecruitValid(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	w.RegisterPool(component.HealthComponent{}, healthPool)

	cmdr := em.Create()
	healthPool.Add(cmdr, component.HealthComponent{HP: 100, MaxHP: 100})

	rs := &RecruitmentSystem{}
	rs.healthPool = healthPool

	err := rs.ValidateRecruit(RecruitRequest{
		CommanderEntity: cmdr,
		UnitType:        component.UnitLightInfantry,
	})
	if err != nil {
		t.Errorf("ValidateRecruit should pass: %v", err)
	}
}

// ===================== System Interface Methods =====================

func TestCombatSystemInterface(t *testing.T) {
	cs := &CombatSystem{Sh: spatial.NewHash(fixed.FromFloat(3.0))}
	if cs.Name() != "CombatSystem" {
		t.Errorf("CombatSystem.Name() = %q, want CombatSystem", cs.Name())
	}
	if cs.Priority() != 80 {
		t.Errorf("CombatSystem.Priority() = %d, want 80", cs.Priority())
	}
}

func TestProjectileSystemInterface(t *testing.T) {
	ps := &ProjectileSystem{}
	if ps.Name() != "ProjectileSystem" {
		t.Errorf("ProjectileSystem.Name() = %q", ps.Name())
	}
}

func TestRecruitmentSystemInterface(t *testing.T) {
	rs := &RecruitmentSystem{}
	if rs.Name() != "RecruitmentSystem" {
		t.Errorf("RecruitmentSystem.Name() = %q, want RecruitmentSystem", rs.Name())
	}
	if rs.Priority() != 70 {
		t.Errorf("RecruitmentSystem.Priority() = %d, want 70", rs.Priority())
	}
}

func TestBuildSystemInterface(t *testing.T) {
	bs := &BuildSystem{}
	if bs.Name() != "BuildSystem" {
		t.Errorf("BuildSystem.Name() = %q, want BuildSystem", bs.Name())
	}
	if bs.Priority() != 65 {
		t.Errorf("BuildSystem.Priority() = %d, want 65", bs.Priority())
	}
}
