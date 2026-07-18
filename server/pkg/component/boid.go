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
	// GarrisonedIn is the entity ID of the Stronghold this unit is inside, or
	// 0 when not garrisoned. Garrisoned units don't move (movement skips them),
	// fire from the stronghold's position, can't be targeted directly, and
	// absorb their share of damage dealt to the stronghold. Issue #54 phase 1B.
	GarrisonedIn uint32
}