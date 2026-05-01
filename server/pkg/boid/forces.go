package boid

import "github.com/user/paper-war/server/pkg/fixed"

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

func CohesionForce(self [2]int64, neighbors [][2]int64) (fx, fy int64) {
	if len(neighbors) == 0 {
		return 0, 0
	}
	var cx, cy int64
	for _, n := range neighbors {
		cx += n[0]
		cy += n[1]
	}
	cx = cx / int64(len(neighbors))
	cy = cy / int64(len(neighbors))
	fx = (cx - self[0]) >> 4
	fy = (cy - self[1]) >> 4
	return
}

func AlignmentForce(selfVel [2]int64, neighborVels [][2]int64) (fx, fy int64) {
	if len(neighborVels) == 0 {
		return 0, 0
	}
	var avgVx, avgVy int64
	for _, v := range neighborVels {
		avgVx += v[0]
		avgVy += v[1]
	}
	avgVx /= int64(len(neighborVels))
	avgVy /= int64(len(neighborVels))
	fx = (avgVx - selfVel[0]) >> 3
	fy = (avgVy - selfVel[1]) >> 3
	return
}