package component

type OwnerComponent struct {
	PlayerID  uint32
	Faction   uint8 // 0 = player, 1 = enemy
}

const (
	FactionPlayer uint8 = 0
	FactionEnemy  uint8 = 1
)
