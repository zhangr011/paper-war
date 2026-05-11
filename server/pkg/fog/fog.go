package fog

const VisionRadiusTiles = 12

type FogGrid struct {
	Width, Height int32
	Visible       []uint8 // 0=fogged, 1=visible
}

func NewFogGrid(w, h int32) *FogGrid {
	return &FogGrid{
		Width:   w,
		Height:  h,
		Visible: make([]uint8, w*h),
	}
}

func (fg *FogGrid) Clear() {
	for i := range fg.Visible {
		fg.Visible[i] = 0
	}
}

func (fg *FogGrid) Reveal(cx, cy int32) {
	r := int32(VisionRadiusTiles)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			tx, ty := cx+dx, cy+dy
			if tx >= 0 && tx < fg.Width && ty >= 0 && ty < fg.Height {
				fg.Visible[ty*fg.Width+tx] = 1
			}
		}
	}
}

func (fg *FogGrid) IsVisible(tx, ty int32) bool {
	if tx < 0 || tx >= fg.Width || ty < 0 || ty >= fg.Height {
		return false
	}
	return fg.Visible[ty*fg.Width+tx] == 1
}

// Data returns a copy of the visibility grid for network transmission.
func (fg *FogGrid) Data() []byte {
	data := make([]byte, len(fg.Visible))
	copy(data, fg.Visible)
	return data
}
