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
	TerrainDesert TerrainType = 10
	// ids 11-15 are RESERVED (retired TerrainStronghold1-5 — strongholds are
	// now Building entities, ADR-0023 / issue #54). Not reused to keep
	// Rock/Brush (16/17) stable and TerrainCosts indices unchanged.
	TerrainRock  TerrainType = 16 // heavy cover, blocks LOS, Heavy-impassable crags
	TerrainBrush TerrainType = 17 // light cover, no LOS block (concealment only)
	// TerrainRamp permits a ground step across a 2-tier elevation cliff
	// (|Δelevation| ≥ 2). Cheap walkable terrain (cost 1 for both profiles)
	// that does NOT block LOS and does NOT conceal — purely a pathfinding
	// edge rule. See tilemap.EdgeWalkable. Phase 1 (terrain-starcraft-plan).
	TerrainRamp TerrainType = 18
)

// BlocksLOS reports whether a tile of this terrain blocks line-of-sight
// through it. Used by the fog system's vision raycasting (issue #55 phase 2).
// The blocker tile itself remains visible — only sight past it is blocked,
// so you see the wall/forest/rock edge but not what is behind it. Brush does
// NOT block — it's concealment (cover) only.
func BlocksLOS(t TerrainType) bool {
	switch t {
	case TerrainForest, TerrainWall, TerrainRock:
		return true
	default:
		return false
	}
}

// Conceals reports whether a tile of this terrain hides a unit standing in it
// from distant viewers (soft cover / ambush terrain). Forest and Brush are
// concealment: a unit inside is hidden from enemies beyond the detection
// radius unless it fires (giving away its position). Contrast BlocksLOS —
// Rock/Wall are hard LOS blockers handled by the fog raycast, so they are NOT
// concealment here (no unit stands on a Wall, and Rock is Heavy-impassable).
// ADR-0029.
func Conceals(t TerrainType) bool {
	switch t {
	case TerrainForest, TerrainBrush:
		return true
	default:
		return false
	}
}

type MovementProfile struct {
	ID           uint8
	TerrainCosts [20]uint8
}

type MovementComponent struct {
	ProfileID uint8
}
