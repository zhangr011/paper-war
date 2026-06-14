package component

// WeaponType determines effectiveness against ArmorType via the Damage Matrix.
type WeaponType uint8

const (
	WeaponGun     WeaponType = 0
	WeaponCannon  WeaponType = 1
	WeaponSniper  WeaponType = 2
	WeaponMissile WeaponType = 3
)

// ArmorType determines vulnerability to WeaponType via the Damage Matrix.
type ArmorType uint8

const (
	ArmorLight    ArmorType = 0
	ArmorHeavy    ArmorType = 1
	ArmorBuilding ArmorType = 2
)

// CombatUnitType identifies one of the 7 predefined unit types.
type CombatUnitType uint8

const (
	UnitLightInfantry     CombatUnitType = 0
	UnitHeavyInfantry     CombatUnitType = 1
	UnitSniper            CombatUnitType = 2
	UnitAntiArmorInfantry CombatUnitType = 3
	UnitMotorGun          CombatUnitType = 4
	UnitMotorArtillery    CombatUnitType = 5
	UnitMotorMissile      CombatUnitType = 6
)

// CombatUnitStats holds the static stats for a CombatUnitType.
type CombatUnitStats struct {
	Type        CombatUnitType
	Weapon      WeaponType
	Armor       ArmorType
	Cost        int32  // Leading Skill cost
	HP          int32
	Damage      int32
	Range       int64  // in tiles
	Cooldown    uint8  // in ticks
	RecruitCost int32  // Gold to recruit
	KillBounty  int32  // Gold earned when killed (80% of RecruitCost, rounded)
}

// CombatUnitTypeTable maps each CombatUnitType to its stats.
var CombatUnitTypeTable = map[CombatUnitType]CombatUnitStats{
	UnitLightInfantry: {
		Type: UnitLightInfantry, Weapon: WeaponGun, Armor: ArmorLight,
		Cost: 1, HP: 100, Damage: 15, Range: 5, Cooldown: 3,
		RecruitCost: 15, KillBounty: 12,
	},
	UnitHeavyInfantry: {
		Type: UnitHeavyInfantry, Weapon: WeaponCannon, Armor: ArmorLight,
		Cost: 2, HP: 60, Damage: 25, Range: 7, Cooldown: 5,
		RecruitCost: 25, KillBounty: 20,
	},
	UnitSniper: {
		Type: UnitSniper, Weapon: WeaponSniper, Armor: ArmorLight,
		Cost: 1, HP: 30, Damage: 20, Range: 8, Cooldown: 12,
		RecruitCost: 50, KillBounty: 40,
	},
	UnitAntiArmorInfantry: {
		Type: UnitAntiArmorInfantry, Weapon: WeaponMissile, Armor: ArmorLight,
		Cost: 2, HP: 60, Damage: 35, Range: 8, Cooldown: 6,
		RecruitCost: 30, KillBounty: 24,
	},
	UnitMotorGun: {
		Type: UnitMotorGun, Weapon: WeaponGun, Armor: ArmorHeavy,
		Cost: 2, HP: 120, Damage: 15, Range: 5, Cooldown: 2,
		RecruitCost: 25, KillBounty: 20,
	},
	UnitMotorArtillery: {
		Type: UnitMotorArtillery, Weapon: WeaponCannon, Armor: ArmorHeavy,
		Cost: 4, HP: 150, Damage: 40, Range: 7, Cooldown: 5,
		RecruitCost: 50, KillBounty: 40,
	},
	UnitMotorMissile: {
		Type: UnitMotorMissile, Weapon: WeaponMissile, Armor: ArmorHeavy,
		Cost: 4, HP: 130, Damage: 50, Range: 9, Cooldown: 7,
		RecruitCost: 60, KillBounty: 48,
	},
}

// damageMatrix maps [WeaponType][ArmorType] to damage percentage (100 = 1.0x).
var damageMatrix = [4][3]int32{
	//              Light  Heavy  Building
	{100, 50, 0},   // Gun
	{50, 100, 25},  // Cannon
	{100, 25, 0},   // Sniper
	{25, 150, 25},  // Missile
}

// DamageMultiplier returns the damage percentage for a weapon vs armor combination.
// 100 = full damage, 50 = half, 150 = 1.5x, 0 = no damage.
func DamageMultiplier(weapon WeaponType, armor ArmorType) int32 {
	if int(weapon) >= len(damageMatrix) || int(armor) >= len(damageMatrix[0]) {
		return 0
	}
	return damageMatrix[weapon][armor]
}

// CanDamageTerrain returns true if the weapon type can damage terrain (Building armor).
// Only Cannon and Missile can damage terrain.
func CanDamageTerrain(weapon WeaponType) bool {
	return weapon == WeaponCannon || weapon == WeaponMissile
}

// UnitTypeComponent is the ECS component that stores a unit's type identity.
// Used by CombatSystem for damage matrix lookups and smart targeting.
type UnitTypeComponent struct {
	Type   CombatUnitType
	Weapon WeaponType
	Armor  ArmorType
	Level  uint8 // 1-6 for CombatUnits, 1-10 for Commanders
}

// CombatUnitTypeNames maps string names to CombatUnitType values.
// Used for roster JSON persistence.
var CombatUnitTypeNames = map[string]CombatUnitType{
	"LightInfantry":     UnitLightInfantry,
	"HeavyInfantry":     UnitHeavyInfantry,
	"Sniper":            UnitSniper,
	"AntiArmorInfantry": UnitAntiArmorInfantry,
	"MotorGun":          UnitMotorGun,
	"MotorArtillery":    UnitMotorArtillery,
	"MotorMissile":      UnitMotorMissile,
}

// CombatUnitTypeName maps CombatUnitType back to string.
func CombatUnitTypeName(t CombatUnitType) string {
	for name, val := range CombatUnitTypeNames {
		if val == t {
			return name
		}
	}
	return "LightInfantry"
}

// ParseCombatUnitType converts a string name to a CombatUnitType.
// Returns UnitLightInfantry and false if not found.
func ParseCombatUnitType(name string) (CombatUnitType, bool) {
	t, ok := CombatUnitTypeNames[name]
	if !ok {
		return UnitLightInfantry, false
	}
	return t, true
}
