package spatial

type cellKey struct {
	X, Y int32
}

type entry struct {
	ID uint64
	X  int64
	Y  int64
}

type Hash struct {
	CellSize  int64
	inverseCS int64
	cells     map[cellKey][]entry
	positions map[uint64]cellKey
}

func NewHash(cellSize int64) *Hash {
	return &Hash{
		CellSize:  cellSize,
		inverseCS: (1 << 12) / cellSize,
		cells:     make(map[cellKey][]entry, 1024),
		positions: make(map[uint64]cellKey, 1024),
	}
}

func (h *Hash) cellCoord(x int64) int32 {
	return int32((x * h.inverseCS) >> 12)
}

func (h *Hash) cellKey(x, y int64) cellKey {
	return cellKey{X: h.cellCoord(x), Y: h.cellCoord(y)}
}

func (h *Hash) Insert(id uint64, x, y int64) {
	ck := h.cellKey(x, y)
	h.cells[ck] = append(h.cells[ck], entry{ID: id, X: x, Y: y})
	h.positions[id] = ck
}

func (h *Hash) Remove(id uint64) {
	ck, ok := h.positions[id]
	if !ok {
		return
	}
	cell := h.cells[ck]
	for i, e := range cell {
		if e.ID == id {
			cell[i] = cell[len(cell)-1]
			h.cells[ck] = cell[:len(cell)-1]
			break
		}
	}
	if len(h.cells[ck]) == 0 {
		delete(h.cells, ck)
	}
	delete(h.positions, id)
}

func (h *Hash) Update(id uint64, x, y int64) {
	h.Remove(id)
	h.Insert(id, x, y)
}

func (h *Hash) Clear() {
	for k := range h.cells {
		delete(h.cells, k)
	}
	for k := range h.positions {
		delete(h.positions, k)
	}
}

func (h *Hash) Query(x, y, radius int64) []uint64 {
	radiusSq := (radius * radius) >> 12
	cx := h.cellCoord(x)
	cy := h.cellCoord(y)

	var result []uint64
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			ck := cellKey{X: cx + dx, Y: cy + dy}
			cell, ok := h.cells[ck]
			if !ok {
				continue
			}
			for _, e := range cell {
				ddx := e.X - x
				ddy := e.Y - y
				distSq := (ddx*ddx + ddy*ddy) >> 12
				if distSq <= radiusSq {
					result = append(result, e.ID)
				}
			}
		}
	}
	return result
}