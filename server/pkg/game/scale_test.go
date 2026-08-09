package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/movement"
)

// TestMapScalePacingTargets verifies the four pacing invariants from issue #45:
//
//  1. Cross-map traversal (long axis) ≤ 240 s.
//  2. PvP first-contact ≤ 120 s (spawns at top/bottom of portrait map).
//  3. Commander vision covers ≥ 25% of the long axis.
//  4. Starter roster occupies ≥ 0.4% of map area.
//
// This is a pure constant-calculation test — no GameSession needed. It runs
// in microseconds and locks down the issue #45 fix against future regressions
// in DefaultMapWidth/Height, defaultCombatUnitSpeed, or fog.VisionRadiusTiles.
//
// If you change any of those constants, this test will tell you whether the
// new values still hit the pacing targets.
func TestMapScalePacingTargets(t *testing.T) {
	// --- Setup: compute the effective speed from the configured constants. ---
	// defaultCombatUnitSpeed picks the long axis internally; here we replicate
	// the math so a regression in either the constant or the formula fails
	// this test, not just the constant.
	speed := defaultCombatUnitSpeed(DefaultMapWidth, DefaultMapHeight)
	effPerTickTiles := fixed.ToFloat(speed / movement.PositionDivisor) // tiles advanced per tick
	effPerSecondTiles := effPerTickTiles * float64(ServerTicksPerSecond)

	// --- Long axis (the actual traversal axis in PvP). ---
	longAxis := DefaultMapHeight
	if DefaultMapWidth > DefaultMapHeight {
		longAxis = DefaultMapWidth
	}

	t.Logf("config: map=%dx%d  longAxis=%d  speed=%d fixed  eff=%.4f tiles/sec",
		DefaultMapWidth, DefaultMapHeight, longAxis, speed, effPerSecondTiles)

	// --- Invariant 1: cross-map traversal on long axis ≤ 240 s. ---
	crossMapSeconds := float64(longAxis) / effPerSecondTiles
	t.Logf("invariant 1 (cross-map time): %.1f s (ceiling 240)", crossMapSeconds)
	if crossMapSeconds > 240 {
		t.Errorf("cross-map time on long axis = %.1fs, want ≤ 240s", crossMapSeconds)
	}

	// --- Invariant 2: PvP first-contact ≤ 120 s. ---
	// Spawns placed at (w/2, 3) and (w/2, h-4) by pkg/tilemap/generate.go.
	// PvP gap on long axis = (h-4) - 3 = h-7 tiles. Both players move, so
	// closing speed is 2× single-unit speed.
	pvpGap := float64(DefaultMapHeight - 7)
	firstContactSeconds := pvpGap / (2 * effPerSecondTiles)
	t.Logf("invariant 2 (PvP first-contact): gap=%.0f tiles  time=%.1f s (ceiling 120)",
		pvpGap, firstContactSeconds)
	if firstContactSeconds > 120 {
		t.Errorf("PvP first-contact = %.1fs, want ≤ 120s", firstContactSeconds)
	}

	// --- Invariant 3: commander vision ≥ 25% of long axis. ---
	// Importing pkg/fog would create an import cycle (game depends on fog
	// transitively); the value is a constant duplicated here as a sentinel.
	// If fog.VisionRadiusTiles changes, update this constant to match.
	const commanderVisionTiles = 12
	visionRatio := float64(commanderVisionTiles) / float64(longAxis)
	t.Logf("invariant 3 (vision coverage): %d / %d = %.1f%% (floor 25%%)",
		commanderVisionTiles, longAxis, visionRatio*100)
	if visionRatio < 0.25 {
		t.Errorf("commander vision = %.1f%% of long axis, want ≥ 25%%", visionRatio*100)
	}

	// --- Invariant 4: starter roster ≥ 0.4% of map area. ---
	// Starter roster = 1 commander + InitialTeamCombatUnits = 6 units.
	// Each occupies ~1 tile. Map area = w * h.
	starterRoster := uint32(1) + InitialTeamCombatUnits
	mapArea := uint32(DefaultMapWidth) * uint32(DefaultMapHeight)
	rosterRatio := float64(starterRoster) / float64(mapArea)
	t.Logf("invariant 4 (roster fill): %d units / %d tiles = %.2f%% (floor 0.4%%)",
		starterRoster, mapArea, rosterRatio*100)
	if rosterRatio < 0.004 {
		t.Errorf("starter roster = %.2f%% of map area, want ≥ 0.4%%", rosterRatio*100)
	}
}

// TestDefaultCombatUnitSpeedUsesLongAxis is a focused regression test for
// the axis-aware fix in defaultCombatUnitSpeed. For a portrait map (w < h),
// speed should match what (h) would have produced under the old formula —
// not what (w) produces. This catches the specific issue #45 regression
// where callers passed only the width.
func TestDefaultCombatUnitSpeedUsesLongAxis(t *testing.T) {
	// Portrait: width < height. Speed should match height.
	portraitSpeed := defaultCombatUnitSpeed(30, 48)
	heightOnlySpeed := defaultCombatUnitSpeed(48, 48) // pretend 48×48 square
	if portraitSpeed != heightOnlySpeed {
		t.Errorf("portrait (30×48) speed = %d, want %d (= 48×48 square, long-axis used)",
			portraitSpeed, heightOnlySpeed)
	}

	// Landscape: width > height. Speed should match width.
	landscapeSpeed := defaultCombatUnitSpeed(48, 30)
	if landscapeSpeed != heightOnlySpeed {
		t.Errorf("landscape (48×30) speed = %d, want %d (= 48×48 square, long-axis used)",
			landscapeSpeed, heightOnlySpeed)
	}

	// Different long axis → different speed. (40×40) uses long=40; (30,50)
	// uses long=50; the larger map is faster per tick to hit the same
	// wall-clock target.
	speed40 := defaultCombatUnitSpeed(40, 40)
	speed50 := defaultCombatUnitSpeed(30, 50)
	if speed40 == speed50 {
		t.Errorf("(40×40) speed = (30×50) speed = %d — expected different (long axes 40 vs 50)", speed40)
	}
}
