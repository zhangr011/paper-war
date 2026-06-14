package network

type Rect struct {
	X, Y, W, H int64 // fixed-point world coordinates
}

func (r Rect) Contains(x, y int64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) Intersects(other Rect) bool {
	return r.X < other.X+other.W && r.X+r.W > other.X &&
		r.Y < other.Y+other.H && r.Y+r.H > other.Y
}

type ClientView struct {
	ClientID  uint32
	ViewRect  Rect
	OwnerID   uint32 // player ID that owns this client
}

type Culler struct {
	views []*ClientView
}

func NewCuller() *Culler {
	return &Culler{}
}

func (c *Culler) AddView(view *ClientView) {
	c.views = append(c.views, view)
}

func (c *Culler) RemoveView(clientID uint32) {
	for i, v := range c.views {
		if v.ClientID == clientID {
			c.views = append(c.views[:i], c.views[i+1:]...)
			return
		}
	}
}

func (c *Culler) UpdateView(clientID uint32, rect Rect) {
	for _, v := range c.views {
		if v.ClientID == clientID {
			v.ViewRect = rect
			return
		}
	}
}

// UnitInfo describes a unit for culling decisions.
type UnitInfo struct {
	EntityID uint32
	X, Y     int64
	SquadID  uint32
	OwnerID  uint32
	IsCommander bool
}

// Cull filters units for a specific client view.
// Returns the entity IDs that should be included in the snapshot.
func Cull(view *ClientView, units []UnitInfo) []uint32 {
	var visible []uint32
	viewRect := view.ViewRect

	for _, u := range units {
		// Always include owned commanders
		if u.IsCommander && u.OwnerID == view.OwnerID {
			visible = append(visible, u.EntityID)
			continue
		}

		// Check if in viewport
		if !viewRect.Contains(u.X, u.Y) {
			continue
		}

		// Include all owned units in viewport
		if u.OwnerID == view.OwnerID {
			visible = append(visible, u.EntityID)
			continue
		}

		// Enemy units in viewport: include (fog of war handled by server separately)
		visible = append(visible, u.EntityID)
	}

	return visible
}

// CullAll generates per-client visible sets.
func (c *Culler) CullAll(units []UnitInfo) map[uint32][]uint32 {
	result := make(map[uint32][]uint32, len(c.views))
	for _, view := range c.views {
		result[view.ClientID] = Cull(view, units)
	}
	return result
}
