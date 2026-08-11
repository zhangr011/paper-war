package tilemap

import (
	"fmt"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
)

func TestNewGameMap(t *testing.T) {
	gm := NewGameMap(4, 4)
	if gm.Width != 4 || gm.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", gm.Width, gm.Height)
	}
	tile := gm.TileAt(0, 0)
	if tile.TerrainType != component.TerrainPlain {
		t.Errorf("default terrain = %d, want plain", tile.TerrainType)
	}
}

func TestSetTerrain(t *testing.T) {
	gm := NewGameMap(4, 4)
	gm.SetTerrain(1, 1, component.TerrainForest)
	tile := gm.TileAt(1, 1)
	if tile.TerrainType != component.TerrainForest {
		t.Errorf("terrain = %d, want forest", tile.TerrainType)
	}
}

func TestCostAt(t *testing.T) {
	profile := component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainForest] = 2
	profile.TerrainCosts[component.TerrainDeep] = 0

	gm := NewGameMap(4, 4)
	gm.SetTerrain(2, 2, component.TerrainForest)
	gm.SetTerrain(3, 3, component.TerrainDeep)

	if gm.CostAt(0, 0, &profile) != 1 {
		t.Errorf("plain cost = %d, want 1", gm.CostAt(0, 0, &profile))
	}
	if gm.CostAt(2, 2, &profile) != 2 {
		t.Errorf("forest cost = %d, want 2", gm.CostAt(2, 2, &profile))
	}
	if gm.CostAt(3, 3, &profile) != 0 {
		t.Errorf("deep cost = %d, want 0", gm.CostAt(3, 3, &profile))
	}
}

func TestOutOfBounds(t *testing.T) {
	gm := NewGameMap(4, 4)
	tile := gm.TileAt(-1, 0)
	if tile != nil {
		t.Error("out of bounds should return nil")
	}
}

func TestNewTestMap(t *testing.T) {
	gm := NewTestMap(5, 5, func(x, y int32) component.TerrainType {
		if x == 2 {
			return component.TerrainWall
		}
		return component.TerrainPlain
	})
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	if gm.CostAt(2, 2, p) != 0 {
		t.Error("wall should be impassable")
	}
	if gm.CostAt(0, 0, p) != 1 {
		t.Error("plain should be passable")
	}
}

// ---------------------------------------------------------------------------
// Property-based tests for the procedural map generator
// Tests run 100 maps with different seeds and validate 13 invariants.
// ---------------------------------------------------------------------------

func TestPropertyDimensions(t *testing.T) {
	for seed, gm := range testMaps {
		if gm.Width != 48 || gm.Height != 96 {
			t.Errorf("seed %d: dimensions = %dx%d, want 48x96", seed, gm.Width, gm.Height)
		}
	}
}

func TestPropertyNoNilTiles(t *testing.T) {
	for seed, gm := range testMaps {
		for y := int32(0); y < gm.Height; y++ {
			for x := int32(0); x < gm.Width; x++ {
				tile := gm.TileAt(x, y)
				if tile == nil {
					t.Fatalf("seed %d: nil tile at (%d,%d)", seed, x, y)
				}
			}
		}
	}
}

func TestPropertySpawnCount(t *testing.T) {
	for seed, gm := range testMaps {
		if len(gm.Spawns) != 2 {
			t.Errorf("seed %d: spawn count = %d, want 2", seed, len(gm.Spawns))
		}
	}
}

func TestPropertySpawnTerrain(t *testing.T) {
	for seed, gm := range testMaps {
		for i, sp := range gm.Spawns {
			tile := gm.TileAt(sp[0], sp[1])
			if tile == nil {
				t.Errorf("seed %d: spawn %d at (%d,%d) is nil", seed, i, sp[0], sp[1])
				continue
			}
			if tile.TerrainType != component.TerrainPlain {
				t.Errorf("seed %d: spawn %d terrain = %d, want Plain", seed, i, tile.TerrainType)
			}
			// Verify 6x6 clearing is plain
			for dy := int32(-3); dy <= 3; dy++ {
				for dx := int32(-3); dx <= 3; dx++ {
					ct := gm.TileAt(sp[0]+dx, sp[1]+dy)
					if ct != nil && ct.TerrainType != component.TerrainPlain {
						t.Errorf("seed %d: spawn %d clearing at (%d,%d) is %d, want Plain",
							seed, i, sp[0]+dx, sp[1]+dy, ct.TerrainType)
					}
				}
			}
		}
	}
}

func TestPropertyForestCoverage(t *testing.T) {
	for seed, gm := range testMaps {
		total := int32(0)
		hills := int32(0)
		water := int32(0)
		forest := int32(0)
		for _, tile := range gm.Tiles {
			total++
			switch tile.TerrainType {
			case component.TerrainHill:
				hills++
			case component.TerrainDeep:
				water++
			case component.TerrainForest:
				forest++
			}
		}
		eligible := total - hills - water
		if eligible == 0 {
			t.Errorf("seed %d: no eligible tiles for forest", seed)
			continue
		}
		coverage := float64(forest) / float64(eligible)
		// Design target is grass-dominant with scattered tree clusters (~15%
		// of eligible tiles), matching design/map.png.  Allow a wide band so
		// noise-driven variation across seeds stays in range.
		if coverage < 0.08 || coverage > 0.25 {
			t.Errorf("seed %d: forest coverage = %.2f, want 0.08-0.25", seed, coverage)
		}
	}
}

func TestPropertyWaterCoverage(t *testing.T) {
	for seed, gm := range testMaps {
		water := int32(0)
		total := int32(len(gm.Tiles))
		for _, tile := range gm.Tiles {
			if tile.TerrainType == component.TerrainDeep {
				water++
			}
		}
		coverage := float64(water) / float64(total)
		if coverage < 0.005 || coverage > 0.08 {
			t.Errorf("seed %d: water coverage = %.3f, want 0.005-0.08", seed, coverage)
		}
	}
}

func TestPropertyHillCoverage(t *testing.T) {
	for seed, gm := range testMaps {
		hills := int32(0)
		total := int32(len(gm.Tiles))
		for _, tile := range gm.Tiles {
			if tile.TerrainType == component.TerrainHill {
				hills++
			}
		}
		coverage := float64(hills) / float64(total)
		if coverage < 0.05 || coverage > 0.25 {
			t.Errorf("seed %d: hill coverage = %.3f, want 0.05-0.25", seed, coverage)
		}
	}
}

// TestPropertyScatterCoverage asserts the environmental scatter pass (issue #55
// phase 3) places both Rock (on hills) and Brush (on plains) on every fixture
// map, and that rocks/brush stay a small minority so the map still reads as
// grassland-with-cover rather than rubble.
func TestPropertyScatterCoverage(t *testing.T) {
	for seed, gm := range testMaps {
		var rock, brush int32
		total := int32(len(gm.Tiles))
		for _, tile := range gm.Tiles {
			switch tile.TerrainType {
			case component.TerrainRock:
				rock++
			case component.TerrainBrush:
				brush++
			}
		}
		if rock == 0 {
			t.Errorf("seed %d: no Rock placed by scatter pass", seed)
		}
		if brush == 0 {
			t.Errorf("seed %d: no Brush placed by scatter pass", seed)
		}
		if float64(rock)/float64(total) > 0.10 {
			t.Errorf("seed %d: rock coverage %.3f too high", seed, float64(rock)/float64(total))
		}
	}
}

func TestPropertyStrongholdCount(t *testing.T) {
	for seed, gm := range testMaps {
		// Strongholds are generator-recorded specs now (entities, not terrain — #54).
		count := len(gm.Strongholds)
		if count < 1 || count > 3 { // strongholdMax = 3
			t.Errorf("seed %d: stronghold specs = %d, want 1-3", seed, count)
		}
	}
}

func TestPropertyLightConnectivity(t *testing.T) {
	profiles := component.StandardMovementProfiles()
	light := profiles[0]
	for seed, gm := range testMaps {
		if len(gm.Spawns) < 2 {
			t.Fatalf("seed %d: not enough spawns", seed)
		}
		if !isConnected(gm, gm.Spawns[0], gm.Spawns[1], light) {
			t.Errorf("seed %d: Light profile has no path between spawns", seed)
		}
	}
}

func TestPropertyHeavyConnectivity(t *testing.T) {
	profiles := component.StandardMovementProfiles()
	heavy := profiles[1]
	for seed, gm := range testMaps {
		if len(gm.Spawns) < 2 {
			t.Fatalf("seed %d: not enough spawns", seed)
		}
		if !isConnected(gm, gm.Spawns[0], gm.Spawns[1], heavy) {
			t.Errorf("seed %d: Heavy profile has no path between spawns", seed)
		}
	}
}

func TestPropertyObjectiveValid(t *testing.T) {
	for seed, gm := range testMaps {
		if gm.Objective.Type < ObjectiveElimination || gm.Objective.Type > ObjectiveSurvival {
			t.Errorf("seed %d: Objective.Type = %d, want 0-2", seed, gm.Objective.Type)
		}
	}
}

func TestPropertyCaptureHasTarget(t *testing.T) {
	for seed, gm := range testMaps {
		if gm.Objective.Type != ObjectiveCapture {
			continue
		}
		// #54 1B: Capture Target is decoupled from strongholds — it points at
		// the map center, a neutral win point independent of stronghold entities.
		wantX, wantY := gm.Width/2, gm.Height/2
		if gm.Objective.TargetX != wantX || gm.Objective.TargetY != wantY {
			t.Errorf("seed %d: Capture target (%d,%d), want map center (%d,%d)",
				seed, gm.Objective.TargetX, gm.Objective.TargetY, wantX, wantY)
		}
		if gm.Objective.HoldTarget != 300 {
			t.Errorf("seed %d: HoldTarget = %d, want 300", seed, gm.Objective.HoldTarget)
		}
	}
}

func TestPropertyDeterminism(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		m1 := GenerateMap(48, 96, seed)
		m2 := GenerateMap(48, 96, seed)

		// Compare dimensions
		if m1.Width != m2.Width || m1.Height != m2.Height {
			t.Errorf("seed %d: dimensions differ", seed)
			continue
		}
		// Compare tiles
		for i := range m1.Tiles {
			if m1.Tiles[i] != m2.Tiles[i] {
				x := int32(i) % m1.Width
				y := int32(i) / m1.Width
				t.Errorf("seed %d: tile (%d,%d) differs: %+v vs %+v", seed, x, y, m1.Tiles[i], m2.Tiles[i])
				break
			}
		}
		// Compare spawns
		if fmt.Sprint(m1.Spawns) != fmt.Sprint(m2.Spawns) {
			t.Errorf("seed %d: spawns differ: %v vs %v", seed, m1.Spawns, m2.Spawns)
		}
		// Compare objective
		if m1.Objective != m2.Objective {
			t.Errorf("seed %d: objectives differ: %+v vs %+v", seed, m1.Objective, m2.Objective)
		}
	}
}

// ---------------------------------------------------------------------------
// Specific generator behavior tests
// ---------------------------------------------------------------------------

func TestGenerateMapHasWater(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	water := 0
	for _, tile := range gm.Tiles {
		if tile.TerrainType == component.TerrainDeep {
			water++
		}
	}
	if water == 0 {
		t.Error("map has no deep water tiles")
	}
}

func TestGenerateMapHasHills(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	hills := 0
	for _, tile := range gm.Tiles {
		if tile.TerrainType == component.TerrainHill {
			hills++
		}
	}
	if hills == 0 {
		t.Error("map has no hill tiles")
	}
}

func TestGenerateMapHasForest(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	forest := 0
	for _, tile := range gm.Tiles {
		if tile.TerrainType == component.TerrainForest {
			forest++
		}
	}
	if forest == 0 {
		t.Error("map has no forest tiles")
	}
}

// TestGenerateMapStrongholdSpecsPlaced verifies each recorded stronghold spec
// sits on passable, non-hill/non-deep ground (the generator's placement rule).
// Replaces the old "indestructible terrain" test — strongholds are entities
// now (ADR-0023 / #54), so there's no stronghold terrain to check.
func TestGenerateMapStrongholdSpecsPlaced(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	if len(gm.Strongholds) == 0 {
		t.Fatal("expected at least one stronghold spec")
	}
	for _, s := range gm.Strongholds {
		tile := gm.TileAt(s.X, s.Y)
		if tile == nil {
			t.Errorf("stronghold spec (%d,%d) out of bounds", s.X, s.Y)
			continue
		}
		if tile.TerrainType == component.TerrainHill || tile.TerrainType == component.TerrainDeep {
			t.Errorf("stronghold spec (%d,%d) on impassable terrain %d",
				s.X, s.Y, tile.TerrainType)
		}
		if s.Level < 1 || s.Level > 5 {
			t.Errorf("stronghold spec (%d,%d) level = %d, want 1-5", s.X, s.Y, s.Level)
		}
	}
}

func TestGenerateMapHasObjective(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	if gm.Objective.Type < ObjectiveElimination || gm.Objective.Type > ObjectiveSurvival {
		t.Errorf("Objective.Type = %d, want 0-2", gm.Objective.Type)
	}
}

func TestGenerateMapBridgesAreDestructible(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	bridgeCount := 0
	for y := int32(0); y < gm.Height; y++ {
		for x := int32(0); x < gm.Width; x++ {
			tile := gm.TileAt(x, y)
			if tile.TerrainType == component.TerrainBridge {
				bridgeCount++
				if tile.Health != 200 || tile.MaxHealth != 200 {
					t.Errorf("bridge at (%d,%d) Health=%d MaxHealth=%d, want 200/200",
						x, y, tile.Health, tile.MaxHealth)
				}
			}
		}
	}
	if bridgeCount == 0 {
		t.Error("map has no bridge tiles")
	}
}

func TestGenerateMapSurvivalObjectiveValid(t *testing.T) {
	for seed, gm := range testMaps {
		if gm.Objective.Type != ObjectiveSurvival {
			continue
		}
		if gm.Objective.Duration != 3000 {
			t.Errorf("seed %d: Survival Duration = %d, want 3000", seed, gm.Objective.Duration)
		}
	}
}

func TestGenerateMapSeedStored(t *testing.T) {
	gm := GenerateMap(48, 96, 42)
	if gm.Seed != 42 {
		t.Errorf("Seed = %d, want 42", gm.Seed)
	}
}

// TestCostAtForFriendlyCreepDiscount verifies the Phase 4 friendly-creep
// movement discount: a Forest tile (base cost 2 for Light) owned by the
// moving unit's creep faction costs less (×0.7, floored at 1) than the
// neutral cost, while an enemy/neutral observer pays the base cost.
func TestCostAtForFriendlyCreepDiscount(t *testing.T) {
	gm := NewTestMap(3, 3, func(x, y int32) component.TerrainType {
		return component.TerrainForest
	})
	profile := component.StandardMovementProfiles()[0] // Light: Forest cost 2
	gm.TileAt(1, 1).CreepOwner = 1

	base := gm.CostAt(1, 1, profile) // neutral observer — no discount
	if base != 2 {
		t.Fatalf("neutral Forest cost = %d, want 2", base)
	}
	friend := gm.CostAtFor(1, 1, profile, 1) // friendly creep faction 1
	if friend >= base {
		t.Errorf("friendly-creep cost = %d, want < base %d", friend, base)
	}
	if friend < 1 {
		t.Errorf("friendly-creep cost = %d, want floor >= 1", friend)
	}
	// Enemy faction (2) gets no discount on faction-1 creep.
	enemy := gm.CostAtFor(1, 1, profile, 2)
	if enemy != base {
		t.Errorf("enemy-creep cost = %d, want base %d", enemy, base)
	}
}
