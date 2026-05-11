package fog

import "testing"

func TestNewFogGrid(t *testing.T) {
	fg := NewFogGrid(10, 10)
	if len(fg.Visible) != 100 {
		t.Errorf("expected 100 tiles, got %d", len(fg.Visible))
	}
}

func TestReveal(t *testing.T) {
	fg := NewFogGrid(64, 64)
	fg.Reveal(32, 32)
	if !fg.IsVisible(32, 32) {
		t.Error("center should be visible")
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
	fg.Clear()
	if fg.IsVisible(5, 5) {
		t.Error("tile should be fogged after clear")
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

func TestRevealCircle(t *testing.T) {
	fg := NewFogGrid(64, 64)
	fg.Reveal(32, 32)
	// Corner of bounding box should not be visible (outside circle)
	r := int32(VisionRadiusTiles)
	if fg.IsVisible(32+r, 32+r) {
		t.Error("corner of bounding box should be outside circle")
	}
}
