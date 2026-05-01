package component

type FormationType uint8

const (
	FormationLine    FormationType = 0
	FormationWedge   FormationType = 1
	FormationCircle  FormationType = 2
	FormationScatter FormationType = 3
)

type RoleOffset struct {
	Role BoidRole
	DX   int64
	DY   int64
}

type FormationComponent struct {
	FormationType FormationType
	Spacing       int64
	RoleOffsets   []RoleOffset
}

type FormationRoleComponent struct {
	OffsetX int64
	OffsetY int64
	Role    BoidRole
}