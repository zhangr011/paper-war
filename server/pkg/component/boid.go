package component

type BoidRole uint8

const (
	RoleMelee     BoidRole = 0
	RoleRanged    BoidRole = 1
	RoleFlanker   BoidRole = 2
	RoleCommander BoidRole = 3
)

type BoidComponent struct {
	SquadID       uint32
	Role          BoidRole
	SeparationW   int64
	CohesionW     int64
	AlignmentW    int64
	FormationW    int64
	NeighborRange int64
}