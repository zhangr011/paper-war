package creep

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// standardProfile returns the Light movement profile for walkability gating.
func standardProfile() *component.MovementProfile {
	for _, p := range component.StandardMovementProfiles() {
		return p // Light is first
	}
	return nil
}

// newWorldWithCommander builds a minimal ECS world with a single alive
// commander of the given faction at tile (cx,cy), and returns the world +
// the standard movement profile.
func newWorldWithCommander(t *testing.T, faction uint8, cx, cy int32) (*ecs.World, *component.MovementProfile) {
	t.Helper()
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.CommanderComponent{}, cmdPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)

	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: int64(cx) << 12, Y: int64(cy) << 12})
	cmdPool.Add(e, component.CommanderComponent{IsAlive: true})
	ownerPool.Add(e, component.OwnerComponent{Faction: faction})
	healthPool.Add(e, component.HealthComponent{HP: 100})
	return w, standardProfile()
}

// TestCreepSpreadsFromCommander asserts that after enough spread ticks the
// tiles orthogonally adjacent to a commander source are claimed, out to the
// MaxDistance cap, and that an unwalkable (Deep) neighbor is NOT claimed.
func TestCreepSpreadsFromCommander(t *testing.T) {
	const w, h = int32(11), int32(11)
	gm := tilemap.NewTestMap(w, h, func(x, y int32) component.TerrainType {
		return component.TerrainPlain
	})
	// Block one neighbor of the source with Deep water (cost 0 → impassable).
	gm.SetTerrain(5, 4, component.TerrainDeep)

	world, profile := newWorldWithCommander(t, component.FactionPlayer, 5, 5)
	sys := &CreepSystem{Gm: gm, World: world, Profile: profile}

	// Tick 0 is a spread tick (0 % 5 == 0): seeds only the source.
	sys.Tick(world, 0)
	if got := gm.TileAt(5, 5).CreepOwner; got != 1 {
		t.Fatalf("source tile CreepOwner = %d, want 1 (faction player)", got)
	}
	// Immediately after the first tick, distance-1 neighbors should be claimed
	// because the BFS runs to MaxDistance within a single spread tick.
	for _, n := range [][2]int32{{6, 5}, {4, 5}, {5, 6}} {
		if got := gm.TileAt(n[0], n[1]).CreepOwner; got != 1 {
			t.Errorf("neighbor (%d,%d) CreepOwner = %d, want 1", n[0], n[1], got)
		}
	}
	// The Deep-water neighbor must remain unclaimed.
	if got := gm.TileAt(5, 4).CreepOwner; got != 0 {
		t.Errorf("unwalkable neighbor (5,4) CreepOwner = %d, want 0 (blocked)", got)
	}

	// A tile at distance MaxDistance+1 must NOT be claimed (radius cap).
	// (5,5) -> (5,6) -> ... distance 6 reaches (5,11) out of bounds; use the
	// horizontal axis: distance 7 = (12,5) out of bounds. Use distance-7 on a
	// longer axis by checking (5+MaxDistance+1, 5) is unclaimed. Map is only
	// 11 wide so pick a guaranteed-out-of-range tile via a second check below.
}

// TestCreepRadiusCap verifies the MaxDistance boundary: a tile exactly
// MaxDistance away IS claimed, one beyond is NOT.
func TestCreepRadiusCap(t *testing.T) {
	const w, h = int32(20), int32(3)
	gm := tilemap.NewTestMap(w, h, func(x, y int32) component.TerrainType {
		return component.TerrainPlain
	})
	world, profile := newWorldWithCommander(t, component.FactionPlayer, 1, 1)
	sys := &CreepSystem{Gm: gm, World: world, Profile: profile}

	sys.Tick(world, 0)
	// Along y=1, tiles (2,1)..(1+MaxDistance,1) should be claimed.
	if got := gm.TileAt(1+MaxDistance, 1).CreepOwner; got != 1 {
		t.Errorf("tile at MaxDistance (%d,1) CreepOwner = %d, want 1", 1+MaxDistance, got)
	}
	if got := gm.TileAt(1 + MaxDistance + 1, 1).CreepOwner; got != 0 {
		t.Errorf("tile beyond MaxDistance (%d,1) CreepOwner = %d, want 0", 1+MaxDistance+1, got)
	}
}

// TestCreepTwoFactionsBothSpread places two faction sources on one map and
// verifies each owns its own neighborhood.
func TestCreepTwoFactionsBothSpread(t *testing.T) {
	const w, h = int32(15), int32(5)
	gm := tilemap.NewTestMap(w, h, func(x, y int32) component.TerrainType {
		return component.TerrainPlain
	})
	em := ecs.NewEntityManager()
	eworld := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	eworld.RegisterPool(component.PositionComponent{}, posPool)
	eworld.RegisterPool(component.CommanderComponent{}, cmdPool)
	eworld.RegisterPool(component.OwnerComponent{}, ownerPool)
	eworld.RegisterPool(component.HealthComponent{}, healthPool)

	for _, c := range []struct {
		faction uint8
		x, y    int32
	}{
		{component.FactionPlayer, 2, 2},
		{component.FactionEnemy, 12, 2},
	} {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{X: int64(c.x) << 12, Y: int64(c.y) << 12})
		cmdPool.Add(e, component.CommanderComponent{IsAlive: true})
		ownerPool.Add(e, component.OwnerComponent{Faction: c.faction})
		healthPool.Add(e, component.HealthComponent{HP: 100})
	}

	sys := &CreepSystem{Gm: gm, World: eworld, Profile: standardProfile()}
	sys.Tick(eworld, 0)

	if got := gm.TileAt(2, 2).CreepOwner; got != 1 {
		t.Errorf("player source CreepOwner = %d, want 1", got)
	}
	if got := gm.TileAt(12, 2).CreepOwner; got != 2 {
		t.Errorf("enemy source CreepOwner = %d, want 2", got)
	}
	if got := gm.TileAt(3, 2).CreepOwner; got != 1 {
		t.Errorf("player neighbor CreepOwner = %d, want 1", got)
	}
	if got := gm.TileAt(11, 2).CreepOwner; got != 2 {
		t.Errorf("enemy neighbor CreepOwner = %d, want 2", got)
	}
}

// TestCreepRecedesWhenSourceDies: a dead commander (HP<=0) contributes no
// creep, so its tile and neighborhood stay unclaimed.
func TestCreepRecedesWhenSourceDies(t *testing.T) {
	const w, h = int32(7), int32(7)
	gm := tilemap.NewTestMap(w, h, func(x, y int32) component.TerrainType {
		return component.TerrainPlain
	})
	em := ecs.NewEntityManager()
	dworld := ecs.NewWorld(em)
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	dworld.RegisterPool(component.PositionComponent{}, posPool)
	dworld.RegisterPool(component.CommanderComponent{}, cmdPool)
	dworld.RegisterPool(component.OwnerComponent{}, ownerPool)
	dworld.RegisterPool(component.HealthComponent{}, healthPool)

	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: 3 << 12, Y: 3 << 12})
	cmdPool.Add(e, component.CommanderComponent{IsAlive: true})
	ownerPool.Add(e, component.OwnerComponent{Faction: component.FactionPlayer})
	healthPool.Add(e, component.HealthComponent{HP: 0}) // dead

	sys := &CreepSystem{Gm: gm, World: dworld, Profile: standardProfile()}
	sys.Tick(dworld, 0)

	if got := gm.TileAt(3, 3).CreepOwner; got != 0 {
		t.Errorf("dead-source tile CreepOwner = %d, want 0", got)
	}
	if got := gm.TileAt(4, 3).CreepOwner; got != 0 {
		t.Errorf("dead-source neighbor CreepOwner = %d, want 0", got)
	}
}
