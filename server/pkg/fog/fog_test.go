package fog

import "testing"

func TestNewFogGrid(t *testing.T) {
	fg := NewFogGrid(10, 10)
	if len(fg.Visible) != 100 {
		t.Errorf("expected 100 tiles, got %d", len(fg.Visible))
	}
	// All tiles start unexplored
	for i, v := range fg.Visible {
		if v != FogUnexplored {
			t.Errorf("tile %d: expected unexplored(0), got %d", i, v)
		}
	}
}

func TestReveal(t *testing.T) {
	fg := NewFogGrid(64, 64)
	fg.Reveal(32, 32)
	if !fg.IsVisible(32, 32) {
		t.Error("center should be visible")
	}
	if !fg.IsCurrentlyVisible(32, 32) {
		t.Error("center should be currently visible")
	}
	if !fg.IsVisible(38, 32) {
		t.Error("tile within radius should be visible")
	}
	if fg.IsVisible(0, 0) {
		t.Error("far tile should not be visible")
	}
}

func TestClear(t *testing.T) {
	fg := NewFogGrid(10, 10)
	fg.Reveal(5, 5)
	// Before clear: tiles are FogVisible
	if !fg.IsCurrentlyVisible(5, 5) {
		t.Error("center should be currently visible before clear")
	}
	fg.Clear()
	// After clear: tiles should be explored, not unexplored
	if fg.IsCurrentlyVisible(5, 5) {
		t.Error("tile should not be currently visible after clear")
	}
	if !fg.IsVisible(5, 5) {
		t.Error("tile should still be explored (not unexplored) after clear")
	}
}

func TestClearPreservesExploredMemory(t *testing.T) {
	fg := NewFogGrid(64, 64)
	// Tick 1: reveal around (32,32)
	fg.Reveal(32, 32)
	// Tick 2: clear + reveal around (10,10)
	fg.Clear()
	fg.Reveal(10, 10)
	// Area around (32,32) should be explored, not unexplored
	if !fg.IsVisible(32, 32) {
		t.Error("previously revealed center should be explored after clear")
	}
	if fg.IsCurrentlyVisible(32, 32) {
		t.Error("previously revealed center should not be currently visible")
	}
	// New area should be currently visible
	if !fg.IsCurrentlyVisible(10, 10) {
		t.Error("new reveal center should be currently visible")
	}
}

func TestIsVisibleBounds(t *testing.T) {
	fg := NewFogGrid(10, 10)
	if fg.IsVisible(-1, 0) {
		t.Error("negative x should be false")
	}
	if fg.IsVisible(0, -1) {
		t.Error("negative y should be false")
	}
	if fg.IsVisible(10, 0) {
		t.Error("x >= width should be false")
	}
}

func TestIsCurrentlyVisibleBounds(t *testing.T) {
	fg := NewFogGrid(10, 10)
	if fg.IsCurrentlyVisible(-1, 0) {
		t.Error("negative x should be false")
	}
	if fg.IsCurrentlyVisible(10, 0) {
		t.Error("x >= width should be false")
	}
}

func TestRevealCircle(t *testing.T) {
	fg := NewFogGrid(64, 64)
	fg.Reveal(32, 32)
	// Corner of bounding box should not be visible (outside circle)
	r := int32(VisionRadiusTiles)
	if fg.IsVisible(32+r, 32+r) {
		t.Error("corner of bounding box should be outside circle")
	}
}

func TestRevealRadius(t *testing.T) {
	fg := NewFogGrid(64, 64)
	fg.RevealRadius(32, 32, 6)
	// Center should be visible
	if !fg.IsCurrentlyVisible(32, 32) {
		t.Error("center should be visible with radius 6")
	}
	// Tile at distance 5 should be visible
	if !fg.IsCurrentlyVisible(37, 32) {
		t.Error("tile at distance 5 should be visible with radius 6")
	}
	// Tile at distance 7 should not be visible
	if fg.IsCurrentlyVisible(39, 32) {
		t.Error("tile at distance 7 should not be visible with radius 6")
	}
}

func TestMultipleRevealSources(t *testing.T) {
	fg := NewFogGrid(64, 64)
	// Two reveal centers far apart
	fg.Reveal(10, 10)
	fg.Reveal(50, 50)
	if !fg.IsCurrentlyVisible(10, 10) {
		t.Error("first center should be visible")
	}
	if !fg.IsCurrentlyVisible(50, 50) {
		t.Error("second center should be visible")
	}
	// Midpoint should not be visible (too far from both)
	if fg.IsCurrentlyVisible(30, 30) {
		t.Error("midpoint should not be visible")
	}
}

func TestExploredNeverLost(t *testing.T) {
	fg := NewFogGrid(64, 64)
	// Reveal, clear, repeat many times — explored memory should persist
	fg.Reveal(20, 20)
	for i := 0; i < 10; i++ {
		fg.Clear()
		// Don't re-reveal — area should stay explored
	}
	if !fg.IsVisible(20, 20) {
		t.Error("explored memory should persist across multiple clears")
	}
	if fg.IsCurrentlyVisible(20, 20) {
		t.Error("should not be currently visible after clears without re-reveal")
	}
}

func TestAllCommandersDeadStillHasExplored(t *testing.T) {
	fg := NewFogGrid(64, 64)
	// Simulate: commander reveals, then dies (no more reveals), but clear still runs
	fg.Reveal(32, 32)
	fg.Clear() // commander dead, no re-reveal
	// Previously seen area should still be explored
	if !fg.IsVisible(32, 32) {
		t.Error("previously seen area should remain explored even with no active vision")
	}
	// Unexplored areas should remain unexplored
	if fg.IsVisible(0, 0) {
		t.Error("never-seen area should remain unexplored")
	}
}

func TestDataReturnsCopy(t *testing.T) {
	fg := NewFogGrid(10, 10)
	fg.Reveal(5, 5)
	d1 := fg.Data()
	d2 := fg.Data()
	// Modifying copy should not affect original
	d1[0] = 99
	if fg.Visible[0] == 99 {
		t.Error("Data() should return a copy, not a reference")
	}
	// Two copies should be independent
	d2[0] = 88
	if d1[0] == 88 {
		t.Error("two Data() calls should return independent copies")
	}
}

func TestFogSystemGetOrCreateGrid(t *testing.T) {
	fs := NewFogSystem(48, 96)
	g1 := fs.GetOrCreateGrid(1)
	if g1 == nil {
		t.Fatal("GetOrCreateGrid should return a grid")
	}
	g2 := fs.GetOrCreateGrid(1)
	if g1 != g2 {
		t.Error("second call should return same grid instance")
	}
	g3 := fs.GetOrCreateGrid(2)
	if g3 == nil {
		t.Fatal("GetOrCreateGrid for new player should return a grid")
	}
	if g1 == g3 {
		t.Error("different players should get different grids")
	}
}

// TestRevealRadiusLOSBlocks verifies terrain line-of-sight (issue #55 phase 2):
// a wall between the viewer and a distant tile hides that tile, while the
// wall tile itself and tiles on the near side remain visible.
func TestRevealRadiusLOSBlocks(t *testing.T) {
	fg := NewFogGrid(24, 24)
	// Wall column at x=10, rows 2..10 — sits between viewer (5,7) and the
	// far-right tiles.
	wall := func(x, y int32) bool { return x == 10 && y >= 2 && y <= 12 }
	fg.BlocksLOS = wall
	fg.RevealRadius(5, 7, 10)

	// Near side and the wall itself: visible.
	if !fg.IsCurrentlyVisible(8, 7) {
		t.Error("near-side tile (8,7) should be visible")
	}
	if !fg.IsCurrentlyVisible(10, 7) {
		t.Error("wall tile (10,7) itself should be visible (blocker is seen)")
	}
	// Tile directly beyond the wall on the same row: blocked.
	if fg.IsCurrentlyVisible(13, 7) {
		t.Error("tile (13,7) beyond wall should be hidden by LOS")
	}
	// Tile reachable by a path that skirts the wall (above/below) is fine —
	// pick one whose Bresenham line from (5,7) clears x==10 outside rows 2..12.
	if !fg.IsCurrentlyVisible(7, 1) {
		t.Error("tile (7,1) with clear LOS should be visible")
	}
}

// TestRevealRadiusNoBlockerWhenNil confirms the legacy path (BlocksLOS nil)
// still reveals by radius only — no behavior regression for callers that
// don't opt into LOS.
func TestRevealRadiusNoBlockerWhenNil(t *testing.T) {
	fg := NewFogGrid(24, 24)
	fg.RevealRadius(5, 7, 6)
	if !fg.IsCurrentlyVisible(11, 7) {
		t.Error("radius-only reveal should reach (11,7)")
	}
}

// TestHasLOSCorners sanity-checks the raycast on a small blocker pattern.
func TestHasLOSCorners(t *testing.T) {
	fg := NewFogGrid(16, 16)
	fg.BlocksLOS = func(x, y int32) bool { return x == 8 && y == 8 } // single blocker
	// Adjacent to viewer: always LOS (no intermediate tile).
	if !fg.hasLOS(5, 5, 6, 5) {
		t.Error("adjacent tile should always have LOS")
	}
	// Line passing through (8,8): blocked.
	if fg.hasLOS(4, 4, 12, 12) {
		t.Error("line through blocker (8,8) should be blocked")
	}
	// The blocker tile itself: visible (end tile not checked).
	if !fg.hasLOS(7, 7, 8, 8) {
		t.Error("blocker tile as target should be visible")
	}
	// Line clearing the blocker: open.
	if !fg.hasLOS(4, 5, 12, 5) {
		t.Error("horizontal line clearing the blocker should have LOS")
	}
}

// BenchmarkRevealRadiusLOS is a perf sanity check for the 10 Hz tick budget
// (issue #55 acceptance). Run with: go test -bench=. ./pkg/fog/
func BenchmarkRevealRadiusLOS(b *testing.B) {
	fg := NewFogGrid(30, 48)
	// Sparse forest blocker pattern, like a real map.
	fg.BlocksLOS = func(x, y int32) bool { return (x+y)%7 == 0 }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fg.RevealRadius(15, 24, 12) // one commander's vision
	}
}
