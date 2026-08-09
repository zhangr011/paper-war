package component

// CollisionComponent holds the per-entity collision state for the hard
// positional push-out (ADR-0030). The Radius is replicated from
// CombatUnitStats at spawn so the collision loop avoids map lookups on the
// hot path. Only combat units (including commanders) carry this component.
type CollisionComponent struct {
	// Radius is the circle radius in 12.4 fixed-point tiles. Two friendly
	// units whose centres are closer than r1+r2 are pushed apart.
	Radius int64
}
