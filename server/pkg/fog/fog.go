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

// Concealment — a unit on a Conceals terrain (Forest/Brush) is hidden from
// enemies unless one of those conditions holds:
//   - a friendly detector is within ConcealmentDetectionRadius tiles, OR
//   - the unit fired within the last ConcealRevealTicks (attacking gives away
//     its position).
// The concealed unit's tile stays FogVisible (the viewer sees the trees, not
// the ambusher). ADR-0029.
const (
	ConcealmentDetectionRadius = 3 // tiles within which a detector spots a concealed unit
	ConcealRevealTicks         = 8 // ~0.8s @ 10 Hz that firing breaks concealment
)

type FogGrid struct {
	Width, Height int32
	Visible       []uint8 // 0=unexplored, 1=explored, 2=visible
	// BlocksLOS, if set, is consulted during RevealRadius: a candidate tile is
	// only revealed if line-of-sight from the center is clear of blockers.
	// The blocker tile itself is revealed (you see the wall/forest edge, not
	// past it). nil = radius-only vision (pre-phase-2 behavior). Issue #55.
	BlocksLOS func(x, y int32) bool
	// ElevationAt returns the elevation band (0/1/2) of a tile for height-aware
	// LOS (Phase 2 of terrain-starcraft-plan). nil = flat map; only BlocksLOS
	// is consulted. When set, a viewer on elevation Ev sees OVER intermediate
	// tiles strictly lower than Ev (low cover, low cliffs) — the only thing
	// that still blocks a high-ground viewer is taller terrain.
	ElevationAt func(x, y int32) uint8
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

// Reveal sets tiles within radius r around (cx, cy) to FogVisible. The viewer
// is assumed to be on low ground (elevation 0); callers that know the viewer's
// elevation should use RevealRadius directly.
func (fg *FogGrid) Reveal(cx, cy int32) {
	fg.RevealRadius(cx, cy, VisionRadiusTiles, 0)
}

// RevealRadius sets tiles within the given radius around (cx, cy) to
// FogVisible, gated by height-aware line-of-sight when fg.BlocksLOS is set.
// viewerElev is the elevation band of the viewer's tile; high ground sees over
// low intermediate cover and low cliffs (Phase 2 of terrain-starcraft-plan).
func (fg *FogGrid) RevealRadius(cx, cy, r int32, viewerElev uint8) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			tx, ty := cx+dx, cy+dy
			if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
				continue
			}
			if fg.BlocksLOS != nil && !fg.hasLOS(cx, cy, tx, ty, viewerElev) {
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
//
// Height rule (Phase 2): a viewer on elevation Ev is blocked by an
// intermediate tile iff (a) that tile's elevation is strictly higher than Ev
// (a taller cliff always walls off sight), or (b) the tile's terrain
// BlocksLOS (Forest/Wall/Rock) AND it is at least as high as the viewer. The
// net effect: high ground sees OVER low cover (Brush, low Forest) and low
// cliffs; only same-or-higher terrain blockers still stop sight.
func (fg *FogGrid) hasLOS(x0, y0, x1, y1 int32, viewerElev uint8) bool {
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
		if !((x == x0 && y == y0) || (x == x1 && y == y1)) && fg.tileBlocksLOS(x, y, viewerElev) {
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

// tileBlocksLOS reports whether an intermediate tile at (x,y) blocks the line
// of sight of a viewer on elevation viewerElev. Phase 2 height rule:
//   - A strictly taller tile (elev > viewerElev) always blocks — a higher
//     cliff walls off sight regardless of what grows on top of it.
//   - Otherwise, terrain blockers (Forest/Wall/Rock via BlocksLOS) block only
//     when at least as high as the viewer (elev >= viewerElev). High ground
//     sees over low cover and low cliffs.
//   - With no elevation data (ElevationAt nil), terrain alone blocks — this
//     preserves the pre-Phase-2 behavior for tests/callers that never opted in.
func (fg *FogGrid) tileBlocksLOS(x, y int32, viewerElev uint8) bool {
	if fg.ElevationAt != nil {
		interElev := fg.ElevationAt(x, y)
		if interElev > viewerElev {
			return true // taller ground always walls off sight
		}
		// Viewer is at least as high as the tile: terrain blockers only block
		// when the blocker is NOT strictly lower than the viewer.
		return fg.BlocksLOS != nil && fg.BlocksLOS(x, y) && interElev >= viewerElev
	}
	return fg.BlocksLOS != nil && fg.BlocksLOS(x, y)
}
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
