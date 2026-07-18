package component

type OwnerComponent struct {
	PlayerID uint32
	Faction  uint8 // 0 = player, 1 = enemy, 0xFF = neutral
}

const (
	FactionPlayer  uint8 = 0
	FactionEnemy   uint8 = 1
	FactionNeutral uint8 = 0xFF // unclaimed — e.g. a Stronghold before capture (#54)
)
