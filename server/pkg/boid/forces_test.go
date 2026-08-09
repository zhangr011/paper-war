package boid

import (
	"testing"

	"github.com/user/paper-war/server/pkg/fixed"
)

func TestAttractionForceSteersTowardTarget(t *testing.T) {
	self := [2]int64{fixed.FromFloat(0.0), fixed.FromFloat(0.0)}
	target := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	fx, fy := AttractionForce(self, target)
	if fx <= 0 || fy <= 0 {
		t.Errorf("attraction should steer toward target, got fx=%d fy=%d", fx, fy)
	}
}

func TestAttractionForceZeroWhenAtTarget(t *testing.T) {
	pos := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	fx, fy := AttractionForce(pos, pos)
	if fx != 0 || fy != 0 {
		t.Errorf("force should be zero at target, got fx=%d fy=%d", fx, fy)
	}
}

func TestAttractionForceDirection(t *testing.T) {
	self := [2]int64{fixed.FromFloat(10.0), fixed.FromFloat(5.0)}
	target := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	fx, fy := AttractionForce(self, target)
	if fx >= 0 {
		t.Errorf("should steer left (negative X), got fx=%d", fx)
	}
	if fy != 0 {
		t.Errorf("Y force should be 0 when only X differs, got fy=%d", fy)
	}
}
