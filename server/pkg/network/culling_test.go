package network

import (
	"testing"
)

func TestRectContains(t *testing.T) {
	r := Rect{X: 100, Y: 100, W: 200, H: 200}
	if !r.Contains(150, 150) {
		t.Error("(150,150) should be in rect")
	}
	if r.Contains(50, 150) {
		t.Error("(50,150) should be outside rect")
	}
	if r.Contains(300, 300) {
		t.Error("(300,300) at edge should be outside rect")
	}
}

func TestCullOwnedUnitsInView(t *testing.T) {
	view := &ClientView{ClientID: 1, OwnerID: 1, ViewRect: Rect{0, 0, 1000, 1000}}
	units := []UnitInfo{
		{EntityID: 1, X: 500, Y: 500, OwnerID: 1},
		{EntityID: 2, X: 500, Y: 500, OwnerID: 2}, // enemy
		{EntityID: 3, X: 2000, Y: 500, OwnerID: 1}, // out of view
	}

	visible := Cull(view, units)
	if len(visible) != 2 {
		t.Errorf("expected 2 visible units, got %d: %v", len(visible), visible)
	}
}

func TestCullCommanderAlwaysVisible(t *testing.T) {
	view := &ClientView{ClientID: 1, OwnerID: 1, ViewRect: Rect{0, 0, 100, 100}}
	units := []UnitInfo{
		{EntityID: 10, X: 5000, Y: 5000, OwnerID: 1, IsCommander: true},
	}

	visible := Cull(view, units)
	if len(visible) != 1 {
		t.Error("owned commander should always be visible even outside view")
	}
}

func TestCullAllMultipleClients(t *testing.T) {
	culler := NewCuller()
	culler.AddView(&ClientView{ClientID: 1, OwnerID: 1, ViewRect: Rect{0, 0, 500, 500}})
	culler.AddView(&ClientView{ClientID: 2, OwnerID: 2, ViewRect: Rect{500, 0, 500, 500}})

	units := []UnitInfo{
		{EntityID: 1, X: 250, Y: 250, OwnerID: 1},
		{EntityID: 2, X: 750, Y: 250, OwnerID: 2},
	}

	result := culler.CullAll(units)
	if len(result[1]) != 1 {
		t.Errorf("client 1 sees %d units, want 1 (only own unit in view)", len(result[1]))
	}
	if len(result[2]) != 1 {
		t.Errorf("client 2 sees %d units, want 1", len(result[2]))
	}
}

func TestCullEmptyView(t *testing.T) {
	view := &ClientView{ClientID: 1, OwnerID: 1, ViewRect: Rect{0, 0, 100, 100}}
	units := []UnitInfo{
		{EntityID: 1, X: 5000, Y: 5000, OwnerID: 2}, // enemy far away
	}
	visible := Cull(view, units)
	if len(visible) != 0 {
		t.Error("no units should be visible")
	}
}
