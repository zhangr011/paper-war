package pathfinding

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// standardProfiles returns the real Light/Heavy profiles so cliff tests run
// against the actual game movement table (Ramp cost = 1 for both).
func standardProfiles() []*component.MovementProfile {
	return component.StandardMovementProfiles()
}

const unreachable = ^uint32(0)

// reachable reports whether the flow field (computed with profile, targeting
// tx,ty) assigns a finite integrated cost to (x,y) — i.e. there is a walkable
// edge-respecting path from (x,y) to the target.
func reachable(ff *FlowField, x, y int32) bool {
	return ff.Costs[y*ff.Width+x] != unreachable
}

// TestCliffTwoTierBlocks asserts a |Δelevation| ≥ 2 step between two Plain
// tiles is impassable for every standard ground profile when neither tile is a
// Ramp. Phase 1 cliff rule (terrain-starcraft-plan.md §1).
func TestCliffTwoTierBlocks(t *testing.T) {
	for _, p := range standardProfiles() {
		// 3x2 map, all Plain. Left column elevation 0, right column elevation 2.
		gm := tilemap.NewGameMap(2, 3)
		for y := int32(0); y < 3; y++ {
			gm.TileAt(0, y).Elevation = 0
			gm.TileAt(1, y).Elevation = 2
		}
		// Target on the right column, read cost on the left. The only edges
		// joining the columns are 2-tier cliffs → must be unreachable.
		ff := Compute(gm, 1, 1, p, 0)
		if reachable(ff, 0, 1) {
			t.Fatalf("%s profile crossed a 2-tier cliff with no ramp (cost=%d)",
				profileName(p), ff.Costs[1*2+0])
		}
	}
}

// TestRampPermitsCliffCrossing asserts that placing a TerrainRamp on one side
// of the 2-tier step opens it for both profiles.
func TestRampPermitsCliffCrossing(t *testing.T) {
	for _, p := range standardProfiles() {
		gm := tilemap.NewGameMap(2, 3)
		for y := int32(0); y < 3; y++ {
			gm.TileAt(1, y).Elevation = 2
		}
		// Make the target tile itself a Ramp at elevation 2. The step from the
		// low Plain (elev 0) onto the Ramp (elev 2) is a delta-2 cliff that the
		// Ramp explicitly permits.
		gm.SetTerrain(1, 1, component.TerrainRamp)
		gm.TileAt(1, 1).Elevation = 2
		ff := Compute(gm, 1, 1, p, 0)
		if !reachable(ff, 0, 1) {
			t.Fatalf("%s profile could not cross a 2-tier cliff even though the "+
				"target tile is a Ramp", profileName(p))
		}
	}
}

// TestOneTierDeltaAlwaysPassable asserts a |Δelevation| == 1 step is walkable
// regardless of Ramp presence (it is never a cliff).
func TestOneTierDeltaAlwaysPassable(t *testing.T) {
	for _, p := range standardProfiles() {
		gm := tilemap.NewGameMap(2, 1)
		gm.TileAt(0, 0).Elevation = 0
		gm.TileAt(1, 0).Elevation = 1
		ff := Compute(gm, 1, 0, p, 0)
		if !reachable(ff, 0, 0) {
			t.Fatalf("%s profile could not cross a 1-tier delta (should always "+
				"be passable)", profileName(p))
		}
	}
}

// TestCliffHillsSpawnConnected asserts that after the cliff rule is in force,
// both standard profiles can still path between the runtime clash spawns on
// ClashHills (the ramp-authored ridge map). Regression guard for Task 5.
func TestCliffHillsSpawnConnected(t *testing.T) {
	gm := tilemap.LoadClashMap("hills")
	if gm == nil {
		t.Fatal("ClashHills map not registered")
	}
	// Runtime clash spawns (main.go: mw/2 ± 0 on x, mh/2 ± 4 on y → (16,12)/(16,20)).
	spawnA := [2]int32{16, 12}
	spawnB := [2]int32{16, 20}
	for _, p := range standardProfiles() {
		ff := Compute(gm, spawnA[0], spawnA[1], p, 0)
		if !reachable(ff, spawnB[0], spawnB[1]) {
			t.Errorf("%s profile cannot path spawn-to-spawn on ClashHills "+
				"(cliff rule disconnected the map)", profileName(p))
		}
	}
}

func profileName(p *component.MovementProfile) string {
	if p.ID == 1 {
		return "Heavy"
	}
	return "Light"
}
