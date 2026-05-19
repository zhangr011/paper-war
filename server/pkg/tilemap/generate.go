package tilemap

import (
	"math"
	"math/rand"
	"sort"

	"github.com/user/paper-war/server/pkg/component"
)

type strongholdSite struct {
	X, Y  int32
	Level int
}

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
	bridgeCount := 3 + r.Intn(2)
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

	// Phase 8: Strongholds scattered across the battlefield. Roads are sparse:
	// building them is expensive, so only important sites become part of the
	// connected road network and lesser outposts may remain off-road.
	strongholds := generateStrongholdSites(w, h, r)
	linkStrongholdsWithRoads(gm, strongholds, r)
	for _, site := range strongholds {
		placeStronghold(gm, site)
	}

	// Phase 9: Shallow water fords (2-3), far from bridges
	placeShallowFords(gm, r)

	// Phase 10: Ensure spawn areas connect to the road network
	ensureSpawnRoadConnection(gm, r)

	// Phase 11: Assign random objective
	assignObjective(gm, r)

	return gm
}

func generateStrongholdSites(w, h int32, r *rand.Rand) []strongholdSite {
	targetCount := int((w * h) / 400)
	if targetCount < 5 {
		targetCount = 5
	}

	margin := int32(5)
	minDist := float64(12)
	sites := make([]strongholdSite, 0, targetCount)
	for attempts := 0; len(sites) < targetCount && attempts < targetCount*80; attempts++ {
		x := margin + int32(r.Intn(int(w-margin*2)))
		y := margin + int32(r.Intn(int(h-margin*2)))

		tooClose := false
		for _, site := range sites {
			if math.Hypot(float64(x-site.X), float64(y-site.Y)) < minDist {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}

		sites = append(sites, strongholdSite{
			X:     x,
			Y:     y,
			Level: 1 + len(sites)%5,
		})
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].Y == sites[j].Y {
			return sites[i].X < sites[j].X
		}
		return sites[i].Y < sites[j].Y
	})
	return sites
}

func linkStrongholdsWithRoads(gm *GameMap, sites []strongholdSite, r *rand.Rand) {
	if len(sites) < 2 {
		return
	}

	roadSites := chooseRoadStrongholds(sites)
	for i := 1; i < len(roadSites); i++ {
		placeRoadPath(gm, roadSites[i-1].X, roadSites[i-1].Y, roadSites[i].X, roadSites[i].Y, r)
	}
}

func chooseRoadStrongholds(sites []strongholdSite) []strongholdSite {
	roadSites := make([]strongholdSite, 0, len(sites))
	for i, site := range sites {
		if site.Level >= 4 || i%3 == 0 {
			roadSites = append(roadSites, site)
		}
	}
	if len(roadSites) < 2 {
		return sites[:2]
	}
	return roadSites
}

func placeRoadPath(gm *GameMap, x1, y1, x2, y2 int32, r *rand.Rand) {
	x, y := x1, y1
	for x != x2 || y != y2 {
		placeRoadTile(gm, x, y)
		if x < x2 {
			x++
		} else if x > x2 {
			x--
		}
		if y < y2 {
			y++
		} else if y > y2 {
			y--
		}
		if r.Intn(4) == 0 {
			if abs32(x2-x) > abs32(y2-y) && y > 1 && y < gm.Height-2 {
				y += []int32{-1, 1}[r.Intn(2)]
			} else if x > 1 && x < gm.Width-2 {
				x += []int32{-1, 1}[r.Intn(2)]
			}
		}
	}
	placeRoadTile(gm, x, y)
}

func placeRoadTile(gm *GameMap, x, y int32) {
	tile := gm.TileAt(x, y)
	if tile == nil {
		return
	}
	if tile.TerrainType == component.TerrainDeep {
		gm.SetTerrain(x, y, component.TerrainBridge)
		tile = gm.TileAt(x, y)
		tile.Health = 500
		tile.MaxHealth = 500
		return
	}
	gm.SetTerrain(x, y, component.TerrainRoad)
	tile = gm.TileAt(x, y)
	tile.Health = 0
	tile.MaxHealth = 0
	tile.BlockLOS = false
	tile.Elevation = 0
}

func placeStronghold(gm *GameMap, site strongholdSite) {
	tt := component.TerrainType(int(component.TerrainStronghold1) + site.Level - 1)
	radius := int32(1 + (site.Level+1)/2)
	for y := site.Y - radius; y <= site.Y+radius; y++ {
		for x := site.X - radius; x <= site.X+radius; x++ {
			tile := gm.TileAt(x, y)
			if tile == nil {
				continue
			}
			if abs32(x-site.X)+abs32(y-site.Y) > radius+1 {
				continue
			}
			gm.SetTerrain(x, y, tt)
			tile = gm.TileAt(x, y)
			tile.Health = 0
			tile.MaxHealth = 0
			tile.BlockLOS = true
			tile.Elevation = int8(1 + site.Level/2)
		}
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
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

// placeShallowFords places 2-3 shallow water fords on deep water tiles,
// each at least 10 tiles Manhattan distance from any bridge.
func placeShallowFords(gm *GameMap, r *rand.Rand) {
	// Collect bridge positions
	var bridges [][2]int32
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			if gm.TileAt(x, y).TerrainType == component.TerrainBridge {
				bridges = append(bridges, [2]int32{x, y})
			}
		}
	}

	// Collect deep water tiles far from bridges
	var candidates [][2]int32
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			if gm.TileAt(x, y).TerrainType != component.TerrainDeep {
				continue
			}
			minDist := int32(999)
			for _, b := range bridges {
				d := abs32(x-b[0]) + abs32(y-b[1])
				if d < minDist {
					minDist = d
				}
			}
			if minDist >= 5 {
				candidates = append(candidates, [2]int32{x, y})
			}
		}
	}

	fordCount := 2 + r.Intn(2) // 2 or 3
	if fordCount > len(candidates) {
		fordCount = len(candidates)
	}
	for i := 0; i < fordCount; i++ {
		idx := r.Intn(len(candidates))
		pos := candidates[idx]
		gm.SetTerrain(pos[0], pos[1], component.TerrainShallow)
		// Remove used candidate
		candidates = append(candidates[:idx], candidates[idx+1:]...)
	}
}

// ensureSpawnRoadConnection checks that each spawn area's road connects
// to the main road/bridge network. If not, places a road path.
func ensureSpawnRoadConnection(gm *GameMap, r *rand.Rand) {
	spawns := [][2]int32{
		{2 + 12 / 2, 2 + 6},               // top-left spawn center road
		{2 + 12 / 2, gm.Height - 14 + 6},  // bottom-left
		{gm.Width - 14 + 12 / 2, 2 + 6},   // top-right
		{gm.Width - 14 + 12 / 2, gm.Height - 14 + 6}, // bottom-right
	}

	for _, spawn := range spawns {
		if !isConnectedToRoadNetwork(gm, spawn[0], spawn[1]) {
			// Find nearest bridge
			nearestBridge := findNearestBridge(gm, spawn[0], spawn[1])
			if nearestBridge != nil {
				placeRoadPath(gm, spawn[0], spawn[1], nearestBridge[0], nearestBridge[1], r)
			}
		}
	}
}

// isConnectedToRoadNetwork does a small BFS from (x,y) along road/bridge tiles.
// Returns true if it reaches a bridge.
func isConnectedToRoadNetwork(gm *GameMap, startX, startY int32) bool {
	visited := make(map[[2]int32]bool)
	queue := [][2]int32{{startX, startY}}
	visited[[2]int32{startX, startY}] = true

	for len(queue) > 0 {
		pos := queue[0]
		queue = queue[1:]
		tile := gm.TileAt(pos[0], pos[1])
		if tile == nil {
			continue
		}
		if tile.TerrainType == component.TerrainBridge {
			return true
		}
		if tile.TerrainType != component.TerrainRoad {
			continue
		}
		for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			next := [2]int32{pos[0] + d[0], pos[1] + d[1]}
			if visited[next] {
				continue
			}
			visited[next] = true
			queue = append(queue, next)
		}
	}
	return false
}

func findNearestBridge(gm *GameMap, x, y int32) *[2]int32 {
	var best *[2]int32
	bestDist := int32(99999)
	for by := int32(0); by < gm.Height; by++ {
		for bx := int32(0); bx < gm.Width; bx++ {
			if gm.TileAt(bx, by).TerrainType == component.TerrainBridge {
				d := abs32(x-bx) + abs32(y-by)
				if d < bestDist {
					bestDist = d
					best = &[2]int32{bx, by}
				}
			}
		}
	}
	return best
}

// assignObjective picks a random objective and fills in type-specific data.
func assignObjective(gm *GameMap, r *rand.Rand) {
	objType := ObjectiveType(r.Intn(3))
	gm.Objective = Objective{Type: objType}

	switch objType {
	case ObjectiveCapture:
		// Find stronghold group closest to map center
		centerX, centerY := gm.Width/2, gm.Height/2
		groups := findStrongholdGroups(gm)
		if len(groups) == 0 {
			gm.Objective.Type = ObjectiveElimination
			return
		}
		var bestGroup [][2]int32
		bestDist := float64(99999)
		for _, g := range groups {
			for _, cell := range g {
				d := math.Hypot(float64(cell[0]-centerX), float64(cell[1]-centerY))
				if d < bestDist {
					bestDist = d
					bestGroup = g
				}
			}
		}
		// Use center of best group
		var sumX, sumY int32
		for _, cell := range bestGroup {
			sumX += cell[0]
			sumY += cell[1]
		}
		gm.Objective.TargetX = sumX / int32(len(bestGroup))
		gm.Objective.TargetY = sumY / int32(len(bestGroup))
		gm.Objective.HoldTarget = 300

	case ObjectiveSurvival:
		gm.Objective.Duration = int32(3000 + r.Intn(3001))
	}
}

func isStrongholdTerrain(tt component.TerrainType) bool {
	return tt >= component.TerrainStronghold1 && tt <= component.TerrainStronghold5
}

func findStrongholdGroups(gm *GameMap) [][][2]int32 {
	visited := make(map[[2]int32]bool)
	var groups [][][2]int32
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			start := [2]int32{x, y}
			if visited[start] {
				continue
			}
			tile := gm.TileAt(x, y)
			if tile == nil || !isStrongholdTerrain(tile.TerrainType) {
				continue
			}
			var group [][2]int32
			queue := [][2]int32{start}
			visited[start] = true
			for len(queue) > 0 {
				cell := queue[0]
				queue = queue[1:]
				group = append(group, cell)
				for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					next := [2]int32{cell[0] + d[0], cell[1] + d[1]}
					if visited[next] {
						continue
					}
					t := gm.TileAt(next[0], next[1])
					if t == nil || !isStrongholdTerrain(t.TerrainType) {
						continue
					}
					visited[next] = true
					queue = append(queue, next)
				}
			}
			groups = append(groups, group)
		}
	}
	return groups
}
