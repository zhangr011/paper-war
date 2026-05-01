package boid

import (
	"testing"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestSeparationForce(t *testing.T) {
	self := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	neighbors := [][2]int64{
		{fixed.FromFloat(6.0), fixed.FromFloat(5.0)},
	}
	fx, fy := SeparationForce(self, neighbors, fixed.FromFloat(3.0))
	if fx >= 0 {
		t.Errorf("separation should push away, fx=%d", fx)
	}
	// Use fy to avoid unused variable error
	if fy != 0 {
		t.Errorf("separation fy should be 0, got %d", fy)
	}
}

func TestCohesionForce(t *testing.T) {
	self := [2]int64{0, 0}
	neighbors := [][2]int64{
		{fixed.FromFloat(10.0), fixed.FromFloat(10.0)},
	}
	fx, fy := CohesionForce(self, neighbors)
	if fx <= 0 || fy <= 0 {
		t.Errorf("cohesion should pull toward center, fx=%d fy=%d", fx, fy)
	}
}

func TestAlignmentForce(t *testing.T) {
	selfVel := [2]int64{fixed.FromFloat(1.0), 0}
	neighborVels := [][2]int64{
		{0, fixed.FromFloat(1.0)},
	}
	fx, fy := AlignmentForce(selfVel, neighborVels)
	if fy <= 0 {
		t.Errorf("alignment should steer toward neighbor vel, fy=%d", fy)
	}
	// Use fx to avoid unused variable error
	if fx == 0 {
		t.Errorf("alignment fx should not be 0")
	}
}

func TestSeparationNoNeighbors(t *testing.T) {
	self := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	fx, fy := SeparationForce(self, nil, fixed.FromFloat(3.0))
	if fx != 0 || fy != 0 {
		t.Errorf("no neighbors = zero force, got (%d,%d)", fx, fy)
	}
}