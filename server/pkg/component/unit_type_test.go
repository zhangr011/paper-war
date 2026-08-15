package component

import (
	"math"
	"testing"
)

func TestCombatUnitTypeTableHasAllTypes(t *testing.T) {
	types := []CombatUnitType{
		UnitLightInfantry, UnitHeavyInfantry, UnitSniper,
		UnitAntiArmorInfantry, UnitMotorGun, UnitMotorArtillery, UnitMotorMissile,
	}
	for _, ut := range types {
		if _, ok := CombatUnitTypeTable[ut]; !ok {
			t.Errorf("CombatUnitTypeTable missing type %d", ut)
		}
	}
}

func TestCombatUnitStats(t *testing.T) {
	tests := []struct {
		Type        CombatUnitType
		Weapon      WeaponType
		Armor       ArmorType
		Cost        int32
		HP          int32
		Damage      int32
		Range       int64
		Cooldown    uint8
		RecruitCost int32
		KillBounty  int32
	}{
		{UnitLightInfantry, WeaponGun, ArmorLight, 1, 100, 15, 3, 3, 15, 12},
		{UnitHeavyInfantry, WeaponCannon, ArmorLight, 2, 60, 25, 4, 5, 25, 20},
		{UnitSniper, WeaponSniper, ArmorLight, 1, 30, 20, 4, 12, 50, 40},
		{UnitAntiArmorInfantry, WeaponMissile, ArmorLight, 2, 60, 35, 4, 6, 30, 24},
		{UnitMotorGun, WeaponGun, ArmorHeavy, 2, 120, 15, 3, 2, 25, 20},
		{UnitMotorArtillery, WeaponCannon, ArmorHeavy, 4, 150, 40, 4, 5, 50, 40},
		{UnitMotorMissile, WeaponMissile, ArmorHeavy, 4, 130, 50, 5, 7, 60, 48},
	}

	for _, tt := range tests {
		stats, ok := CombatUnitTypeTable[tt.Type]
		if !ok {
			t.Errorf("type %d not in table", tt.Type)
			continue
		}
		if stats.Weapon != tt.Weapon {
			t.Errorf("%d: Weapon = %d, want %d", tt.Type, stats.Weapon, tt.Weapon)
		}
		if stats.Armor != tt.Armor {
			t.Errorf("%d: Armor = %d, want %d", tt.Type, stats.Armor, tt.Armor)
		}
		if stats.Cost != tt.Cost {
			t.Errorf("%d: Cost = %d, want %d", tt.Type, stats.Cost, tt.Cost)
		}
		if stats.HP != tt.HP {
			t.Errorf("%d: HP = %d, want %d", tt.Type, stats.HP, tt.HP)
		}
		if stats.Damage != tt.Damage {
			t.Errorf("%d: Damage = %d, want %d", tt.Type, stats.Damage, tt.Damage)
		}
		if stats.Range != tt.Range {
			t.Errorf("%d: Range = %d, want %d", tt.Type, stats.Range, tt.Range)
		}
		if stats.Cooldown != tt.Cooldown {
			t.Errorf("%d: Cooldown = %d, want %d", tt.Type, stats.Cooldown, tt.Cooldown)
		}
		if stats.RecruitCost != tt.RecruitCost {
			t.Errorf("%d: RecruitCost = %d, want %d", tt.Type, stats.RecruitCost, tt.RecruitCost)
		}
		if stats.KillBounty != tt.KillBounty {
			t.Errorf("%d: KillBounty = %d, want %d", tt.Type, stats.KillBounty, tt.KillBounty)
		}
	}
}

func TestKillBountyIs80Percent(t *testing.T) {
	for ut, stats := range CombatUnitTypeTable {
		expected := int32(math.Round(float64(stats.RecruitCost) * 0.8))
		if stats.KillBounty != expected {
			t.Errorf("type %d: KillBounty = %d, want 80%% of %d = %d", ut, stats.KillBounty, stats.RecruitCost, expected)
		}
	}
}

func TestAllTypesHavePositiveStats(t *testing.T) {
	for ut, stats := range CombatUnitTypeTable {
		if stats.HP <= 0 {
			t.Errorf("type %d: HP = %d, want > 0", ut, stats.HP)
		}
		if stats.Damage <= 0 {
			t.Errorf("type %d: Damage = %d, want > 0", ut, stats.Damage)
		}
		if stats.Range <= 0 {
			t.Errorf("type %d: Range = %d, want > 0", ut, stats.Range)
		}
		if stats.RecruitCost <= 0 {
			t.Errorf("type %d: RecruitCost = %d, want > 0", ut, stats.RecruitCost)
		}
	}
}

func TestDamageMatrix(t *testing.T) {
	tests := []struct {
		Weapon WeaponType
		Armor  ArmorType
		Want   int32
	}{
		{WeaponGun, ArmorLight, 100},
		{WeaponGun, ArmorHeavy, 50},
		{WeaponGun, ArmorBuilding, 0},
		{WeaponCannon, ArmorLight, 50},
		{WeaponCannon, ArmorHeavy, 100},
		{WeaponCannon, ArmorBuilding, 25},
		{WeaponSniper, ArmorLight, 100},
		{WeaponSniper, ArmorHeavy, 25},
		{WeaponSniper, ArmorBuilding, 0},
		{WeaponMissile, ArmorLight, 25},
		{WeaponMissile, ArmorHeavy, 150},
		{WeaponMissile, ArmorBuilding, 25},
	}

	for _, tt := range tests {
		got := DamageMultiplier(tt.Weapon, tt.Armor)
		if got != tt.Want {
			t.Errorf("DamageMultiplier(%d, %d) = %d, want %d", tt.Weapon, tt.Armor, got, tt.Want)
		}
	}
}

func TestCanDamageTerrain(t *testing.T) {
	tests := []struct {
		Weapon WeaponType
		Want   bool
	}{
		{WeaponGun, false},
		{WeaponCannon, true},
		{WeaponSniper, false},
		{WeaponMissile, true},
	}

	for _, tt := range tests {
		got := CanDamageTerrain(tt.Weapon)
		if got != tt.Want {
			t.Errorf("CanDamageTerrain(%d) = %v, want %v", tt.Weapon, got, tt.Want)
		}
	}
}

func TestWeaponTypeCount(t *testing.T) {
	max := WeaponType(0)
	for _, w := range []WeaponType{WeaponGun, WeaponCannon, WeaponSniper, WeaponMissile} {
		if w > max {
			max = w
		}
	}
	if max != 3 {
		t.Errorf("max WeaponType = %d, want 3 (4 values: 0-3)", max)
	}
}

func TestArmorTypeCount(t *testing.T) {
	max := ArmorType(0)
	for _, a := range []ArmorType{ArmorLight, ArmorHeavy, ArmorBuilding} {
		if a > max {
			max = a
		}
	}
	if max != 2 {
		t.Errorf("max ArmorType = %d, want 2 (3 values: 0-2)", max)
	}
}

func TestCombatUnitTypeNameRoundtrip(t *testing.T) {
	// Every type in the table should round-trip through name -> parse
	for ut := range CombatUnitTypeTable {
		name := CombatUnitTypeName(ut)
		if name == "" {
			t.Errorf("type %d: CombatUnitTypeName returned empty string", ut)
			continue
		}
		parsed, ok := ParseCombatUnitType(name)
		if !ok {
			t.Errorf("type %d: ParseCombatUnitType(%q) failed", ut, name)
			continue
		}
		if parsed != ut {
			t.Errorf("type %d: name %q parsed back to %d, want %d", ut, name, parsed, ut)
		}
	}
}

func TestCombatUnitTypeNameAllKnownTypes(t *testing.T) {
	tests := []struct {
		Type CombatUnitType
		Name string
	}{
		{UnitLightInfantry, "LightInfantry"},
		{UnitHeavyInfantry, "HeavyInfantry"},
		{UnitSniper, "Sniper"},
		{UnitAntiArmorInfantry, "AntiArmorInfantry"},
		{UnitMotorGun, "MotorGun"},
		{UnitMotorArtillery, "MotorArtillery"},
		{UnitMotorMissile, "MotorMissile"},
	}
	for _, tt := range tests {
		got := CombatUnitTypeName(tt.Type)
		if got != tt.Name {
			t.Errorf("CombatUnitTypeName(%d) = %q, want %q", tt.Type, got, tt.Name)
		}
	}
}

func TestCombatUnitTypeNameUnknownType(t *testing.T) {
	// Invalid type should return default "LightInfantry"
	got := CombatUnitTypeName(CombatUnitType(99))
	if got != "LightInfantry" {
		t.Errorf("CombatUnitTypeName(99) = %q, want %q", got, "LightInfantry")
	}
}

func TestParseCombatUnitTypeValid(t *testing.T) {
	valid := map[string]CombatUnitType{
		"LightInfantry":     UnitLightInfantry,
		"HeavyInfantry":     UnitHeavyInfantry,
		"Sniper":            UnitSniper,
		"AntiArmorInfantry": UnitAntiArmorInfantry,
		"MotorGun":          UnitMotorGun,
		"MotorArtillery":    UnitMotorArtillery,
		"MotorMissile":      UnitMotorMissile,
	}
	for name, want := range valid {
		got, ok := ParseCombatUnitType(name)
		if !ok {
			t.Errorf("ParseCombatUnitType(%q): ok = false, want true", name)
			continue
		}
		if got != want {
			t.Errorf("ParseCombatUnitType(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestParseCombatUnitTypeInvalid(t *testing.T) {
	invalid := []string{"", "Unknown", "infantry", "light_infantry", "TANK", "sniper"}
	for _, name := range invalid {
		got, ok := ParseCombatUnitType(name)
		if ok {
			t.Errorf("ParseCombatUnitType(%q): ok = true, want false (got type %d)", name, got)
		}
		if got != UnitLightInfantry {
			t.Errorf("ParseCombatUnitType(%q) = %d on failure, want default %d", name, got, UnitLightInfantry)
		}
	}
}

func TestDamageMultiplierInvalidWeapon(t *testing.T) {
	// Out-of-bounds weapon should return 0
	got := DamageMultiplier(WeaponType(99), ArmorLight)
	if got != 0 {
		t.Errorf("DamageMultiplier(99, 0) = %d, want 0", got)
	}
}

func TestDamageMultiplierInvalidArmor(t *testing.T) {
	// Out-of-bounds armor should return 0
	got := DamageMultiplier(WeaponGun, ArmorType(99))
	if got != 0 {
		t.Errorf("DamageMultiplier(0, 99) = %d, want 0", got)
	}
}

func TestCombatUnitTypeNamesCount(t *testing.T) {
	// Should have exactly 7 entries
	if len(CombatUnitTypeNames) != 7 {
		t.Errorf("len(CombatUnitTypeNames) = %d, want 7", len(CombatUnitTypeNames))
	}
}
