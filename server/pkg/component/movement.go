package component

type TerrainType uint8

const (
	TerrainPlain   TerrainType = 0
	TerrainRoad    TerrainType = 1
	TerrainShallow TerrainType = 2
	TerrainDeep    TerrainType = 3
	TerrainForest  TerrainType = 4
	TerrainHill    TerrainType = 5
	TerrainSwamp   TerrainType = 6
	TerrainBridge  TerrainType = 7
	TerrainWall    TerrainType = 8
	TerrainSnow    TerrainType = 9
	TerrainDesert  TerrainType = 10
)

type MovementProfile struct {
	ID           uint8
	TerrainCosts [16]uint8
}

type MovementComponent struct {
	ProfileID uint8
}