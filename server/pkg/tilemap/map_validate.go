package tilemap

import (
	"github.com/user/paper-war/server/pkg/component"
)

// isConnected checks if there is a traversable path from start to end using the given movement profile.
// A tile is traversable if its terrain cost is > 0 for the profile.
func isConnected(gm *GameMap, start, end [2]int32, profile *component.MovementProfile) bool {
	if start == end {
		return true
	}

	visited := make(map[[2]int32]bool)
	queue := [][2]int32{start}
	visited[start] = true

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := cur[0]+d[0], cur[1]+d[1]
			next := [2]int32{nx, ny}

			if visited[next] {
				continue
			}
			if !gm.inBounds(nx, ny) {
				continue
			}

			cost := gm.CostAt(nx, ny, profile)
			if cost == 0 {
				continue // impassable
			}

			if next == end {
				return true
			}

			visited[next] = true
			queue = append(queue, next)
		}
	}

	return false
}
