package formation

import (
	"math"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

type Offset struct {
	DX, DY int64
	Role   component.BoidRole
}

func CalcOffsets(ft component.FormationType, spacing int64, roles []component.BoidRole) []Offset {
	switch ft {
	case component.FormationLine:
		return lineFormation(spacing, roles)
	case component.FormationWedge:
		return wedgeFormation(spacing, roles)
	case component.FormationCircle:
		return circleFormation(spacing, roles)
	case component.FormationScatter:
		return scatterFormation(spacing, roles)
	default:
		return lineFormation(spacing, roles)
	}
}

// DiscOffsets returns count slot positions arranged in concentric rings
// centered on (0,0), filling the innermost ring first. Ring r (0-indexed)
// sits at radius (r+1)*spacing and holds up to 6*(r+1) units. A partially
// filled ring is spread evenly around the FULL circle so the cluster has no
// directional bias and its mass stays centered on the commander. This
// replaces the old forward-extending spawn grid (oy = (row+1)*spacing) which
// stacked units ahead of the commander and produced a loose ~1.3-tile cluster.
// Role assignment is left to the caller.
func DiscOffsets(count int, spacing int64) [][2]int64 {
	out := make([][2]int64, count)
	placed := 0
	ring := 0
	for placed < count {
		radius := int64(ring+1) * spacing
		cap := 6 * (ring + 1)
		if cap > count-placed {
			cap = count - placed
		}
		for i := 0; i < cap; i++ {
			angle := 2 * math.Pi * float64(i) / float64(cap)
			// spacing is a fixed-point raw value (e.g. fixed.FromFloat(0.6));
			// keep arithmetic in fixed-point via fixed.Mul so the result is
			// a fixed-point offset, not a float-scaled blow-up.
			cosF := fixed.FromFloat(math.Cos(angle))
			sinF := fixed.FromFloat(math.Sin(angle))
			out[placed+i] = [2]int64{
				fixed.Mul(cosF, radius),
				fixed.Mul(sinF, radius),
			}
		}
		placed += cap
		ring++
	}
	return out
}

func lineFormation(spacing int64, roles []component.BoidRole) []Offset {
	offsets := make([]Offset, len(roles))
	meleeCount := 0
	rangedCount := 0
	for _, r := range roles {
		if r == component.RoleMelee || r == component.RoleFlanker {
			meleeCount++
		} else {
			rangedCount++
		}
	}
	meleeIdx := 0
	rangedIdx := 0
	for i, r := range roles {
		if r == component.RoleMelee || r == component.RoleFlanker {
			x := int64(meleeIdx) * spacing
			if meleeCount > 1 {
				x -= spacing * int64(meleeCount-1) / 2
			}
			offsets[i] = Offset{DX: x, DY: -spacing, Role: r}
			meleeIdx++
		} else {
			x := int64(rangedIdx) * spacing
			if rangedCount > 1 {
				x -= spacing * int64(rangedCount-1) / 2
			}
			offsets[i] = Offset{DX: x, DY: spacing, Role: r}
			rangedIdx++
		}
	}
	return offsets
}

func wedgeFormation(spacing int64, roles []component.BoidRole) []Offset {
	offsets := make([]Offset, len(roles))
	for i := range roles {
		offsets[i] = Offset{
			DX:   0,
			DY:   -int64(len(roles)-1-i) * spacing,
			Role: roles[i],
		}
	}
	return offsets
}

func circleFormation(spacing int64, roles []component.BoidRole) []Offset {
	n := len(roles)
	offsets := make([]Offset, n)
	melee := 0
	ranged := 0
	for i, r := range roles {
		var radius int64
		var idx int
		var count int
		if r == component.RoleMelee || r == component.RoleFlanker {
			radius = spacing
			idx = melee
			melee++
			count = melee
		} else {
			radius = spacing * 2
			idx = ranged
			ranged++
			count = ranged
		}
		if count == 0 {
			count = 1
		}
		angle := 2 * math.Pi * float64(idx) / float64(count)
		offsets[i] = Offset{
			DX:   fixed.FromFloat(math.Cos(angle) * float64(radius)),
			DY:   fixed.FromFloat(math.Sin(angle) * float64(radius)),
			Role: r,
		}
	}
	return offsets
}

func scatterFormation(spacing int64, roles []component.BoidRole) []Offset {
	offsets := make([]Offset, len(roles))
	for i := range roles {
		angle := 2 * math.Pi * float64(i) / float64(len(roles))
		r := float64(spacing * 3)
		offsets[i] = Offset{
			DX:   fixed.FromFloat(math.Cos(angle) * r),
			DY:   fixed.FromFloat(math.Sin(angle) * r),
			Role: roles[i],
		}
	}
	return offsets
}