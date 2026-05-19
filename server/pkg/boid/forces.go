package boid

import "github.com/user/paper-war/server/pkg/fixed"

// SeparationForce pushes units apart when too close.
func SeparationForce(self [2]int64, neighbors [][2]int64, range_ int64) (fx, fy int64) {
	for _, n := range neighbors {
		dx := self[0] - n[0]
		dy := self[1] - n[1]
		distSq := fixed.DistSq(dx, dy)
		if distSq <= 0 {
			continue
		}
		dist := fixed.ISqrt(distSq)
		if dist > range_ {
			continue
		}
		strength := fixed.Div(range_-dist, dist)
		fx += fixed.Mul(dx, strength)
		fy += fixed.Mul(dy, strength)
	}
	return
}

// AttractionForce steers toward a target position (v1: replaces CommanderForce/CohesionForce/AlignmentForce).
func AttractionForce(self, target [2]int64) (fx, fy int64) {
	dx := target[0] - self[0]
	dy := target[1] - self[1]
	if dx == 0 && dy == 0 {
		return 0, 0
	}
	fx = dx >> 4
	fy = dy >> 4
	return
}