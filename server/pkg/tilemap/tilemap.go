package tilemap

import "github.com/user/paper-war/server/pkg/component"

type Tile struct {
	TerrainType component.TerrainType
	// Elevation is a discrete hill-layer band, authored on ANY terrain
	// (ADR-0033): a Δ2 step between adjacent tiles is a cliff (impassable
	// unless a Ramp sits on either end — EdgeWalkableFor), regardless of the
	// tiles' terrain types. Values: 0 = low, 1 = mid, 2 = peak. Issue #49.
	Elevation uint8
	BlockLOS  bool
	Health    int32
	MaxHealth int32
	// CreepOwner is the faction index (1 or 2) of the spreading "creep"
	// terrain that owns this tile, or 0 when unclaimed. Creep is an OVERLAY —
	// it does not replace TerrainType. A ground unit whose creep-faction
	// matches CreepOwner gets a movement discount on this tile
	// (Phase 4, terrain-starcraft-plan.md §4). 0 = none, 1 = faction 1
	// (FactionPlayer), 2 = faction 2 (FactionEnemy).
	CreepOwner uint8
}

type ObjectiveType uint8

const (
	ObjectiveElimination ObjectiveType = 0
	ObjectiveCapture    ObjectiveType = 1
	ObjectiveSurvival   ObjectiveType = 2
)

type Objective struct {
	Type        ObjectiveType
	TargetX     int32 // Capture: stronghold tile X
	TargetY     int32 // Capture: stronghold tile Y
	HoldTarget  int32 // Capture: ticks needed to hold (300)
	HoldCounter int32 // Capture: current progress
	Duration    int32 // Survival: total ticks
}

// StrongholdSpec is a generator-recorded stronghold placement: a tile position
// plus its level. The session spawns a Stronghold entity for each spec at
// match start (ADR-0023 / issue #54) — strongholds are no longer terrain.
type StrongholdSpec struct {
	X, Y  int32
	Level uint8 // 1-5
}

type GameMap struct {
	Width, Height int32
	Tiles         []Tile
	Objective     Objective
	Spawns        [][2]int32      // generator-placed spawn positions
	Strongholds   []StrongholdSpec // generator-recorded stronghold placements (#54)
	Seed          int64           // seed for debugging/reproducibility
}

func NewGameMap(w, h int32) *GameMap {
	tiles := make([]Tile, w*h)
	for i := range tiles {
		tiles[i] = Tile{TerrainType: component.TerrainPlain}
	}
	return &GameMap{Width: w, Height: h, Tiles: tiles}
}

func NewTestMap(w, h int32, fn func(x, y int32) component.TerrainType) *GameMap {
	gm := NewGameMap(w, h)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			gm.SetTerrain(x, y, fn(x, y))
		}
	}
	return gm
}

func (m *GameMap) index(x, y int32) int {
	return int(y*m.Width + x)
}

func (m *GameMap) inBounds(x, y int32) bool {
	return x >= 0 && x < m.Width && y >= 0 && y < m.Height
}

func (m *GameMap) TileAt(x, y int32) *Tile {
	if !m.inBounds(x, y) {
		return nil
	}
	return &m.Tiles[m.index(x, y)]
}

func (m *GameMap) SetTerrain(x, y int32, tt component.TerrainType) {
	if !m.inBounds(x, y) {
		return
	}
	t := &m.Tiles[m.index(x, y)]
	t.TerrainType = tt
	// Keep BlockLOS in sync with terrain so every placement (generator, clash
	// maps, editor-pasted maps) is correct without each site re-deriving it.
	// Issue #55 phase 2 — retires BlockLOS as a dead field.
	t.BlockLOS = component.BlocksLOS(tt)
}

func (m *GameMap) CostAt(x, y int32, profile *component.MovementProfile) uint8 {
	return m.CostAtFor(x, y, profile, 0)
}

// CostAtFor is the faction-aware movement cost of tile (x,y) under profile.
// creepFaction is the moving unit's creep-faction index (1 or 2; 0 = neutral
// observer used by validators/tests). When creepFaction != 0 and equals the
// tile's CreepOwner, the friendly-creep movement discount (×0.7) is applied
// so friendly creep acts as a speed/routing bonus (Phase 4,
// terrain-starcraft-plan.md §4). The discount is floored at 1 so a tile never
// becomes free. Callers that only care about pure walkability (connectivity,
// map validation) pass creepFaction=0 and get the undiscounted cost.
func (m *GameMap) CostAtFor(x, y int32, profile *component.MovementProfile, creepFaction uint8) uint8 {
	if !m.inBounds(x, y) {
		return 0
	}
	t := &m.Tiles[m.index(x, y)]
	cost := profile.TerrainCosts[t.TerrainType]
	if creepFaction != 0 && t.CreepOwner == creepFaction && cost > 0 {
		// ×0.7 friendly-creep discount (floor 1 so the tile is never free).
		discounted := uint32(float64(cost) * 0.7)
		if discounted < 1 {
			discounted = 1
		}
		cost = uint8(discounted)
	}
	return cost
}

// EdgeWalkable reports whether a ground unit with the given profile may step
// from (x1,y1) to the adjacent tile (x2,y2), and at what cost. It is the
// neutral-observer variant (no faction discount); callers that need the
// friendly-creep discount should use EdgeWalkableFor. Phase 1
// (terrain-starcraft-plan.md §1). Used by validators/tests.
func (m *GameMap) EdgeWalkable(x1, y1, x2, y2 int32, profile *component.MovementProfile) (walkable bool, cost uint8) {
	return m.EdgeWalkableFor(x1, y1, x2, y2, profile, 0)
}

// EdgeWalkableFor is the faction-aware edge walkability + cost helper. It
// combines the destination tile's CostAtFor (so friendly creep applies its
// ×0.7 discount) with the elevation-cliff rule. creepFaction is the moving
// unit's creep-faction index (1/2; 0 = neutral). Used by pathfinding.Compute
// (Phase 4, terrain-starcraft-plan.md §4).
//
//	|Δelevation| ≥ 2 is a cliff and impassable UNLESS one of the two tiles is
//	TerrainRamp. |Δ| ≤ 1 is always walkable (subject to terrain cost).
//
// Callers must pass adjacent coordinates (4- or 8-neighbor); this function
// does not range-check adjacency.
func (m *GameMap) EdgeWalkableFor(x1, y1, x2, y2 int32, profile *component.MovementProfile, creepFaction uint8) (walkable bool, cost uint8) {
	cost = m.CostAtFor(x2, y2, profile, creepFaction)
	if cost == 0 {
		return false, 0
	}
	t1 := m.TileAt(x1, y1)
	t2 := m.TileAt(x2, y2)
	if t1 == nil || t2 == nil {
		return false, 0
	}
	delta := int(t1.Elevation) - int(t2.Elevation)
	if delta < 0 {
		delta = -delta
	}
	if delta >= 2 {
		// Cliff: only a Ramp tile (on either end) permits the crossing.
		if t1.TerrainType != component.TerrainRamp && t2.TerrainType != component.TerrainRamp {
			return false, 0
		}
	}
	return true, cost
}