package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestLiveCliffRampRouting verifies the Phase 1 cliff/ramp rule
// (EdgeWalkableFor: |Δelevation| ≥ 2 is impassable unless one tile is Ramp)
// drives real pathfinding on a real session cache+map — the same flow field
// MovementSystem reads (movement.go:146-150). This is the integration layer
// above the isolated cliff_test.go: same EdgeWalkableFor rule, but exercised
// through GameSession.ResetWithMap → pathfinding.Cache on an authored map.
//
// A full-height cliff wall (col 8, all rows, Elevation 2) splits the map. With
// a Ramp at (8,8), the west tile (4,8) is reachable from an east-side target
// (12,8) and the flow direction points east toward the crossing. Without the
// Ramp, the wall is uncrossable and the west tile is unreachable (infinite
// cost). Both halves are needed: the Ramp case proves crossing, the no-Ramp
// case proves the cliff actually blocks.
func TestLiveCliffRampRouting(t *testing.T) {
	const wallX = int32(8)
	light := component.StandardMovementProfiles()[0]

	build := func(withRamp bool) *tilemap.GameMap {
		m := tilemap.NewGameMap(16, 16)
		for y := int32(0); y < 16; y++ {
			m.TileAt(wallX, y).Elevation = 2 // cliff wall (Plain at elev 2)
		}
		if withRamp {
			m.SetTerrain(wallX, 8, component.TerrainRamp)
			m.TileAt(wallX, 8).Elevation = 2
		}
		return m
	}

	inf := ^uint32(0)

	// With Ramp: west tile reachable, flow points east toward the crossing.
	gs := NewGameSession()
	gs.ResetWithMap(build(true))
	ff := gs.Cache.Get(12, 8, light, 1) // creepFaction 1 = player unit
	costWest := ff.Costs[8*16+4]
	dir := ff.GetDirection(4, 8)
	if costWest == inf {
		t.Errorf("with Ramp: (4,8) cost=inf — ramp crossing did not make the west side reachable")
	}
	if dir.DX <= 0 {
		t.Errorf("with Ramp: flow at (4,8) DX=%d, want >0 (east, toward the ramp crossing)", dir.DX)
	}

	// Without Ramp: the Δ2 cliff wall makes the west side unreachable.
	gs2 := NewGameSession()
	gs2.ResetWithMap(build(false))
	ff2 := gs2.Cache.Get(12, 8, light, 1)
	if costWest2 := ff2.Costs[8*16+4]; costWest2 != inf {
		t.Errorf("without Ramp: (4,8) cost=%d, want inf (Δ2 cliff should block with no ramp)", costWest2)
	}
}
