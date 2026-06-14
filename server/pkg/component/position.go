package component

type PositionComponent struct {
	X, Y  int64
	Angle int16
}

type VelocityComponent struct {
	Vx, Vy int64
	Speed  int64
}