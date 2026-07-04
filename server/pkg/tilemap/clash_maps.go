package tilemap

import (
	"math/rand"

	"github.com/user/paper-war/server/pkg/component"
)

// clashMap creates a 48×96 GameMap filled with Plains.
func clashMap() *GameMap {
	return NewGameMap(48, 96)
}

const cw int32 = 48
const ch int32 = 96
const cmidX int32 = 24
const cmidY int32 = 48

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

// ---------------------------------------------------------------------------
// ClashPlains: Open field. Few scattered trees. Fast ranged engagements.
// v1.4: single central road connects the two bases (top-center ↔ bottom-
// center). Scattered forests don't block the road.
// ---------------------------------------------------------------------------
func ClashPlains() *GameMap {
	m := clashMap()

	// Scattered tree clusters (symmetric pairs) — kept away from the
	// central road corridor (cmidX-1 .. cmidX+1) so there's exactly one
	// path between the bases.
	clusters := [][2]int32{
		{8, 20}, {12, 35}, {6, 50}, {15, 65}, {10, 80},
		{18, 25}, {20, 55}, {14, 75},
	}
	for _, c := range clusters {
		setSym(m, c[0], c[1], component.TerrainForest)
		for _, dy := range []int32{-1, 0, 1} {
			for _, dx := range []int32{-1, 0, 1} {
				if dx == 0 && dy == 0 {
					continue
				}
				// Don't place forest on the road corridor
				nx := c[0] + dx
				if nx >= cmidX-1 && nx <= cmidX+1 {
					continue
				}
				setSym(m, nx, c[1]+dy, component.TerrainForest)
			}
		}
	}

	// Light hills in center (off-road)
	setSymRect(m, cmidX-6, cmidY-3, 3, 6, component.TerrainHill)
	setSymRect(m, cmidX+6, cmidY-3, 3, 6, component.TerrainHill)

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
	// v1.4: removed horizontal + diagonal clearings — there is now ONE
	// path (the central corridor) connecting the two bases. The rest
	// of the map is dense forest.
	for y := int32(0); y < ch; y++ {
		for dx := int32(-3); dx <= 3; dx++ {
			m.SetTerrain(cmidX+dx, y, component.TerrainPlain)
		}
	}

	// Small clearings near spawns
	for dx := int32(-6); dx <= 6; dx++ {
		for dy := int32(-3); dy <= 3; dy++ {
			m.SetTerrain(cmidX+dx, 8+dy, component.TerrainPlain)
			m.SetTerrain(cmidX+dx, ch-9+dy, component.TerrainPlain)
		}
	}

	// Road along center vertical
	for y := int32(10); y < ch-10; y++ {
		m.SetTerrain(cmidX, y, component.TerrainRoad)
		m.SetTerrain(cmidX-1, y, component.TerrainRoad)
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashRoad: Central highway flanked by mixed terrain. Chokepoint control.
// v1.4: narrowed the road from 5 tiles to 2 — a single tight corridor
// connecting the two bases. Forests and terrain on both sides are
// impassable barriers (not alternate routes).
// ---------------------------------------------------------------------------
func ClashRoad() *GameMap {
	m := clashMap()

	// Single central road (2 tiles wide, down from 5).
	// This is the ONE path connecting the two bases.
	for y := int32(0); y < ch; y++ {
		for dx := int32(-1); dx <= 0; dx++ {
			m.SetTerrain(cmidX+dx, y, component.TerrainRoad)
		}
	}

	// Forest patches on both sides
	// Left side forests
	forestPatches := [][4]int32{ // {x, y, w, h}
		{3, 10, 5, 8},
		{2, 30, 6, 6},
		{4, 55, 5, 7},
		{3, 75, 4, 8},
	}
	for _, p := range forestPatches {
		for dy := int32(0); dy < p[3]; dy++ {
			for dx := int32(0); dx < p[2]; dx++ {
				// Left side
				m.SetTerrain(p[0]+dx, p[1]+dy, component.TerrainForest)
				// Right side (mirror)
				m.SetTerrain(cw-1-p[0]-dx, p[1]+dy, component.TerrainForest)
			}
		}
	}

	// Hill bunkers flanking the road at 1/4 and 3/4 marks
	for _, my := range []int32{24, 72} {
		// Left bunker
		for dy := int32(-3); dy <= 3; dy++ {
			for dx := int32(-3); dx <= 3; dx++ {
				m.SetTerrain(cmidX-8+dx, my+dy, component.TerrainHill)
				m.SetTerrain(cmidX+8+dx, my+dy, component.TerrainHill)
			}
		}
	}

	// Shallow ponds along the road edges
	for _, py := range []int32{15, 40, 60, 85} {
		for dy := int32(0); dy < 3; dy++ {
			for dx := int32(0); dx < 3; dx++ {
				m.SetTerrain(cmidX-7+dx, py+dy, component.TerrainShallow)
				m.SetTerrain(cmidX+5+dx, py+dy, component.TerrainShallow)
			}
		}
	}

	// Wall fortifications at center crossroads
	for _, wy := range []int32{cmidY - 2, cmidY + 2} {
		for dx := int32(-6); dx <= -4; dx++ {
			m.SetTerrain(cmidX+dx, wy, component.TerrainWall)
			m.SetTerrain(cmidX-dx-1, wy, component.TerrainWall)
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashRiver: River bisects the map. ONE bridge crossing. Bridge control.
// v1.4: reduced from 3 bridges to 1 — the single bridge is the only path
// between the two bases. Whichever team controls the bridge wins.
// ---------------------------------------------------------------------------
func ClashRiver() *GameMap {
	m := clashMap()

	// River running horizontally across the middle (y=44..52)
	riverTop := int32(44)
	riverBot := int32(52)
	for y := riverTop; y <= riverBot; y++ {
		for x := int32(0); x < cw; x++ {
			if y == riverTop || y == riverBot {
				m.SetTerrain(x, y, component.TerrainShallow)
			} else {
				m.SetTerrain(x, y, component.TerrainDeep)
			}
		}
	}

	// 1 central bridge only (was 3: left, center, right).
	// This is the ONE path connecting the two bases.
	bx := cmidX
	for y := riverTop - 1; y <= riverBot+1; y++ {
		for dx := int32(-2); dx <= 2; dx++ {
			m.SetTerrain(bx+dx, y, component.TerrainBridge)
		}
	}
	// Road approach to bridge (single corridor from each base)
	for y := int32(0); y < ch; y++ {
		if y >= riverTop-1 && y <= riverBot+1 {
			continue // bridge tiles already set
		}
		for dx := int32(-1); dx <= 1; dx++ {
			m.SetTerrain(bx+dx, y, component.TerrainRoad)
		}
	}

	// Forest patches on each side of river (away from the road)
	forests := [][3]int32{ // x, y, size
		{4, 15, 4}, {15, 20, 3}, {20, 10, 5},
		{4, 70, 4}, {15, 75, 3}, {20, 80, 5},
	}
	for _, f := range forests {
		for dy := int32(0); dy < f[2]; dy++ {
			for dx := int32(0); dx < f[2]; dx++ {
				setSym(m, f[0]+dx, f[1]+dy, component.TerrainForest)
			}
		}
	}

	// Hills overlooking the single bridge
	for dy := int32(-2); dy <= 2; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			m.SetTerrain(bx+dx, riverTop-5+dy, component.TerrainHill)
			m.SetTerrain(bx+dx, riverBot+5+dy, component.TerrainHill)
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashStronghold: Central fortress with walls and gates. Siege warfare.
// ---------------------------------------------------------------------------
func ClashStronghold() *GameMap {
	m := clashMap()

	// Outer wall ring (16×16 centered on map center)
	wallR := int32(10)
	gateWidth := int32(3) // 3-tile wide gates

	// Build wall ring
	for dy := int32(-wallR); dy <= wallR; dy++ {
		for dx := int32(-wallR); dx <= wallR; dx++ {
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
			// Only outer ring (Chebyshev distance == wallR)
			if dist == wallR {
				m.SetTerrain(cmidX+dx, cmidY+dy, component.TerrainWall)
			}
		}
	}

	// Carve gates (N, S, E, W)
	for dx := int32(-gateWidth); dx <= gateWidth; dx++ {
		// North gate
		m.SetTerrain(cmidX+dx, cmidY-wallR, component.TerrainRoad)
		// South gate
		m.SetTerrain(cmidX+dx, cmidY+wallR, component.TerrainRoad)
	}
	for dy := int32(-gateWidth); dy <= gateWidth; dy++ {
		// West gate
		m.SetTerrain(cmidX-wallR, cmidY+dy, component.TerrainRoad)
		// East gate
		m.SetTerrain(cmidX+wallR, cmidY+dy, component.TerrainRoad)
	}

	// Inner stronghold tile at center
	for dy := int32(-3); dy <= 3; dy++ {
		for dx := int32(-3); dx <= 3; dx++ {
			m.SetTerrain(cmidX+dx, cmidY+dy, component.TerrainStronghold3)
		}
	}
	// Stronghold inner ring
	for dy := int32(-5); dy <= 5; dy++ {
		for dx := int32(-5); dx <= 5; dx++ {
			t := m.TileAt(cmidX+dx, cmidY+dy)
			if t != nil && t.TerrainType == component.TerrainPlain {
				m.SetTerrain(cmidX+dx, cmidY+dy, component.TerrainStronghold1)
			}
		}
	}

	// Roads from gates outward to map edges.
	// v1.4: only the N-S road connects the two bases. The E-W cross-
	// road is removed — it was an alternate path.
	for y := int32(0); y < ch; y++ {
		m.SetTerrain(cmidX, y, component.TerrainRoad)
		m.SetTerrain(cmidX-1, y, component.TerrainRoad)
	}

	// Forest patches outside walls
	outerForests := [][2]int32{
		{5, 10}, {18, 8}, {5, 25}, {18, 30},
		{5, 70}, {18, 72}, {5, 85}, {18, 80},
	}
	for _, f := range outerForests {
		for dy := int32(-2); dy <= 2; dy++ {
			for dx := int32(-2); dx <= 2; dx++ {
				setSym(m, f[0]+dx, f[1]+dy, component.TerrainForest)
			}
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// ClashHills: Rolling hills with valleys. Ranged units on high ground.
// ---------------------------------------------------------------------------
func ClashHills() *GameMap {
	m := clashMap()

	// Hill ridges running horizontally across the map
	// Ridge 1: y=20-26
	// Ridge 2: y=44-52 (center)
	// Ridge 3: y=70-76
	ridges := [][2]int32{{20, 26}, {44, 52}, {70, 76}}
	for _, r := range ridges {
		for y := r[0]; y <= r[1]; y++ {
			for x := int32(0); x < cw; x++ {
				// Main ridge = hill
				if y == r[0] || y == r[1] {
					m.SetTerrain(x, y, component.TerrainHill)
				} else {
					m.SetTerrain(x, y, component.TerrainHill)
				}
			}
		}
		// Create a single gap (pass) in each ridge — centered only.
		// v1.4: was 3 passes per ridge (left, center, right); now 1.
		// The central pass is the ONLY path between the two bases.
		gapX := cmidX
		for y := r[0]; y <= r[1]; y++ {
			for dx := int32(-2); dx <= 2; dx++ {
				m.SetTerrain(gapX+dx, y, component.TerrainPlain)
			}
		}
	}

	// Valleys between ridges: some swamp (low ground)
	for _, sy := range []int32{33, 34, 61, 62} {
		for _, sx := range []int32{4, 10, 16, 22, 28, 34, 40} {
			for dy := int32(0); dy < 3; dy++ {
				for dx := int32(0); dx < 3; dx++ {
					if sx+dx < cw {
						m.SetTerrain(sx+dx, sy+dy, component.TerrainSwamp)
					}
				}
			}
		}
	}

	// Forest patches in valleys
	valleyForests := [][2]int32{
		{5, 33}, {15, 35}, {30, 34}, {40, 36},
		{5, 61}, {15, 63}, {30, 62}, {40, 64},
	}
	for _, f := range valleyForests {
		for dy := int32(0); dy < 3; dy++ {
			for dx := int32(0); dx < 3; dx++ {
				if f[0]+dx < cw {
					m.SetTerrain(f[0]+dx, f[1]+dy, component.TerrainForest)
				}
			}
		}
	}

	// Roads through the single central pass
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

	return m
}

// LoadClashMap returns a pre-designed clash map by name.
// Returns nil if the name is not recognized.
func LoadClashMap(name string) *GameMap {
	switch name {
	case "plains":
		return ClashPlains()
	case "forest":
		return ClashForest()
	case "road":
		return ClashRoad()
	case "river":
		return ClashRiver()
	case "stronghold":
		return ClashStronghold()
	case "hills":
		return ClashHills()
	case "random":
		maps := []func()*GameMap{
			ClashPlains, ClashForest, ClashRoad,
			ClashRiver, ClashStronghold, ClashHills,
		}
		return maps[rand.Intn(len(maps))]()
	default:
		return nil
	}
}
