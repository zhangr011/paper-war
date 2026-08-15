package boid

// AttractionForce steers toward a target position (v1: replaces CommanderForce/CohesionForce/AlignmentForce).
// The >>2 gain makes commander-cohesion (follow) the dominant force on a
// moving follower: force = distance/4 × AttractionW(6.0) = 1.5×distance,
// which exceeds the flow-field weight (2.5) beyond ~1.7 tiles from the
// commander. It still decays to zero at the target, so arrival stays smooth.
func AttractionForce(self, target [2]int64) (fx, fy int64) {
	dx := target[0] - self[0]
	dy := target[1] - self[1]
	if dx == 0 && dy == 0 {
		return 0, 0
	}
	fx = dx >> 2
	fy = dy >> 2
	return
}