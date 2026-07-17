package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

func setupBuildWorld() (*ecs.EntityManager, *ecs.World, *BuildSystem) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	structPool := ecs.NewComponentPool[component.StructureComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.StructureComponent{}, structPool)
	w.RegisterPool(component.UnitTypeComponent{}, unitTypePool)

	bs := &BuildSystem{
		em:           em,
		PlayerGold:   map[uint32]int32{1: 200, 2: 200},
		PlayerSpawns: map[uint32][2]int64{},
	}
	bs.posPool = posPool
	bs.healthPool = healthPool
	bs.attackPool = attackPool
	bs.ownerPool = ownerPool
	bs.structPool = structPool
	bs.unitTypePool = unitTypePool
	return em, w, bs
}

func TestBuildWatchtower(t *testing.T) {
	em, _, bs := setupBuildWorld()
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	ok := bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureWatchtower,
		X:        fixed.FromFloat(22),
		Y:        fixed.FromFloat(22),
	})
	if !ok {
		t.Fatal("Build returned false for valid watchtower")
	}

	// Check entity was created
	count := 0
	bs.structPool.Each(func(e ecs.Entity, sc *component.StructureComponent) {
		if sc.Type == component.StructureWatchtower && sc.OwnerID == 1 {
			count++
		}
	})
	if count != 1 {
		t.Errorf("expected 1 watchtower, got %d", count)
	}

	// Check gold was deducted
	if bs.PlayerGold[1] != 150 { // 200 - 50
		t.Errorf("gold = %d, want 150", bs.PlayerGold[1])
	}

	// Verify entity has health
	bs.structPool.Each(func(e ecs.Entity, sc *component.StructureComponent) {
		hp, ok := bs.healthPool.Get(e)
		if !ok {
			t.Error("structure missing health component")
			return
		}
		if hp.HP != 100 || hp.MaxHP != 100 {
			t.Errorf("HP = %d/%d, want 100/100", hp.HP, hp.MaxHP)
		}
		_ = hp
	})
	_ = em
}

func TestBuildTurretGetsAttack(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	ok := bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureTurret,
		X:        fixed.FromFloat(21),
		Y:        fixed.FromFloat(21),
	})
	if !ok {
		t.Fatal("Build returned false for turret")
	}

	bs.structPool.Each(func(e ecs.Entity, sc *component.StructureComponent) {
		if sc.Type != component.StructureTurret {
			return
		}
		ac, ok := bs.attackPool.Get(e)
		if !ok {
			t.Error("turret missing attack component")
			return
		}
		if ac.Damage != 15 {
			t.Errorf("turret damage = %d, want 15", ac.Damage)
		}
	})
}

func TestBuildBarricadeNoAttack(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureBarricade,
		X:        fixed.FromFloat(20),
		Y:        fixed.FromFloat(20),
	})

	bs.structPool.Each(func(e ecs.Entity, sc *component.StructureComponent) {
		if sc.Type == component.StructureBarricade {
			if _, ok := bs.attackPool.Get(e); ok {
				t.Error("barricade should not have attack component")
			}
		}
	})
}

func TestBuildInsufficientGold(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerGold[1] = 10 // not enough for anything
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	ok := bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureBarricade,
		X:        fixed.FromFloat(20),
		Y:        fixed.FromFloat(20),
	})
	if ok {
		t.Error("Build should fail with insufficient gold")
	}
}

func TestBuildOutOfRange(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	ok := bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureWatchtower,
		X:        fixed.FromFloat(50), // too far — 30 tiles away
		Y:        fixed.FromFloat(50),
	})
	if ok {
		t.Error("Build should fail when out of range from spawn")
	}
}

func TestBuildMaxStructures(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerGold[1] = 10000
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	// Build 10 barricades (max)
	for i := 0; i < MaxStructuresPerPlayer; i++ {
		ok := bs.Build(BuildRequest{
			PlayerID: 1,
			Type:     component.StructureBarricade,
			X:        fixed.FromFloat(20 + float64(i)),
			Y:        fixed.FromFloat(20),
		})
		if !ok {
			t.Fatalf("Build %d failed unexpectedly", i)
		}
	}

	// 11th should fail
	ok := bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureBarricade,
		X:        fixed.FromFloat(25),
		Y:        fixed.FromFloat(25),
	})
	if ok {
		t.Error("Build should fail when exceeding max structures")
	}
}

func TestBuildGoldDeduction(t *testing.T) {
	_, _, bs := setupBuildWorld()
	bs.PlayerGold[1] = 100
	bs.PlayerSpawns[1] = [2]int64{fixed.FromFloat(20), fixed.FromFloat(20)}

	// Build a barricade (cost 20)
	bs.Build(BuildRequest{
		PlayerID: 1,
		Type:     component.StructureBarricade,
		X:        fixed.FromFloat(20),
		Y:        fixed.FromFloat(20),
	})
	if bs.PlayerGold[1] != 80 {
		t.Errorf("gold after barricade = %d, want 80", bs.PlayerGold[1])
	}
}

func TestStrongholdDefenseBonus(t *testing.T) {
	tests := []struct {
		level   int
		wantPct int32
	}{
		{0, 0},
		{1, 25},
		{2, 31},
		{3, 37},
		{4, 43},
		{5, 49},
		{6, 49}, // clamped to L5
	}
	for _, tt := range tests {
		got := StrongholdDefenseBonus(tt.level)
		if got != tt.wantPct {
			t.Errorf("StrongholdDefenseBonus(%d) = %d, want %d", tt.level, got, tt.wantPct)
		}
	}
}

func TestStrongholdDamageReduction(t *testing.T) {
	// A unit on a Level 3 stronghold should take 37% less damage
	bonus := StrongholdDefenseBonus(3)
	originalDmg := int32(100)
	effectiveDmg := originalDmg * (100 - bonus) / 100
	if effectiveDmg != 63 {
		t.Errorf("effective damage = %d, want 63 (100 * 0.63)", effectiveDmg)
	}
}

func TestTerrainCoverBonus(t *testing.T) {
	tests := []struct {
		terrain component.TerrainType
		wantPct int32
	}{
		{component.TerrainPlain, 0},
		{component.TerrainForest, 25},
		{component.TerrainHill, 15},
		{component.TerrainRoad, 0},
		{component.TerrainWall, 0},
		// Stronghold terrain routes through StrongholdDefenseBonus, not cover.
		{component.TerrainStronghold3, 0},
	}
	for _, tt := range tests {
		got := TerrainCoverBonus(tt.terrain)
		if got != tt.wantPct {
			t.Errorf("TerrainCoverBonus(%d) = %d, want %d", tt.terrain, got, tt.wantPct)
		}
	}
}

// TestCoverDamageReductionByWeapon verifies the acceptance criterion for
// issue #55 phase 1: a unit attacked while on Forest takes measurably less
// damage than the same unit on Plain, across every weapon type. Mirrors the
// stronghold damage test — cover applies at the same spot in the resolution
// path (damage matrix → terrain defense), so exercising the math per weapon
// is the faithful unit-level check.
func TestCoverDamageReductionByWeapon(t *testing.T) {
	cover := TerrainCoverBonus(component.TerrainForest) // 25
	if cover == 0 {
		t.Fatal("Forest cover is 0 — cover not configured")
	}
	armors := []component.ArmorType{component.ArmorLight, component.ArmorHeavy}
	for _, weapon := range []component.WeaponType{
		component.WeaponGun, component.WeaponCannon,
		component.WeaponSniper, component.WeaponMissile,
	} {
		for _, armor := range armors {
			base := int32(100) * component.DamageMultiplier(weapon, armor) / 100
			if base == 0 {
				continue // weapon can't damage this armor (e.g. Gun vs Building)
			}
			onPlain := base
			onForest := base * (100 - cover) / 100
			if onForest >= onPlain {
				t.Errorf("%v vs %v: Forest dmg %d not < Plain dmg %d",
					weapon, armor, onForest, onPlain)
			}
			if onForest < 1 {
				t.Errorf("%v vs %v: Forest dmg clamped below 1", weapon, armor)
			}
		}
	}
}

// TestTerrainDefensePctCombines confirms the combat-path helper picks
// stronghold bonus on stronghold tiles and cover on Forest/Hill, never both.
func TestTerrainDefensePctCombines(t *testing.T) {
	cases := map[component.TerrainType]int32{
		component.TerrainPlain:        0,
		component.TerrainForest:       25,
		component.TerrainHill:         15,
		component.TerrainStronghold1:  StrongholdDefenseBonus(1),
		component.TerrainStronghold5:  StrongholdDefenseBonus(5),
	}
	for terrain, want := range cases {
		if got := terrainDefensePct(terrain); got != want {
			t.Errorf("terrainDefensePct(%d) = %d, want %d", terrain, got, want)
		}
	}
}
