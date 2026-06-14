package pathfinding

import (
	"container/heap"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type Direction struct {
	DX, DY int64
}

type FlowField struct {
	Width, Height int32
	Directions    []Direction
	Costs         []uint32
}

func (f *FlowField) GetDirection(x, y int32) Direction {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return Direction{}
	}
	return f.Directions[y*f.Width+x]
}

func Compute(gm *tilemap.GameMap, targetX, targetY int32, profile *component.MovementProfile) *FlowField {
	w, h := gm.Width, gm.Height
	size := int(w * h)
	costs := make([]uint32, size)
	for i := range costs {
		costs[i] = ^uint32(0)
	}

	pq := &priorityQueue{}
	heap.Init(pq)
	costs[targetY*w+targetX] = 0
	heap.Push(pq, &pqItem{x: targetX, y: targetY, cost: 0})

	dirs4 := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*pqItem)
		if cur.cost > costs[cur.y*w+cur.x] {
			continue
		}
		for _, d := range dirs4 {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			mc := gm.CostAt(nx, ny, profile)
			if mc == 0 {
				continue
			}
			newCost := cur.cost + uint32(mc)
			ni := ny*w + nx
			if newCost < costs[ni] {
				costs[ni] = newCost
				heap.Push(pq, &pqItem{x: nx, y: ny, cost: newCost})
			}
		}
	}

	dirs8 := [8][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	directions := make([]Direction, size)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			i := y*w + x
			if costs[i] == ^uint32(0) || (x == targetX && y == targetY) {
				continue
			}
			bestCost := costs[i]
			bestDX, bestDY := int32(0), int32(0)
			for _, d := range dirs8 {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || nx >= w || ny < 0 || ny >= h {
					continue
				}
				ni := ny*w + nx
				if costs[ni] < bestCost {
					bestCost = costs[ni]
					bestDX = d[0]
					bestDY = d[1]
				}
			}
			if bestDX != 0 || bestDY != 0 {
				length := fixed.ISqrt(fixed.FromFloat(float64(bestDX*bestDX + bestDY*bestDY)))
				if length > 0 {
					directions[i] = Direction{
						DX: fixed.Div(fixed.FromFloat(float64(bestDX)), length),
						DY: fixed.Div(fixed.FromFloat(float64(bestDY)), length),
					}
				}
			}
		}
	}

	return &FlowField{Width: w, Height: h, Directions: directions, Costs: costs}
}

type pqItem struct {
	x, y int32
	cost uint32
}
type priorityQueue []*pqItem
func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].cost < pq[j].cost }
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*pqItem)) }
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}