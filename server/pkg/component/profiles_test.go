package component

import (
	"testing"
)

func TestStandardMovementProfilesHasTwo(t *testing.T) {
	profiles := StandardMovementProfiles()
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
}

func TestLightProfileCanCrossShallow(t *testing.T) {
	profiles := StandardMovementProfiles()
	light := profiles[0]
	if light.TerrainCosts[TerrainShallow] == 0 {
		t.Error("Light profile should be able to cross shallow water (cost > 0)")
	}
	if light.TerrainCosts[TerrainDeep] != 0 {
		t.Error("Light profile should not cross deep water")
	}
}

func TestHeavyProfileCannotCrossShallow(t *testing.T) {
	profiles := StandardMovementProfiles()
	heavy := profiles[1]
	if heavy.TerrainCosts[TerrainShallow] != 0 {
		t.Error("Heavy profile should NOT be able to cross shallow water")
	}
	if heavy.TerrainCosts[TerrainDeep] != 0 {
		t.Error("Heavy profile should not cross deep water")
	}
}

func TestBothProfilesCanUseRoads(t *testing.T) {
	profiles := StandardMovementProfiles()
	for i, p := range profiles {
		if p.TerrainCosts[TerrainRoad] != 1 {
			t.Errorf("profile %d: road cost = %d, want 1", i, p.TerrainCosts[TerrainRoad])
		}
	}
}

func TestBothProfilesCanUseBridges(t *testing.T) {
	profiles := StandardMovementProfiles()
	for i, p := range profiles {
		if p.TerrainCosts[TerrainBridge] != 1 {
			t.Errorf("profile %d: bridge cost = %d, want 1", i, p.TerrainCosts[TerrainBridge])
		}
	}
}

func TestHeavyProfileSlowerOnRoughTerrain(t *testing.T) {
	profiles := StandardMovementProfiles()
	light := profiles[0]
	heavy := profiles[1]
	// Heavy should be slower on forest, hill, swamp
	for _, terrain := range []TerrainType{TerrainForest, TerrainHill, TerrainSwamp} {
		if heavy.TerrainCosts[terrain] <= light.TerrainCosts[terrain] {
			t.Errorf("Heavy cost on terrain %d = %d, should be > Light cost %d",
				terrain, heavy.TerrainCosts[terrain], light.TerrainCosts[terrain])
		}
	}
}

func TestArmorTypeToProfileID(t *testing.T) {
	tests := []struct {
		armor   ArmorType
		wantID  uint8
	}{
		{ArmorLight, 0},
		{ArmorHeavy, 1},
	}
	for _, tt := range tests {
		got := ArmorTypeToProfileID(tt.armor)
		if got != tt.wantID {
			t.Errorf("ArmorTypeToProfileID(%d) = %d, want %d", tt.armor, got, tt.wantID)
		}
	}
}

// TestBlocksLOS verifies the terrains the fog raycast treats as sight blockers
// (issue #55 phase 2/3). Forest/Wall/Rock block; Brush (concealment only) and
// everything else do not.
func TestBlocksLOS(t *testing.T) {
	blocking := []TerrainType{TerrainForest, TerrainWall, TerrainRock}
	clear := []TerrainType{TerrainPlain, TerrainRoad, TerrainHill, TerrainBrush, TerrainSwamp}
	for _, tt := range blocking {
		if !BlocksLOS(tt) {
			t.Errorf("BlocksLOS(%d) = false, want true", tt)
		}
	}
	for _, tt := range clear {
		if BlocksLOS(tt) {
			t.Errorf("BlocksLOS(%d) = true, want false", tt)
		}
	}
}

// TestRockPassableForBothProfiles — Rock is heavy cover but must not cut Heavy
// routes, so it stays passable (slow) for both profiles. Issue #55 phase 3.
func TestRockPassableForBothProfiles(t *testing.T) {
	profiles := StandardMovementProfiles()
	for _, p := range profiles {
		if p.TerrainCosts[TerrainRock] == 0 {
			t.Errorf("profile %d: Rock is impassable (cost 0) — would risk Heavy connectivity", p.ID)
		}
	}
}
