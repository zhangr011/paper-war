package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// TestEdgeWalkableUphillCost — the ADR-0034 uphill assault tax: each
// elevation band climbed costs +UphillStepCost on top of destination terrain
// cost; downhill and level steps are free; Ramp tiles are exempt (the
// channeled route). Cost only — never blocks.
func TestEdgeWalkableUphillCost(t *testing.T) {
	light := component.StandardMovementProfiles()[0] // Hill cost 3

	m := NewGameMap(8, 8)
	// (1,1) plain e0 — (2,1) Hill e1 — (3,1) Hill e1 — (4,1) plain e0
	// (2,2) Ramp e1 (the channeled step up)
	m.SetTerrain(2, 1, component.TerrainHill)
	m.TileAt(2, 1).Elevation = 1
	m.SetTerrain(3, 1, component.TerrainHill)
	m.TileAt(3, 1).Elevation = 1
	m.SetTerrain(2, 2, component.TerrainRamp)
	m.TileAt(2, 2).Elevation = 1

	cases := []struct {
		name          string
		x1, y1, x2, y2 int32
		wantOK        bool
		wantCost      uint8
	}{
		{"uphill plain→hill(e1): hill 3 + tax 1", 1, 1, 2, 1, true, 3 + UphillStepCost},
		{"level hill→hill: no tax", 2, 1, 3, 1, true, 3},
		{"downhill hill→plain: no tax", 2, 1, 1, 1, true, 1},
		{"ramp step up: exempt", 1, 2, 2, 2, true, 1},
		{"ramp→hill top: exempt", 2, 2, 2, 1, true, 3},
	}
	for _, c := range cases {
		ok, cost := m.EdgeWalkable(c.x1, c.y1, c.x2, c.y2, light)
		if ok != c.wantOK || cost != c.wantCost {
			t.Errorf("%s: EdgeWalkable(%d,%d→%d,%d) = (%v,%d), want (%v,%d)",
				c.name, c.x1, c.y1, c.x2, c.y2, ok, cost, c.wantOK, c.wantCost)
		}
	}
}
