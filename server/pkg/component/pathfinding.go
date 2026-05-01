package component

type PathfindingComponent struct {
	TargetX     int64
	TargetY     int64
	FlowFieldID uint32
	Stuck       bool
}