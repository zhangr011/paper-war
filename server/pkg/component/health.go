package component

type HealthComponent struct {
	HP, MaxHP    int32
	Armor        int32
	Morale       int32
	LastAttacker uint32 // entity ID of the last attacker (for kill credit)
}

type AttackType uint8

const (
	AttackMelee     AttackType = 0
	AttackRanged    AttackType = 1
	AttackArtillery AttackType = 2
)

type AttackComponent struct {
	Range         int64
	Damage        int32
	Cooldown      uint8
	LastAttack    uint32
	TargetID      uint32
	AttackType    AttackType
	GroundTargetX int64 // set by CmdAttackGround
	GroundTargetY int64 // set by CmdAttackGround
}

type ProjectileComponent struct {
	X, Y         int64
	DX, DY       int64
	TargetX, TargetY int64
	Damage       int32
	ImpactTick   uint32
	SplashRadius int64
}