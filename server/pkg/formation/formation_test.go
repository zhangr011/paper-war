package formation

import (
	"testing"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestLineFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationLine, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleMelee, component.RoleRanged, component.RoleRanged,
	})
	if len(offsets) != 4 {
		t.Fatalf("expected 4 offsets, got %d", len(offsets))
	}
	if offsets[0].DY >= 0 {
		t.Error("melee should be in front (negative Y)")
	}
}

func TestWedgeFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationWedge, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleMelee, component.RoleRanged,
	})
	if len(offsets) != 3 {
		t.Fatalf("expected 3 offsets, got %d", len(offsets))
	}
	if offsets[0].DY >= offsets[1].DY {
		t.Error("wedge tip should have most negative Y")
	}
}

func TestCircleFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationCircle, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleRanged, component.RoleMelee, component.RoleRanged,
	})
	if len(offsets) != 4 {
		t.Fatalf("expected 4 offsets, got %d", len(offsets))
	}
	meleeDist := abs(offsets[0].DX) + abs(offsets[0].DY)
	rangedDist := abs(offsets[1].DX) + abs(offsets[1].DY)
	if meleeDist >= rangedDist {
		t.Error("melee should be closer to center than ranged")
	}
}

func abs(x int64) int64 {
	if x < 0 { return -x }
	return x
}