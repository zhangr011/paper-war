package tilemap

import (
	"sort"

	"github.com/user/paper-war/server/pkg/component"
)

// Connectivity guarantee for generated maps.
//
// A unit's flow field is zero at tiles it cannot reach (cost = MAX in the
// Dijkstra). If a target is in a disconnected region of the passability graph,
// the flow field never resolves and the unit freezes — the dominant cause of
// solo-match stalemates. This file enforces that every generated map is a
// single connected component for every standard movement profile, so no target
// is ever unreachable.
//
// Passability matches the flow-field gate at pathfinding/flowfield.go:52 — a
// tile is passable for a profile when gm.CostAt(x,y,profile) > 0.

// ConnectedFor reports whether every passable tile for profile is reachable
// from every other passable tile (4-neighbor, matching flow-field adjacency).
// O(W·H). Connected iff the flood-fill visited count equals the total
// passable-tile count.
func (m *GameMap) ConnectedFor(profile *component.MovementProfile) bool {
	total := int32(0)
	seedX, seedY := int32(-1), int32(-1)
	for y := int32(0); y < m.Height; y++ {
		for x := int32(0); x < m.Width; x++ {
			if profile.TerrainCosts[m.Tiles[y*m.Width+x].TerrainType] > 0 {
				total++
				if seedX < 0 {
					seedX, seedY = x, y
				}
			}
		}
	}
	// No passable tiles (or a single one) — vacuously connected.
	if total <= 1 {
		return true
	}

	visited := make([]bool, m.Width*m.Height)
	queue := [][2]int32{{seedX, seedY}}
	visited[seedY*m.Width+seedX] = true
	count := int32(1)
	dirs := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range dirs {
			nx, ny := cur[0]+d[0], cur[1]+d[1]
			if !m.inBounds(nx, ny) {
				continue
			}
			nidx := ny*m.Width + nx
			if visited[nidx] {
				continue
			}
			if profile.TerrainCosts[m.Tiles[nidx].TerrainType] == 0 {
				continue
			}
			visited[nidx] = true
			count++
			queue = append(queue, [2]int32{nx, ny})
		}
	}
	return count == total
}

// RepairConnectivity reconnects disconnected passable components for every
// profile by converting Deep boundary tiles into crossings:
//   - profiles that can cross Shallow (Light) get Shallow fords,
//   - profiles that cannot (Heavy) get Bridges (which both profiles cross).
//
// The strictest profile (fewest passable tiles) is repaired first — its
// Bridges help the lenient profiles too. Repair is idempotent: a map that is
// already connected is untouched.
//
// Returns true if every profile is single-component on return.
func RepairConnectivity(m *GameMap, profiles []*component.MovementProfile) bool {
	// Run strictest-first so Heavy's Bridges (passable for both) also help
	// Light. Order by number of passable terrain types — Heavy (Shallow
	// impassable) has fewer than Light, so Heavy repairs first and its Bridges
	// reconnect both. If Light ran first it would convert Deep boundaries to
	// Shallow fords (Heavy-impassable), removing the Deep tiles Heavy needs.
	sorted := append([]*component.MovementProfile(nil), profiles...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return passableTerrainTypes(sorted[i]) < passableTerrainTypes(sorted[j])
	})
	for _, p := range sorted {
		repairForProfile(m, p)
	}
	for _, p := range profiles {
		if !m.ConnectedFor(p) {
			return false
		}
	}
	return true
}

// passableTerrainTypes counts how many terrain types are passable for profile.
// Fewer passable types = stricter profile. Used to order repair so the
// strictest profile (Heavy) runs first.
func passableTerrainTypes(profile *component.MovementProfile) int {
	n := 0
	for _, c := range profile.TerrainCosts {
		if c > 0 {
			n++
		}
	}
	return n
}

// repairForProfile reconnects the passable components for one profile by
// carving crossings through Deep water. Each iteration finds the shortest
// Deep path joining two distinct passable components (multi-source BFS
// through Deep, so it handles wide rivers/lakes — not just 1-tile seams) and
// converts it to the profile's crossing terrain. Iterates to a cap so a
// multi-component map is fully rejoined.
func repairForProfile(m *GameMap, profile *component.MovementProfile) {
	const repairIterCap = 16

	// Pick the cheapest crossing the profile can traverse. Shallow for Light
	// (cost 2, natural ford); Bridge for Heavy (cost 1, span). Bridge also
	// works for Light, but Light-only repair prefers Shallow so Heavy routes
	// stay constrained to the existing bridges.
	var crossing component.TerrainType
	if profile.TerrainCosts[component.TerrainShallow] > 0 {
		crossing = component.TerrainShallow
	} else {
		crossing = component.TerrainBridge
	}

	for iter := 0; iter < repairIterCap; iter++ {
		if m.ConnectedFor(profile) {
			return
		}
		comp := labelComponents(m, profile)
		path := shortestDeepCrossing(m, comp)
		if len(path) == 0 {
			// No Deep path can join any two components — nothing more to do.
			return
		}
		for _, p := range path {
			idx := p[1]*m.Width + p[0]
			m.SetTerrain(p[0], p[1], crossing)
			if crossing == component.TerrainBridge {
				m.Tiles[idx].Health = bridgeHealth
				m.Tiles[idx].MaxHealth = bridgeHealth
			}
		}
	}
}

// shortestDeepCrossing finds the shortest Deep-tile path that joins two
// distinct passable components, via multi-source BFS through Deep water.
// Sources are Deep tiles adjacent to a passable component, tagged with that
// component id; the BFS propagates through Deep until two fronts from
// different components meet. Returns the Deep tiles on the joining path
// (inclusive of both meeting endpoints), or nil if no two components are
// joinable through Deep.
func shortestDeepCrossing(m *GameMap, comp []int) [][2]int32 {
	const unvisited = -1
	info := make([]deepCell, m.Width*m.Height)
	for i := range info {
		info[i].srcComp = unvisited
	}

	dirs := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	var queue []int32

	// Seed: every Deep tile adjacent to a passable component.
	for y := int32(0); y < m.Height; y++ {
		for x := int32(0); x < m.Width; x++ {
			idx := y*m.Width + x
			if m.Tiles[idx].TerrainType != component.TerrainDeep {
				continue
			}
			adj := map[int]struct{}{}
			for _, d := range dirs {
				nx, ny := x+d[0], y+d[1]
				if !m.inBounds(nx, ny) {
					continue
				}
				c := comp[ny*m.Width+nx]
				if c >= 0 {
					adj[c] = struct{}{}
				}
			}
			if len(adj) == 0 {
				continue
			}
			if len(adj) >= 2 {
				// 1-tile seam — convert just this tile.
				return [][2]int32{{x, y}}
			}
			// Single adjacent component — seed the front.
			for c := range adj {
				info[idx].srcComp = c
				info[idx].pred = -1
				queue = append(queue, idx)
			}
		}
	}

	// BFS through Deep tiles. When a tile is reached by a front from a
	// different component than an existing front, the two meet there.
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		cx, cy := cur%m.Width, cur/m.Width
		curComp := info[cur].srcComp
		for _, d := range dirs {
			nx, ny := cx+d[0], cy+d[1]
			if !m.inBounds(nx, ny) {
				continue
			}
			nidx := ny*m.Width + nx
			if m.Tiles[nidx].TerrainType != component.TerrainDeep {
				continue
			}
			if info[nidx].srcComp == unvisited {
				info[nidx].srcComp = curComp
				info[nidx].pred = cur
				queue = append(queue, nidx)
				continue
			}
			if info[nidx].srcComp != curComp {
				// Fronts from two different components meet at nidx. The
				// joining path is: trace back from nidx to its source, then
				// back from cur to its source.
				return reconstructDeepPath(info, m.Width, nidx, cur)
			}
		}
	}
	return nil
}

// deepCell tracks the multi-source BFS through Deep water.
type deepCell struct {
	srcComp int   // which passable component this front originated from
	pred    int32 // predecessor flat index, -1 if this is a source
}

// reconstructDeepPath walks predecessors from meetA and meetB back to their
// sources, returning the full Deep path (both ends inclusive).
func reconstructDeepPath(info []deepCell, width, meetA, meetB int32) [][2]int32 {
	var path [][2]int32
	for _, start := range []int32{meetA, meetB} {
		var segment [][2]int32
		cur := start
		for cur != -1 {
			segment = append(segment, [2]int32{cur % width, cur / width})
			cur = info[cur].pred
		}
		// segment is ordered meeting-tile → source; prepend so final path
		// reads source → meetA → meetB → source.
		path = append(reverseSeg(segment), path...)
	}
	return path
}

func reverseSeg(s [][2]int32) [][2]int32 {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// labelComponents BFS-labels every passable tile with a component id (>= 0);
// impassable tiles keep -1.
func labelComponents(m *GameMap, profile *component.MovementProfile) []int {
	comp := make([]int, m.Width*m.Height)
	for i := range comp {
		comp[i] = -1
	}
	nextID := 0
	dirs := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for y := int32(0); y < m.Height; y++ {
		for x := int32(0); x < m.Width; x++ {
			idx := y*m.Width + x
			if comp[idx] != -1 {
				continue
			}
			if profile.TerrainCosts[m.Tiles[idx].TerrainType] == 0 {
				continue
			}
			comp[idx] = nextID
			queue := [][2]int32{{x, y}}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				for _, d := range dirs {
					nx, ny := cur[0]+d[0], cur[1]+d[1]
					if !m.inBounds(nx, ny) {
						continue
					}
					nidx := ny*m.Width + nx
					if comp[nidx] != -1 {
						continue
					}
					if profile.TerrainCosts[m.Tiles[nidx].TerrainType] == 0 {
						continue
					}
					comp[nidx] = nextID
					queue = append(queue, [2]int32{nx, ny})
				}
			}
			nextID++
		}
	}
	return comp
}
