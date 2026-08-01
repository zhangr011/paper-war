package tilemap

import (
	"log"
	"math"
	"math/rand"
	"sort"

	perlin "github.com/aquilax/go-perlin"
	"github.com/user/paper-war/server/pkg/component"
)

// Generation constants — internal tuning knobs for the heightmap pipeline.
// Not exposed to callers. Extract to config when clash maps need this pipeline.
const (
	// Heightmap noise
	heightFreq    = 0.05  // base frequency for heightmap noise
	heightStretch = 2.5   // X-axis stretching for elongated ridges
	heightAlpha   = 2.0   // Perlin persistence
	heightBeta    = 2.0   // Perlin lacunarity
	heightOctaves = 1     // single octave

	// Forest noise
	forestFreq  = 0.18  // higher freq → smaller, more scattered forest patches (matches design/map.png scattered tree clusters)
	forestCoord = 1000.0 // coordinate offset for independent noise layer

	// Coverage targets
	hillFraction   = 0.12 // ~12% of tiles become hill
	waterFraction  = 0.02 // ~2% of tiles become deep water
	forestFraction = 0.15 // ~15% of eligible tiles become forest (design/map.png is grass-dominant with ~5% dark green patches; higher fraction still reads as grassland with scattered tree cover)

	// Environmental scatter (issue #55 phase 3)
	rockFraction  = 0.08 // fraction of hill tiles that become Rock
	brushFraction = 0.05 // fraction of plain tiles that become Brush

	// River
	riverMaxWidth = 3 // max width at downstream end

	// Strongholds
	strongholdDenom = 2000 // w*h / this = stronghold count
	strongholdMax   = 3    // cap on stronghold count
	passThreshold1  = 2    // primary: non-hill with 2+ hill neighbors
	passThreshold2  = 1    // relaxed: non-hill with 1+ hill neighbor

	// Bridges
	bridgeHealth      = 200
	bridgeMaxHalfSpan = 3 // max tiles each Y direction a bridge converts (bounds the crossing so it doesn't eat an entire vertical river run)
	bridgeEdgeMargin  = bridgeMaxHalfSpan + 1 // narrows within this of a map edge are skipped so the full bridge span stays interior (no corner bridges with no water to span)

	// Spawns
	spawnClearRadius = 3 // 6x6 clearing (radius 3)
	spawnSearchDepth = 12 // max rows inward from edge

	// Objective
	survivalChance = 15  // 15% of maps roll Survival
	captureChance  = 50  // 50% of non-Survival maps roll Capture (#54 1B: Target = map center, decoupled from strongholds)
	survivalTicks  = 3000 // 5 minutes at ServerTicksPerSecond=10
	captureHold    = 300  // 30 seconds at ServerTicksPerSecond=10
)

// GenerateMap creates a procedural terrain map using a heightmap-driven pipeline.
// The output is fully deterministic: same (w, h, seed) always produces the same map.
//
// Pipeline: heightmap → hills → river → lake → forest → strongholds → bridges → spawns → objective → validate
//
// If the generated map fails connectivity validation, it retries with incremented seeds
// (up to maxMapRetries) before giving up.
func GenerateMap(w, h int32, seed int64) *GameMap {
	const maxMapRetries = 20
	for attempt := 0; attempt < maxMapRetries; attempt++ {
		gm := generateMapOnce(w, h, seed+int64(attempt))
		// Validate connectivity
		profiles := component.StandardMovementProfiles()
		spawn1 := gm.Spawns[0]
		spawn2 := gm.Spawns[1]
		if isConnected(gm, spawn1, spawn2, profiles[0]) && isConnected(gm, spawn1, spawn2, profiles[1]) {
			return gm
		}
		// Connectivity failed — retry with next seed
	}
	// All retries failed — return the last map anyway (rare edge case)
	log.Printf("WARNING: GenerateMap failed connectivity after %d retries with base seed %d", maxMapRetries, seed)
	return generateMapOnce(w, h, seed)
}

// generateMapOnce creates a single procedural map instance for the given seed.
func generateMapOnce(w, h int32, seed int64) *GameMap {
	gm := NewGameMap(w, h)
	r := rand.New(rand.NewSource(seed))
	p := perlin.NewPerlin(heightAlpha, heightBeta, heightOctaves, seed)

	// Stage 1: Generate heightmap
	heightmap := make([]float64, w*h)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			nx := float64(x) * heightFreq * heightStretch
			ny := float64(y) * heightFreq
			heightmap[y*w+x] = p.Noise2D(nx, ny)
		}
	}

	// Stage 2: Classify hills from heightmap
	// Find the threshold that gives ~hillFraction coverage.  Within the
	// hill region, the top 25% by height (top ~3% of total area) is
	// promoted to peak layer (2); the rest of the hills become mid
	// layer (1).  Non-hill tiles keep the implicit zero-value (low).
	// Issue #49 — discrete 3-layer model replaces the continuous 0-100
	// int8 elevation that no downstream consumer actually consumed as
	// a continuous signal.
	hillThreshold := findPercentile(heightmap, 1.0-hillFraction)
	peakThreshold := findPercentile(heightmap, 1.0-hillFraction*0.25)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			v := heightmap[y*w+x]
			if v >= hillThreshold {
				gm.SetTerrain(x, y, component.TerrainHill)
				if v >= peakThreshold {
					gm.TileAt(x, y).Elevation = 2
				} else {
					gm.TileAt(x, y).Elevation = 1
				}
			}
		}
	}

	// Stage 3: River — downhill trace from random high point
	riverTiles := traceRiver(gm, heightmap, r, w, h)

	// Stage 4: Lake — sea level sweep for remaining water budget
	waterBudget := int(float64(w*h) * waterFraction)
	remainingBudget := waterBudget - len(riverTiles)
	if remainingBudget > 0 {
		lakeTiles := fillLake(gm, heightmap, remainingBudget, w, h)
		riverTiles = append(riverTiles, lakeTiles...)
	}

	// Stage 5: Forest — second noise layer with adaptive threshold
	applyForest(gm, p, w, h)

	// Stage 6: Environmental scatter — rocks (heavy cover, blocks LOS) on
	// hills and brush (light cover) on plains. Sparse. Issue #55 phase 3.
	applyScatter(gm, r, w, h)

	// Stage 7: Pass detection & stronghold placement (records gm.Strongholds
	// specs — entities are spawned by the session at match start, #54).
	placeStrongholds(gm, r, w, h)

	// Stage 8: Bridge placement on river
	placeBridges(gm, riverTiles, r)

	// Stage 9: Spawn placement
	spawn1 := placeSpawn(gm, w/2, int32(3), w, h) // top-center
	spawn2 := placeSpawn(gm, w/2, h-4, w, h)       // bottom-center

	// Stage 10: Objective assignment
	assignProceduralObjective(gm, r)

	// Store metadata (validation handled by caller GenerateMap)
	gm.Spawns = [][2]int32{spawn1, spawn2}
	gm.Seed = seed

	return gm
}

// findPercentile returns the value at the given percentile in the heightmap.
func findPercentile(data []float64, frac float64) float64 {
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	idx := int(frac * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func minIndex(data []float64) int {
	minVal := data[0]
	minIdx := 0
	for i, v := range data {
		if v < minVal {
			minVal = v
			minIdx = i
		}
	}
	return minIdx
}

// traceRiver traces a downhill path from a random high point to the lowest point.
// Returns the list of deep water tiles placed.
func traceRiver(gm *GameMap, heightmap []float64, r *rand.Rand, w, h int32) [][2]int32 {
	// Find tiles in top 10% of elevation
	var highCandidates [][2]int32
	threshold10 := findPercentile(heightmap, 0.90)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			if heightmap[y*w+x] >= threshold10 {
				highCandidates = append(highCandidates, [2]int32{x, y})
			}
		}
	}
	if len(highCandidates) == 0 {
		return nil
	}

	// Pick a random high point
	source := highCandidates[r.Intn(len(highCandidates))]

	// Find the global minimum
	lowestIdx := minIndex(heightmap)
	target := [2]int32{int32(lowestIdx) % w, int32(lowestIdx) / w}

	// Trace downhill via steepest descent
	var path [][2]int32
	visited := make(map[[2]int32]bool)
	cur := source
	visited[cur] = true

	for cur != target {
		path = append(path, cur)
		// Find the lowest unvisited neighbor
		bestNext := cur
		bestHeight := heightmap[cur[1]*w+cur[0]]

		dirs := [][2]int32{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
		// Shuffle for tiebreaking
		r.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })

		for _, d := range dirs {
			nx, ny := cur[0]+d[0], cur[1]+d[1]
			if nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			next := [2]int32{nx, ny}
			if visited[next] {
				continue
			}
			h := heightmap[ny*w+nx]
			if h < bestHeight {
				bestHeight = h
				bestNext = next
			}
		}

		if bestNext == cur {
			// Stuck — all lower neighbors visited. Allow revisiting.
			// Just move toward target.
			dx := target[0] - cur[0]
			dy := target[1] - cur[1]
			if abs32(abs32(dx)) > abs32(dy) {
				if dx > 0 {
					bestNext = [2]int32{cur[0] + 1, cur[1]}
				} else {
					bestNext = [2]int32{cur[0] - 1, cur[1]}
				}
			} else {
				if dy > 0 {
					bestNext = [2]int32{cur[0], cur[1] + 1}
				} else {
					bestNext = [2]int32{cur[0], cur[1] - 1}
				}
			}
		}

		visited[bestNext] = true
		cur = bestNext

		// Safety: don't trace forever
		if len(path) > int(w+h)*2 {
			break
		}
	}
	path = append(path, target)

	// Paint river tiles with variable width
	var allRiver [][2]int32
	pathLen := len(path)
	for i, pos := range path {
		// Width: 1 upstream, widening to riverMaxWidth downstream
		t := float64(i) / float64(pathLen) // 0.0 at source, 1.0 at lake
		width := 1 + int(t*float64(riverMaxWidth-1))
		if width < 1 {
			width = 1
		}

		tile := gm.TileAt(pos[0], pos[1])
		if tile != nil && tile.TerrainType == component.TerrainHill {
			// Don't overwrite hills with river — skip
			continue
		}

		gm.SetTerrain(pos[0], pos[1], component.TerrainDeep)
		allRiver = append(allRiver, pos)

		// Widen: add perpendicular tiles
		if width > 1 {
			for dw := 1; dw < width; dw++ {
				for _, offset := range []int32{int32(dw), -int32(dw)} {
					nx := pos[0] + offset
					ny := pos[1]
					if nx >= 0 && nx < w && ny >= 0 && ny < h {
						t2 := gm.TileAt(nx, ny)
						if t2 != nil && t2.TerrainType != component.TerrainHill {
							gm.SetTerrain(nx, ny, component.TerrainDeep)
							allRiver = append(allRiver, [2]int32{nx, ny})
						}
					}
				}
			}
		}
	}

	return allRiver
}

// fillLake fills the lowest depression to consume remaining water budget.
func fillLake(gm *GameMap, heightmap []float64, budget int, w, h int32) [][2]int32 {
	if budget <= 0 {
		return nil
	}

	// Sort all tile positions by height (ascending)
	type posHeight struct {
		x, y int32
		h    float64
	}
	candidates := make([]posHeight, 0, w*h)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			tile := gm.TileAt(x, y)
			if tile != nil && tile.TerrainType == component.TerrainPlain {
				candidates = append(candidates, posHeight{x, y, heightmap[y*w+x]})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].h < candidates[j].h
	})

	// Fill the lowest tiles that form a connected region
	// Start from the absolute lowest plain tile
	var lakeTiles [][2]int32
	if len(candidates) == 0 {
		return nil
	}

	// BFS from lowest tile, expanding to neighbors of similar or lower height
	visited := make(map[[2]int32]bool)
	queue := [][2]int32{{candidates[0].x, candidates[0].y}}
	visited[queue[0]] = true

	for len(queue) > 0 && len(lakeTiles) < budget {
		cur := queue[0]
		queue = queue[1:]

		tile := gm.TileAt(cur[0], cur[1])
		if tile == nil || tile.TerrainType != component.TerrainPlain {
			continue
		}

		gm.SetTerrain(cur[0], cur[1], component.TerrainDeep)
		lakeTiles = append(lakeTiles, cur)

		for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := cur[0]+d[0], cur[1]+d[1]
			next := [2]int32{nx, ny}
			if visited[next] || nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			nt := gm.TileAt(nx, ny)
			if nt != nil && (nt.TerrainType == component.TerrainPlain || nt.TerrainType == component.TerrainDeep) {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	return lakeTiles
}

// applyForest applies forest terrain using a second noise layer with adaptive threshold.
func applyForest(gm *GameMap, p *perlin.Perlin, w, h int32) float64 {
	// Generate forest noise
	forestNoise := make([]float64, w*h)
	var eligible []struct {
		idx int
		val float64
	}

	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			idx := y*w + x
			tile := gm.TileAt(x, y)
			if tile.TerrainType == component.TerrainPlain {
				nx := float64(x)*forestFreq + forestCoord
				ny := float64(y)*forestFreq + forestCoord
				forestNoise[idx] = p.Noise2D(nx, ny)
			eligible = append(eligible, struct {
				idx int
				val float64
			}{int(idx), forestNoise[idx]})
			}
		}
	}

	if len(eligible) == 0 {
		return 0
	}

	// Find the threshold at (1-forestFraction) percentile — tiles above this become forest
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].val < eligible[j].val
	})
	cutoffIdx := int(float64(len(eligible)) * (1 - forestFraction))
	if cutoffIdx < 0 {
		cutoffIdx = 0
	}
	if cutoffIdx >= len(eligible) {
		cutoffIdx = len(eligible) - 1
	}
	threshold := eligible[cutoffIdx].val

	// Apply forest to tiles above threshold
	for _, e := range eligible {
		if e.val >= threshold {
			x := int32(e.idx % int(w))
			y := int32(e.idx / int(w))
			gm.SetTerrain(x, y, component.TerrainForest)
		}
	}

	return threshold
}

// applyScatter sprinkles environmental objects across the map: Rock on hill
// tiles (heavy cover, blocks LOS) and Brush on plains (light cover, no LOS
// block). Sparse by design — adds tactical texture without overtaking the
// terrain. Rocks are passable-slow for Heavy so they don't cut Heavy routes
// (connectivity is still re-validated by GenerateMap's retry loop). Issue #55
// phase 3.
func applyScatter(gm *GameMap, r *rand.Rand, w, h int32) {
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			tile := gm.TileAt(x, y)
			if tile == nil {
				continue
			}
			switch tile.TerrainType {
			case component.TerrainHill:
				if r.Float64() < rockFraction {
					gm.SetTerrain(x, y, component.TerrainRock)
				}
			case component.TerrainPlain:
				if r.Float64() < brushFraction {
					gm.SetTerrain(x, y, component.TerrainBrush)
				}
			}
		}
	}
}

// placeStrongholds finds ridge passes and records stronghold placements on
// gm.Strongholds (StrongholdSpec — position + level). Strongholds are no
// longer terrain; the session spawns a Stronghold entity for each spec at
// match start (ADR-0023 / issue #54).
func placeStrongholds(gm *GameMap, r *rand.Rand, w, h int32) [][2]int32 {
	// Detect passes: non-hill tiles flanked by hill tiles
	type pass struct {
		x, y   int32
		score  int // number of hill neighbors
	}

	var passes []pass
	for y := int32(1); y < h-1; y++ {
		for x := int32(1); x < w-1; x++ {
			tile := gm.TileAt(x, y)
			if tile == nil || tile.TerrainType == component.TerrainHill ||
				tile.TerrainType == component.TerrainDeep {
				continue
			}
			hillNeighbors := 0
			for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nt := gm.TileAt(x+d[0], y+d[1])
				if nt != nil && nt.TerrainType == component.TerrainHill {
					hillNeighbors++
				}
			}
			if hillNeighbors >= passThreshold1 {
				passes = append(passes, pass{x, y, hillNeighbors})
			}
		}
	}

	// If not enough passes, relax threshold
	if len(passes) < 1 {
		for y := int32(1); y < h-1; y++ {
			for x := int32(1); x < w-1; x++ {
				tile := gm.TileAt(x, y)
				if tile == nil || tile.TerrainType == component.TerrainHill ||
					tile.TerrainType == component.TerrainDeep {
					continue
				}
				hillNeighbors := 0
				for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nt := gm.TileAt(x+d[0], y+d[1])
					if nt != nil && nt.TerrainType == component.TerrainHill {
						hillNeighbors++
					}
				}
				if hillNeighbors >= passThreshold2 {
					passes = append(passes, pass{x, y, hillNeighbors})
				}
			}
		}
	}

	// If still no passes, find tiles nearest to ridges
	if len(passes) < 1 {
		var hills [][2]int32
		for y := int32(0); y < h; y++ {
			for x := int32(0); x < w; x++ {
				if gm.TileAt(x, y).TerrainType == component.TerrainHill {
					hills = append(hills, [2]int32{x, y})
				}
			}
		}
		for y := int32(1); y < h-1; y++ {
			for x := int32(1); x < w-1; x++ {
				tile := gm.TileAt(x, y)
				if tile == nil || tile.TerrainType == component.TerrainHill ||
					tile.TerrainType == component.TerrainDeep {
					continue
				}
				minDist := float64(999)
				for _, hp := range hills {
					d := math.Hypot(float64(x-hp[0]), float64(y-hp[1]))
					if d < minDist {
						minDist = d
					}
				}
				if minDist < 4 {
					passes = append(passes, pass{x, y, int(10 - minDist)})
				}
			}
		}
	}

	// Determine stronghold count
	targetCount := int((w * h) / strongholdDenom)
	if targetCount < 1 {
		targetCount = 1
	}
	if targetCount > strongholdMax {
		targetCount = strongholdMax
	}

	if len(passes) < targetCount {
		targetCount = len(passes)
	}

	// Sort passes by score (prefer higher-scoring passes), then by proximity to center
	sort.Slice(passes, func(i, j int) bool {
		return passes[i].score > passes[j].score
	})

	// Pick top passes, ensuring minimum spacing
	var selected [][2]int32
	for _, ps := range passes {
		if len(selected) >= targetCount {
			break
		}
		tooClose := false
		for _, s := range selected {
			if abs32(ps.x-s[0])+abs32(ps.y-s[1]) < 10 {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		selected = append(selected, [2]int32{ps.x, ps.y})
	}

	// Record stronghold placements as specs (no terrain — strongholds are
	// entities spawned by the session at match start, ADR-0023 / issue #54).
	// Skip hill/deep tiles so the stronghold sits on passable ground.
	for _, pos := range selected {
		tile := gm.TileAt(pos[0], pos[1])
		if tile != nil && tile.TerrainType != component.TerrainHill &&
			tile.TerrainType != component.TerrainDeep {
			gm.Strongholds = append(gm.Strongholds, StrongholdSpec{
				X: pos[0], Y: pos[1], Level: 1,
			})
		}
	}

	return selected
}

// placeBridges places 1-2 destructible bridges at the narrowest river sections.
func placeBridges(gm *GameMap, riverTiles [][2]int32, r *rand.Rand) {
	if len(riverTiles) < 3 {
		return
	}

	// Group river tiles into contiguous segments
	// For simplicity, find "narrow" points: river tiles with non-water neighbors on both sides
	// along one axis. A narrow point has land close on at least one axis.
	type narrowPoint struct {
		x, y    int32
		gapSize int // estimated width at this point
	}

	var narrows []narrowPoint
	riverSet := make(map[[2]int32]bool)
	for _, rt := range riverTiles {
		riverSet[rt] = true
	}

	for _, rt := range riverTiles {
		// Check horizontal gap: count consecutive river tiles in X direction
		gapX := 1
		for dx := int32(1); ; dx++ {
			if riverSet[[2]int32{rt[0] + dx, rt[1]}] {
				gapX++
			} else {
				break
			}
		}
		for dx := int32(-1); ; dx-- {
			if riverSet[[2]int32{rt[0] + dx, rt[1]}] {
				gapX++
			} else {
				break
			}
		}

		// Only consider points where gap is small (narrow) and not at the
		// map edge (edge bridges have no water to span on one side).
		if gapX <= 4 &&
			rt[0] >= bridgeEdgeMargin && rt[0] < gm.Width-bridgeEdgeMargin &&
			rt[1] >= bridgeEdgeMargin && rt[1] < gm.Height-bridgeEdgeMargin {
			narrows = append(narrows, narrowPoint{rt[0], rt[1], gapX})
		}
	}

	// Sort by gap size (narrowest first)
	sort.Slice(narrows, func(i, j int) bool {
		return narrows[i].gapSize < narrows[j].gapSize
	})

	// Place 1-2 bridges at narrowest points with minimum spacing
	bridgeCount := 1
	if len(narrows) > 5 {
		bridgeCount = 2
	}

	placed := 0
	for _, np := range narrows {
		if placed >= bridgeCount {
			break
		}

		// Check minimum spacing from existing bridges
		tooClose := false
		for y := int32(0); y < gm.Height; y++ {
			for x := int32(0); x < gm.Width; x++ {
				if gm.TileAt(x, y).TerrainType == component.TerrainBridge {
					if abs32(x-np.x)+abs32(y-np.y) < 10 {
						tooClose = true
						break
					}
				}
			}
			if tooClose {
				break
			}
		}
		if tooClose {
			continue
		}

		// Place bridge: convert this river tile and any adjacent river tiles at same x
		gm.SetTerrain(np.x, np.y, component.TerrainBridge)
		tile := gm.TileAt(np.x, np.y)
		tile.Health = bridgeHealth
		tile.MaxHealth = bridgeHealth

		// Also bridge adjacent river tiles in Y direction (full crossing),
		// but bounded to bridgeMaxHalfSpan each way so the bridge spans the
		// river width instead of consuming an entire vertical Deep run
		// (which would leave no water adjacent and eat long river segments).
		for dy := int32(-1); dy >= -bridgeMaxHalfSpan; dy-- {
			t := gm.TileAt(np.x, np.y+dy)
			if t != nil && t.TerrainType == component.TerrainDeep {
				gm.SetTerrain(np.x, np.y+dy, component.TerrainBridge)
				t = gm.TileAt(np.x, np.y+dy)
				t.Health = bridgeHealth
				t.MaxHealth = bridgeHealth
			} else {
				break
			}
		}
		for dy := int32(1); dy <= bridgeMaxHalfSpan; dy++ {
			t := gm.TileAt(np.x, np.y+dy)
			if t != nil && t.TerrainType == component.TerrainDeep {
				gm.SetTerrain(np.x, np.y+dy, component.TerrainBridge)
				t = gm.TileAt(np.x, np.y+dy)
				t.Health = bridgeHealth
				t.MaxHealth = bridgeHealth
			} else {
				break
			}
		}

		placed++
	}
}

// placeSpawn finds a suitable spawn location near (targetX, targetY) and clears a 6x6 area.
func placeSpawn(gm *GameMap, targetX, targetY int32, w, h int32) [2]int32 {
	// Try target position first, then search outward
	cx, cy := targetX, targetY

	// Clamp
	if cx < spawnClearRadius {
		cx = spawnClearRadius
	}
	if cx >= w-spawnClearRadius {
		cx = w - spawnClearRadius - 1
	}
	if cy < spawnClearRadius {
		cy = spawnClearRadius
	}
	if cy >= h-spawnClearRadius {
		cy = h - spawnClearRadius - 1
	}

	// Check if center is suitable (not hill, not water)
	tile := gm.TileAt(cx, cy)
	if tile != nil && (tile.TerrainType == component.TerrainHill || tile.TerrainType == component.TerrainDeep) {
		// Search outward along edge row then inward
		found := false
		// Determine search direction (top spawn searches rows going inward, bottom goes inward)
		rowDir := int32(1)
		if targetY > h/2 {
			rowDir = -1
		}

		for dy := int32(0); dy < spawnSearchDepth && !found; dy++ {
			for dx := int32(0); dx < w/2 && !found; dx++ {
				for _, ox := range []int32{dx, -dx} {
					tx := cx + ox
					ty := cy + dy*rowDir
					if tx < spawnClearRadius || tx >= w-spawnClearRadius ||
						ty < spawnClearRadius || ty >= h-spawnClearRadius {
						continue
					}
					t := gm.TileAt(tx, ty)
					if t != nil && t.TerrainType != component.TerrainHill && t.TerrainType != component.TerrainDeep {
						cx, cy = tx, ty
						found = true
						break
					}
				}
			}
		}
	}

	// Clear 6x6 area to plains
	for dy := int32(-spawnClearRadius); dy <= spawnClearRadius; dy++ {
		for dx := int32(-spawnClearRadius); dx <= spawnClearRadius; dx++ {
			tile := gm.TileAt(cx+dx, cy+dy)
			if tile != nil {
				tile.TerrainType = component.TerrainPlain
				tile.BlockLOS = false
				tile.Health = 0
				tile.MaxHealth = 0
			}
		}
	}

	return [2]int32{cx, cy}
}

// assignProceduralObjective assigns objective based on the map's recorded
// stronghold placements (gm.Strongholds — specs, not terrain, per #54).
func assignProceduralObjective(gm *GameMap, r *rand.Rand) {
	// 15% Survival chance
	if r.Intn(100) < survivalChance {
		gm.Objective = Objective{
			Type:     ObjectiveSurvival,
			Duration: survivalTicks,
		}
		return
	}

	// Capture objective targets a neutral map-center point that is INDEPENDENT
	// of stronghold positions (#54 1B / ADR-0023: Stronghold ≠ Target — a
	// stronghold is a capturable resource, the Target is the win point). The
	// target is a designated tile; occupation-hold there wins the match.
	centerX, centerY := gm.Width/2, gm.Height/2

	// 50% Capture chance (regardless of strongholds); otherwise Elimination.
	if r.Intn(100) < captureChance {
		gm.Objective = Objective{
			Type:       ObjectiveCapture,
			TargetX:    centerX,
			TargetY:    centerY,
			HoldTarget: captureHold,
		}
		return
	}

	// Default: Elimination
	gm.Objective = Objective{
		Type: ObjectiveElimination,
	}
}

// abs32 returns the absolute value of an int32.
func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
