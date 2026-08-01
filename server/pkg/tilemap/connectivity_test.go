package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

// connectivity_test.go — regression tests for the generator connectivity
// guarantee. The solo-match stalemate (93% of seeds) traced to Deep rivers
// bisecting the map into regions a unit's flow field could not reach.
// TestConnectivity_AllSeeds would have caught the root cause directly.

const connectivityTestSeeds = 30

// TestConnectivity_AllSeeds asserts that GenerateMap produces a single
// connected component for BOTH Light and Heavy profiles across seeds 1–30.
// This is the headline stalemate regression test.
func TestConnectivity_AllSeeds(t *testing.T) {
	profiles := component.StandardMovementProfiles()
	for seed := int64(1); seed <= connectivityTestSeeds; seed++ {
		gm := GenerateMap(48, 96, seed)
		for i, p := range profiles {
			if !gm.ConnectedFor(p) {
				t.Errorf("seed %d: profile %d (%s) not fully connected — disconnected regions exist",
					seed, i, profileName(i))
			}
		}
	}
}

// TestConnectivity_Repair builds a map with a known bisecting Deep river and
// asserts RepairConnectivity reconnects both profiles.
func TestConnectivity_Repair(t *testing.T) {
	// 7-wide, 7-tall map. Column x=3 is a vertical Deep river wall, splitting
	// the map into left (x=0..2) and right (x=4..6) halves of Plain.
	gm := NewTestMap(7, 7, func(x, y int32) component.TerrainType {
		if x == 3 {
			return component.TerrainDeep
		}
		return component.TerrainPlain
	})

	profiles := component.StandardMovementProfiles()

	// Precondition: both profiles are disconnected by the river.
	if gm.ConnectedFor(profiles[0]) {
		t.Fatal("Light profile should be disconnected by bisecting river")
	}
	if gm.ConnectedFor(profiles[1]) {
		t.Fatal("Heavy profile should be disconnected by bisecting river")
	}

	// Heavy repair places Bridges at Deep boundary tiles — reconnects both
	// sides for both profiles (Bridge is passable for Light and Heavy).
	if !RepairConnectivity(gm, profiles) {
		t.Fatal("RepairConnectivity returned false — could not reconnect map")
	}
	for i, p := range profiles {
		if !gm.ConnectedFor(p) {
			t.Errorf("after repair: profile %d (%s) still disconnected", i, profileName(i))
		}
	}

	// The separator column must now contain at least one non-Deep crossing.
	crossings := 0
	for y := int32(0); y < gm.Height; y++ {
		tt := gm.TileAt(3, y).TerrainType
		if tt == component.TerrainBridge || tt == component.TerrainShallow {
			crossings++
		}
	}
	if crossings == 0 {
		t.Error("repair placed no crossings on the bisecting river column")
	}
}

// TestConnectivity_LightFord verifies that when only the Light profile is
// disconnected and Shallow is the natural crossing, repair places Shallow
// fords (not Bridges) — keeping Heavy routes constrained.
func TestConnectivity_LightFord(t *testing.T) {
	// Same bisecting river, but pre-place a Bridge so Heavy is already
	// connected (Bridge is Heavy-passable). Light is also connected via the
	// bridge, so this case asserts repair is a no-op on already-connected
	// profiles. To exercise Light-only repair we use a Shallow-impassable
	// barrier for Heavy only: a Deep river with no bridge, then call
	// repair targeting just the Light profile.
	gm := NewTestMap(7, 7, func(x, y int32) component.TerrainType {
		if x == 3 {
			return component.TerrainDeep
		}
		return component.TerrainPlain
	})
	light := component.StandardMovementProfiles()[0]
	repairForProfile(gm, light)

	if !gm.ConnectedFor(light) {
		t.Fatal("Light profile still disconnected after Light-targeted repair")
	}
	// Light repair must use Shallow fords.
	hasShallow := false
	for y := int32(0); y < gm.Height; y++ {
		if gm.TileAt(3, y).TerrainType == component.TerrainShallow {
			hasShallow = true
			break
		}
	}
	if !hasShallow {
		t.Error("Light-targeted repair did not place any Shallow fords")
	}
}

// TestConnectivity_AlreadyConnected is a no-op: a fully passable map is
// returned unchanged.
func TestConnectivity_AlreadyConnected(t *testing.T) {
	gm := NewGameMap(6, 6) // all Plain
	profiles := component.StandardMovementProfiles()
	if !RepairConnectivity(gm, profiles) {
		t.Fatal("RepairConnectivity returned false on a fully-passable map")
	}
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			if tt := gm.TileAt(x, y).TerrainType; tt != component.TerrainPlain {
				t.Errorf("tile (%d,%d) = %d, want Plain — repair should not modify a connected map", x, y, tt)
			}
		}
	}
}

func profileName(idx int) string {
	if idx == 1 {
		return "Heavy"
	}
	return "Light"
}
