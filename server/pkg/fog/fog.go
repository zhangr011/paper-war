package fog

// Fog visibility states.
const (
	FogUnexplored = 0 // never seen — fully black on client
	FogExplored   = 1 // previously seen but not currently in vision — dimmed
	FogVisible    = 2 // currently in vision — full detail
)

const (
	VisionRadiusTiles     = 12 // commander vision
	UnitVisionRadiusTiles = 6  // combat unit vision
)

type FogGrid struct {
	Width, Height int32
	Visible       []uint8 // 0=unexplored, 1=explored, 2=visible
	// BlocksLOS, if set, is consulted during RevealRadius: a candidate tile is
	// only revealed if line-of-sight from the center is clear of blockers.
	// The blocker tile itself is revealed (you see the wall/forest edge, not
	// past it). nil = radius-only vision (pre-phase-2 behavior). Issue #55.
	BlocksLOS func(x, y int32) bool
}

func NewFogGrid(w, h int32) *FogGrid {
	return &FogGrid{
		Width:   w,
		Height:  h,
		Visible: make([]uint8, w*h),
	}
}

// Clear downgrades visible→explored but never loses explored memory.
func (fg *FogGrid) Clear() {
	for i, v := range fg.Visible {
		if v == FogVisible {
			fg.Visible[i] = FogExplored
		}
	}
}

// Reveal sets tiles within radius r around (cx, cy) to FogVisible.
func (fg *FogGrid) Reveal(cx, cy int32) {
	fg.RevealRadius(cx, cy, VisionRadiusTiles)
}

// RevealRadius sets tiles within the given radius around (cx, cy) to
// FogVisible, gated by line-of-sight when fg.BlocksLOS is set.
func (fg *FogGrid) RevealRadius(cx, cy, r int32) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			tx, ty := cx+dx, cy+dy
			if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
				continue
			}
			if fg.BlocksLOS != nil && !fg.hasLOS(cx, cy, tx, ty) {
				continue
			}
			fg.Visible[ty*fg.Width+tx] = FogVisible
		}
	}
}

// hasLOS traces a Bresenham line from (x0,y0) to (x1,y1) and returns false if
// any *intermediate* tile blocks LOS. Start and end tiles are never checked —
// a viewer sees out of its own tile, and the target is visible even if it is
// itself a blocker (you see the wall, not through it). Issue #55 phase 2.
func (fg *FogGrid) hasLOS(x0, y0, x1, y1 int32) bool {
	dx := x1 - x0
	adx := dx
	if adx < 0 {
		adx = -adx
	}
	dy := y1 - y0
	ady := dy
	if ady < 0 {
		ady = -ady
	}
	sx := int32(1)
	if dx < 0 {
		sx = -1
	}
	sy := int32(1)
	if dy < 0 {
		sy = -1
	}
	err := adx - ady
	x, y := x0, y0
	for {
		if !((x == x0 && y == y0) || (x == x1 && y == y1)) && fg.BlocksLOS(x, y) {
			return false
		}
		if x == x1 && y == y1 {
			return true
		}
		e2 := 2 * err
		if e2 > -ady {
			err -= ady
			x += sx
		}
		if e2 < adx {
			err += adx
			y += sy
		}
	}
}

// IsVisible returns true if the tile has been seen (explored or currently visible).
func (fg *FogGrid) IsVisible(tx, ty int32) bool {
	if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
		return false
	}
	return fg.Visible[ty*fg.Width+tx] != FogUnexplored
}

// IsCurrentlyVisible returns true only if the tile is in active vision (FogVisible).
func (fg *FogGrid) IsCurrentlyVisible(tx, ty int32) bool {
	if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
		return false
	}
	return fg.Visible[ty*fg.Width+tx] == FogVisible
}

// Data returns a copy of the visibility grid for network transmission.
func (fg *FogGrid) Data() []byte {
	data := make([]byte, len(fg.Visible))
	copy(data, fg.Visible)
	return data
}

type FogSystem struct {
	Grids      map[uint32]*FogGrid
	MapW, MapH int32
}

func NewFogSystem(mapW, mapH int32) *FogSystem {
	return &FogSystem{
		Grids: make(map[uint32]*FogGrid),
		MapW:  mapW,
		MapH:  mapH,
	}
}

func (fs *FogSystem) GetGrid(playerID uint32) *FogGrid {
	return fs.Grids[playerID]
}

func (fs *FogSystem) GetOrCreateGrid(playerID uint32) *FogGrid {
	grid, ok := fs.Grids[playerID]
	if !ok {
		grid = NewFogGrid(fs.MapW, fs.MapH)
		fs.Grids[playerID] = grid
	}
	return grid
}
