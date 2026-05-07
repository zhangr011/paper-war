package tilemap

import (
	"math/rand"

	"github.com/user/paper-war/server/pkg/component"
)

// GenerateMap creates a symmetric natural terrain map for portrait play.
// Map features: horizontal river with bridges, vertical roads, forests, hills,
// open plains. The map is horizontally symmetric (mirrored left-right) for
// fairness while the battlefield advances along the vertical axis.
func GenerateMap(w, h int32, seed int64) *GameMap {
	gm := NewGameMap(w, h)
	r := rand.New(rand.NewSource(seed))

	midX := w / 2
	midY := h / 2

	// Phase 1: River (horizontal, winding across the center)
	riverY := midY
	for x := int32(0); x < w; x++ {
		// Winding river
		riverY += int32(r.Intn(3) - 1)
		if riverY < midY-3 {
			riverY = midY - 3
		}
		if riverY > midY+3 {
			riverY = midY + 3
		}
		// River width = 2-3 tiles
		width := 2 + r.Intn(2)
		for dy := int32(0); dy < int32(width); dy++ {
			gm.SetTerrain(x, riverY+dy, component.TerrainDeep)
			// Shallow banks on edges
			if gm.TileAt(x, riverY-1) != nil && gm.TileAt(x, riverY-1).TerrainType == component.TerrainPlain {
				gm.SetTerrain(x, riverY-1, component.TerrainShallow)
			}
			bottomEdge := riverY + int32(width)
			if gm.TileAt(x, bottomEdge) != nil && gm.TileAt(x, bottomEdge).TerrainType == component.TerrainPlain {
				gm.SetTerrain(x, bottomEdge, component.TerrainShallow)
			}
		}
	}

	// Phase 2: Bridges with vertical roads (north-south crossings)
	bridgeCount := 2 + r.Intn(2)
	bridgeSpacing := w / int32(bridgeCount+1)
	for i := int32(0); i < int32(bridgeCount); i++ {
		bx := bridgeSpacing*(i+1) + int32(r.Intn(5)-2)
		if bx < 1 || bx >= w-1 {
			bx = bridgeSpacing * (i + 1)
		}
		// Find the river at this x and place bridge
		for y := int32(0); y < h; y++ {
			tile := gm.TileAt(bx, y)
			if tile != nil && tile.TerrainType == component.TerrainDeep {
				gm.SetTerrain(bx, y, component.TerrainBridge)
				tile = gm.TileAt(bx, y)
				tile.Health = 500
				tile.MaxHealth = 500
			}
		}
		// Roads leading north-south to bridges
		for y := int32(0); y < h; y++ {
			tile := gm.TileAt(bx, y)
			if tile != nil && tile.TerrainType == component.TerrainPlain {
				gm.SetTerrain(bx, y, component.TerrainRoad)
			}
			if tile != nil && tile.TerrainType == component.TerrainShallow {
				gm.SetTerrain(bx, y, component.TerrainRoad)
			}
		}
	}

	// Phase 3: Forests (clusters of trees on both sides)
	for i := 0; i < 12; i++ {
		fx := int32(r.Intn(int(midX - 4)))
		fy := int32(r.Intn(int(h)))
		size := 3 + r.Intn(5)
		placeCluster(gm, fx, fy, size, component.TerrainForest, r)
		// Mirror
		mirrorX := w - 1 - fx
		placeCluster(gm, mirrorX, fy, size, component.TerrainForest, r)
	}

	// Phase 4: Hills (elevated areas)
	for i := 0; i < 6; i++ {
		hx := int32(r.Intn(int(midX - 6)))
		hy := int32(r.Intn(int(h)))
		size := 4 + r.Intn(6)
		placeCluster(gm, hx, hy, size, component.TerrainHill, r)
		mirrorX := w - 1 - hx
		placeCluster(gm, mirrorX, hy, size, component.TerrainHill, r)
	}

	// Phase 5: Spawn areas (clear plains for player starts)
	clearArea(gm, 2, 2, 12, 12)       // top-left (player 1)
	clearArea(gm, 2, h-14, 12, 12)    // bottom-left
	clearArea(gm, w-14, 2, 12, 12)    // top-right (player 2)
	clearArea(gm, w-14, h-14, 12, 12) // bottom-right

	// Phase 6: A few scattered swamp patches
	for i := 0; i < 4; i++ {
		sx := int32(r.Intn(int(midX - 3)))
		sy := int32(r.Intn(int(h)))
		size := 2 + r.Intn(3)
		placeCluster(gm, sx, sy, size, component.TerrainSwamp, r)
		mirrorX := w - 1 - sx
		placeCluster(gm, mirrorX, sy, size, component.TerrainSwamp, r)
	}

	// Phase 7: Destructible walls (strategic chokepoints)
	for i := 0; i < 3; i++ {
		wx := int32(r.Intn(int(midX-8)) + 4)
		wy := int32(r.Intn(int(h-4)) + 2)
		length := int32(3 + r.Intn(4))
		placeWall(gm, wx, wy, length, true, r)
		mirrorX := w - 1 - wx
		placeWall(gm, mirrorX, wy, length, true, r)
	}

	return gm
}

// placeCluster places a blob of terrain using a simple flood-fill growth.
func placeCluster(gm *GameMap, cx, cy int32, size int, tt component.TerrainType, r *rand.Rand) {
	queue := [][2]int32{{cx, cy}}
	visited := make(map[[2]int32]bool)

	for len(queue) > 0 && size > 0 {
		// Pick random from queue
		idx := r.Intn(len(queue))
		pos := queue[idx]
		queue = append(queue[:idx], queue[idx+1:]...)

		if visited[pos] {
			continue
		}
		visited[pos] = true

		tile := gm.TileAt(pos[0], pos[1])
		if tile == nil {
			continue
		}
		// Don't overwrite river crossings or the main road network.
		if tile.TerrainType == component.TerrainDeep ||
			tile.TerrainType == component.TerrainBridge ||
			tile.TerrainType == component.TerrainRoad {
			continue
		}

		gm.SetTerrain(pos[0], pos[1], tt)
		size--

		// Add neighbors
		for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := pos[0]+d[0], pos[1]+d[1]
			if !visited[[2]int32{nx, ny}] {
				queue = append(queue, [][2]int32{{nx, ny}}...)
			}
		}
	}
}

// placeWall places a line of destructible wall segments.
func placeWall(gm *GameMap, x, y, length int32, horizontal bool, r *rand.Rand) {
	for i := int32(0); i < length; i++ {
		var wx, wy int32
		if horizontal {
			wx, wy = x+i, y
		} else {
			wx, wy = x, y+i
		}
		tile := gm.TileAt(wx, wy)
		if tile == nil {
			continue
		}
		if tile.TerrainType == component.TerrainDeep || tile.TerrainType == component.TerrainBridge {
			continue
		}
		if tile.TerrainType == component.TerrainRoad {
			continue
		}
		gm.SetTerrain(wx, wy, component.TerrainWall)
		tile = gm.TileAt(wx, wy)
		tile.Health = 300
		tile.MaxHealth = 300
		tile.BlockLOS = true
		tile.Elevation = 2
	}
}

// clearArea resets a rectangular area to plains+roads (safe spawn zones).
func clearArea(gm *GameMap, x, y, w, h int32) {
	for dy := int32(0); dy < h; dy++ {
		for dx := int32(0); dx < w; dx++ {
			tile := gm.TileAt(x+dx, y+dy)
			if tile != nil {
				tile.TerrainType = component.TerrainPlain
				tile.Elevation = 0
				tile.BlockLOS = false
				tile.Health = 0
				tile.MaxHealth = 0
			}
		}
	}
	// Road through center of spawn area, matching the map's vertical road axis.
	roadX := x + w/2
	for dy := int32(0); dy < h; dy++ {
		gm.SetTerrain(roadX, y+dy, component.TerrainRoad)
	}
}
