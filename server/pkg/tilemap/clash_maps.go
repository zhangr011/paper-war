package tilemap

import (
	"math/rand"

	"github.com/user/paper-war/server/pkg/component"
)

// clashMap creates a 32×32 GameMap filled with Plains.
func clashMap() *GameMap {
	return NewGameMap(32, 32)
}

const cw int32 = 32
const ch int32 = 32
const cmidX int32 = 16
const cmidY int32 = 16

// setSym sets terrain at (x,y) and its horizontal mirror (w-1-x, y).
func setSym(m *GameMap, x, y int32, t component.TerrainType) {
	m.SetTerrain(x, y, t)
	m.SetTerrain(cw-1-x, y, t)
}

// setSymRect fills a horizontally-symmetric rectangle centered on cx.
func setSymRect(m *GameMap, cx, y, hw, h int32, t component.TerrainType) {
	for dy := int32(0); dy < h; dy++ {
		for dx := int32(-hw); dx <= hw; dx++ {
			m.SetTerrain(cx+dx, y+dy, t)
		}
	}
}

// placeWall sets a Wall tile and gives it destructible HP (Phase 3). Walls are
// breached by cannon/AoE splash via TerrainSystem.ProcessDestruction → Plain.
func placeWall(m *GameMap, x, y int32) {
	m.SetTerrain(x, y, component.TerrainWall)
	t := m.TileAt(x, y)
	if t != nil {
		t.Health = wallHealth
		t.MaxHealth = wallHealth
	}
}

// ---------------------------------------------------------------------------
// ClashPlains: Open field. Few scattered trees. Fast ranged engagements.
// v1.4: single central road connects the two bases. Scattered forests
// don't block the road.
// ---------------------------------------------------------------------------
func ClashPlains() *GameMap {
	m := clashMap()

	// Scattered tree clusters (symmetric pairs) — kept away from the
	// central road corridor (cmidX-1 .. cmidX+1) so there's exactly one
	// path between the bases.
	clusters := [][2]int32{
		{5, 6}, {8, 12}, {4, 20}, {10, 24},
		{12, 8}, {14, 22}, {6, 26},
	}
	for _, c := range clusters {
		setSym(m, c[0], c[1], component.TerrainForest)
		for _, dy := range []int32{-1, 0, 1} {
			for _, dx := range []int32{-1, 0, 1} {
				if dx == 0 && dy == 0 {
					continue
				}
				nx := c[0] + dx
				if nx >= cmidX-1 && nx <= cmidX+1 {
					continue
				}
				setSym(m, nx, c[1]+dy, component.TerrainForest)
			}
		}
	}

	// Light hills off-center
	setSymRect(m, cmidX-5, cmidY-2, 2, 4, component.TerrainHill)
	setSymRect(m, cmidX+5, cmidY-2, 2, 4, component.TerrainHill)

	// Single road from top spawn to bottom spawn (2 tiles wide).
	// This is the ONE path connecting the two bases.
	for y := int32(0); y < ch; y++ {
		m.SetTerrain(cmidX, y, component.TerrainRoad)
		m.SetTerrain(cmidX-1, y, component.TerrainRoad)
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashForest: Dense tree cover, narrow clearings. Melee/ambush advantage.
// v1.4: single vertical corridor connecting the two bases.
// ---------------------------------------------------------------------------
func ClashForest() *GameMap {
	m := clashMap()

	// Fill most of the map with forest
	for y := int32(0); y < ch; y++ {
		for x := int32(0); x < cw; x++ {
			m.SetTerrain(x, y, component.TerrainForest)
		}
	}

	// Carve a single vertical clearing through center.
	for y := int32(0); y < ch; y++ {
		for dx := int32(-2); dx <= 2; dx++ {
			m.SetTerrain(cmidX+dx, y, component.TerrainPlain)
		}
	}

	// Small clearings near spawns
	for dx := int32(-4); dx <= 4; dx++ {
		for dy := int32(-2); dy <= 2; dy++ {
			m.SetTerrain(cmidX+dx, 4+dy, component.TerrainPlain)
			m.SetTerrain(cmidX+dx, ch-5+dy, component.TerrainPlain)
		}
	}

	// Road along center vertical
	for y := int32(5); y < ch-5; y++ {
		m.SetTerrain(cmidX, y, component.TerrainRoad)
		m.SetTerrain(cmidX-1, y, component.TerrainRoad)
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashRoad: Central highway flanked by mixed terrain. Chokepoint control.
// v1.4: narrowed the road to 2 tiles — a single tight corridor.
// ---------------------------------------------------------------------------
func ClashRoad() *GameMap {
	m := clashMap()

	// Single central road (2 tiles wide).
	for y := int32(0); y < ch; y++ {
		for dx := int32(-1); dx <= 0; dx++ {
			m.SetTerrain(cmidX+dx, y, component.TerrainRoad)
		}
	}

	// Forest patches on both sides
	forestPatches := [][4]int32{ // {x, y, w, h}
		{2, 3, 4, 5},
		{1, 12, 4, 4},
		{2, 22, 3, 5},
	}
	for _, p := range forestPatches {
		for dy := int32(0); dy < p[3]; dy++ {
			for dx := int32(0); dx < p[2]; dx++ {
				m.SetTerrain(p[0]+dx, p[1]+dy, component.TerrainForest)
				m.SetTerrain(cw-1-p[0]-dx, p[1]+dy, component.TerrainForest)
			}
		}
	}

	// Hill bunkers flanking the road at 1/3 and 2/3 marks
	for _, my := range []int32{8, 24} {
		for dy := int32(-2); dy <= 2; dy++ {
			for dx := int32(-2); dx <= 2; dx++ {
				m.SetTerrain(cmidX-5+dx, my+dy, component.TerrainHill)
				m.SetTerrain(cmidX+5+dx, my+dy, component.TerrainHill)
			}
		}
	}

	// Shallow ponds along the road edges
	for _, py := range []int32{5, 14, 20, 28} {
		for dy := int32(0); dy < 2; dy++ {
			for dx := int32(0); dx < 2; dx++ {
				m.SetTerrain(cmidX-4+dx, py+dy, component.TerrainShallow)
				m.SetTerrain(cmidX+3+dx, py+dy, component.TerrainShallow)
			}
		}
	}

	// Wall fortifications at center crossroads
	for _, wy := range []int32{cmidY - 1, cmidY + 1} {
		for dx := int32(-4); dx <= -3; dx++ {
			placeWall(m, cmidX+dx, wy)
			placeWall(m, cmidX-dx-1, wy)
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashRiver: River bisects the map. ONE bridge crossing. Bridge control.
// ---------------------------------------------------------------------------
func ClashRiver() *GameMap {
	m := clashMap()

	// River running horizontally across the middle (y=13..19)
	riverTop := int32(13)
	riverBot := int32(19)
	for y := riverTop; y <= riverBot; y++ {
		for x := int32(0); x < cw; x++ {
			if y == riverTop || y == riverBot {
				m.SetTerrain(x, y, component.TerrainShallow)
			} else {
				m.SetTerrain(x, y, component.TerrainDeep)
			}
		}
	}

	// 1 central bridge only — the ONE path connecting the two bases.
	bx := cmidX
	for y := riverTop - 1; y <= riverBot+1; y++ {
		for dx := int32(-2); dx <= 2; dx++ {
			m.SetTerrain(bx+dx, y, component.TerrainBridge)
		}
	}
	for y := int32(0); y < ch; y++ {
		if y >= riverTop-1 && y <= riverBot+1 {
			continue
		}
		for dx := int32(-1); dx <= 1; dx++ {
			m.SetTerrain(bx+dx, y, component.TerrainRoad)
		}
	}

	// Forest patches on each side of river (away from the road)
	forests := [][3]int32{ // x, y, size
		{3, 5, 3}, {10, 7, 2}, {13, 3, 3},
		{3, 24, 3}, {10, 24, 2}, {13, 27, 3},
	}
	for _, f := range forests {
		for dy := int32(0); dy < f[2]; dy++ {
			for dx := int32(0); dx < f[2]; dx++ {
				setSym(m, f[0]+dx, f[1]+dy, component.TerrainForest)
			}
		}
	}

	// Hills overlooking the single bridge
	for dy := int32(-1); dy <= 1; dy++ {
		for dx := int32(-1); dx <= 1; dx++ {
			m.SetTerrain(bx+dx, riverTop-3+dy, component.TerrainHill)
			m.SetTerrain(bx+dx, riverBot+3+dy, component.TerrainHill)
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashStronghold: Central fortress with walls and gates. Siege warfare.
// v1.4: smaller fortress ring for the 32×32 map. Only the N-S road.
// ---------------------------------------------------------------------------
func ClashStronghold() *GameMap {
	m := clashMap()

	// Outer wall ring (centered on map center) — smaller for 32×32
	wallR := int32(6)
	gateWidth := int32(2)

	// Build wall ring (Chebyshev distance == wallR)
	for dy := -wallR; dy <= wallR; dy++ {
		for dx := -wallR; dx <= wallR; dx++ {
			dist := dx
			if dy < 0 {
				if -dy > dist {
					dist = -dy
				}
			} else {
				if dy > dist {
					dist = dy
				}
			}
			if dx < 0 {
				if -dx > dist {
					dist = -dx
				}
			} else {
				if dx > dist {
					dist = dx
				}
			}
			if dist == wallR {
				placeWall(m, cmidX+dx, cmidY+dy)
			}
		}
	}

	// Carve gates (N, S)
	for dx := int32(-gateWidth); dx <= gateWidth; dx++ {
		m.SetTerrain(cmidX+dx, cmidY-wallR, component.TerrainRoad)
		m.SetTerrain(cmidX+dx, cmidY+wallR, component.TerrainRoad)
	}

	// Central fortress keep: strongholds are no longer terrain (ADR-0023 /
	// issue #54 — they're entities), so the inner keep is a solid Wall block
	// ringed by the wall above. The N-S road carves the gate below.
	for dy := int32(-2); dy <= 2; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			placeWall(m, cmidX+dx, cmidY+dy)
		}
	}

	// Only the N-S road connects the two bases (v1.4: E-W removed)
	for y := int32(0); y < ch; y++ {
		m.SetTerrain(cmidX, y, component.TerrainRoad)
		m.SetTerrain(cmidX-1, y, component.TerrainRoad)
	}

	// Forest patches outside walls
	outerForests := [][2]int32{
		{3, 4}, {12, 3}, {3, 9}, {12, 10},
		{3, 24}, {12, 25}, {3, 28}, {12, 27},
	}
	for _, f := range outerForests {
		for dy := int32(-1); dy <= 1; dy++ {
			for dx := int32(-1); dx <= 1; dx++ {
				setSym(m, f[0]+dx, f[1]+dy, component.TerrainForest)
			}
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashHills: Rolling hills with valleys. Ranged units on high ground.
// v1.4: 2 ridges with 1 central pass each.
// ---------------------------------------------------------------------------
func ClashHills() *GameMap {
	m := clashMap()

	// 2 hill ridges with single central passes
	ridges := [][2]int32{{6, 9}, {22, 25}}
	for _, r := range ridges {
		for y := r[0]; y <= r[1]; y++ {
			for x := int32(0); x < cw; x++ {
				m.SetTerrain(x, y, component.TerrainHill)
			}
		}
		// Single central gap
		for y := r[0]; y <= r[1]; y++ {
			for dx := int32(-2); dx <= 2; dx++ {
				m.SetTerrain(cmidX+dx, y, component.TerrainPlain)
			}
		}
	}

	// Valleys between ridges: some swamp
	for _, sy := range []int32{14, 15} {
		for _, sx := range []int32{3, 8, 12, 20, 24, 28} {
			for dy := int32(0); dy < 2; dy++ {
				for dx := int32(0); dx < 2; dx++ {
					if sx+dx < cw {
						m.SetTerrain(sx+dx, sy+dy, component.TerrainSwamp)
					}
				}
			}
		}
	}

	// Forest patches in valleys
	valleyForests := [][2]int32{
		{3, 14}, {10, 14}, {20, 15}, {27, 14},
	}
	for _, f := range valleyForests {
		for dy := int32(0); dy < 2; dy++ {
			for dx := int32(0); dx < 2; dx++ {
				if f[0]+dx < cw {
					m.SetTerrain(f[0]+dx, f[1]+dy, component.TerrainForest)
				}
			}
		}
	}

	// Road through the single central pass
	for y := int32(0); y < ch; y++ {
		for dx := int32(-1); dx <= 1; dx++ {
			px := cmidX + dx
			if px >= 0 && px < cw {
				t := m.TileAt(px, y)
				if t != nil && t.TerrainType == component.TerrainHill {
					m.SetTerrain(px, y, component.TerrainRoad)
				}
			}
		}
	}

	// Ramps up the ridge faces flanking the central pass (Phase 1,
	// terrain-starcraft-plan.md §1). The pass (cols cmidX-2..cmidX+2) stays
	// at elevation 0 and remains the primary N-S route; these Ramp tiles
	// replace the slope-Hill ring at the pass edge with a deliberate 2-tier
	// cliff that only a Ramp permits crossing, giving units an authored
	// high-ground route up to the peaks. Elevation 2 is set explicitly here —
	// Ramp is not Hill, so DeriveElevation (run in LoadClashMap) leaves it
	// untouched, preserving the cliff. Placed on both ridges (rows 7-8 and
	// 23-24 = the peak rows) at cols cmidX-3 (13) and cmidX+3 (19), the ridge
	// tiles immediately flanking the pass gap on each side.
	rampRows := []int32{7, 8, 23, 24}
	rampCols := []int32{cmidX - 3, cmidX + 3}
	for _, ry := range rampRows {
		for _, rx := range rampCols {
			m.SetTerrain(rx, ry, component.TerrainRamp)
			if t := m.TileAt(rx, ry); t != nil {
				t.Elevation = 2
			}
		}
	}

	return m
}

// LoadClashMap returns a pre-designed clash map by name.
// Returns nil if the name is not recognized.
func LoadClashMap(name string) *GameMap {
	var m *GameMap
	switch name {
	case "plains":
		m = ClashPlains()
	case "forest":
		m = ClashForest()
	case "road":
		m = ClashRoad()
	case "river":
		m = ClashRiver()
	case "stronghold":
		m = ClashStronghold()
	case "hills":
		m = ClashHills()
	case "random":
		maps := []func()*GameMap{
			ClashPlains, ClashForest, ClashRoad,
			ClashRiver, ClashStronghold, ClashHills,
		}
		m = maps[rand.Intn(len(maps))]()
	default:
		return nil
	}
	// Clash maps author Hill tiles without Elevation; derive layer (peak/slope)
	// from hill topology so the elevation-aware hill shader renders correctly.
	// Procedural GenerateMap is NOT routed through this — its heightmap-based
	// assignment (generate.go:108-122) is more accurate than topology.
	if m != nil {
		DeriveElevation(m)
	}
	return m
}
