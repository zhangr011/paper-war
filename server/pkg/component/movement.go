package component

type TerrainType uint8

const (
	TerrainPlain       TerrainType = 0
	TerrainRoad        TerrainType = 1
	TerrainShallow     TerrainType = 2
	TerrainDeep        TerrainType = 3
	TerrainForest      TerrainType = 4
	TerrainHill        TerrainType = 5
	TerrainSwamp       TerrainType = 6
	TerrainBridge      TerrainType = 7
	TerrainWall        TerrainType = 8
	TerrainSnow        TerrainType = 9
	TerrainDesert      TerrainType = 10
	TerrainStronghold1 TerrainType = 11
	TerrainStronghold2 TerrainType = 12
	TerrainStronghold3 TerrainType = 13
	TerrainStronghold4 TerrainType = 14
	TerrainStronghold5 TerrainType = 15
)

// BlocksLOS reports whether a tile of this terrain blocks line-of-sight
// through it. Used by the fog system's vision raycasting (issue #55 phase 2).
// The blocker tile itself remains visible — only sight past it is blocked,
// so you see the wall/forest edge but not what is behind it.
func BlocksLOS(t TerrainType) bool {
	switch t {
	case TerrainForest, TerrainWall:
		return true
	default:
		return false
	}
}

type MovementProfile struct {
	ID           uint8
	TerrainCosts [16]uint8
}

type MovementComponent struct {
	ProfileID uint8
}
