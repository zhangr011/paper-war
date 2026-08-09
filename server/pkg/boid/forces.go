package boid

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