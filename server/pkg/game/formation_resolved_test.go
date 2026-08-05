package game_test

// Formation resolvability spec. At rest (no move order), a squad must settle
// into its slot grid closely enough that the grid is resolvable: each combat
// unit ends up within HALF a slot-pitch of its own slot (i.e. closer to its
// slot than to any neighbor's). The slot pitch is derived from the units' own
// FormationRoleComponent offsets, so the assertion is independent of the
// configured spacing value.
//
// Goes red when the configured spacing is too tight for the production-path
// (gs.Tick) scatter to be pulled back in (slot pitch < scatter), and green
// once spacing is wide enough that slot attraction dominates.

import (
	"math"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/game"
)

func TestFormationGridIsResolvedAtRest(t *testing.T) {
	gs := game.NewGameSession()
	const sq = uint32(1)
	cx := fixed.FromFloat(15.0)
	cy := fixed.FromFloat(24.0)
	gs.SpawnSquadWithType(1, sq, cx, cy, 9, component.UnitLightInfantry)

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	frPool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])

	// Settle under the PRODUCTION tick path (gs.Tick), not World.Tick — the
	// formation has to hold against whatever the post-tick hooks do.
	for i := 0; i < 60; i++ {
		gs.Tick()
	}

	var cmdX, cmdY float64
	cmdPool.Each(func(e ecs.Entity, c *component.CommanderComponent) {
		if c.SquadID == sq && c.IsAlive {
			if p, ok := posPool.Get(e); ok {
				cmdX, cmdY = fixed.ToFloat(p.X), fixed.ToFloat(p.Y)
			}
		}
	})

	// Derive the slot pitch = smallest nonzero |offset| component across the
	// squad (the grid step). Half of that is the resolvability threshold.
	pitch := math.MaxFloat64
	type row struct {
		slotDist float64
	}
	var rows []row
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID != sq || b.Role == component.RoleCommander {
			return
		}
		p, _ := posPool.Get(e)
		fr, _ := frPool.Get(e)
		ox, oy := fixed.ToFloat(fr.OffsetX), fixed.ToFloat(fr.OffsetY)
		if ox > 0 && ox < pitch {
			pitch = ox
		}
		if oy > 0 && oy < pitch {
			pitch = oy
		}
		ux, uy := fixed.ToFloat(p.X), fixed.ToFloat(p.Y)
		d := math.Hypot(ux-(cmdX+ox), uy-(cmdY+oy))
		rows = append(rows, row{slotDist: d})
	})

	if pitch == math.MaxFloat64 || len(rows) == 0 {
		t.Fatalf("could not derive slot pitch (no nonzero offsets?)")
	}
	threshold := pitch / 2
	var sum float64
	var worst float64
	for _, r := range rows {
		sum += r.slotDist
		if r.slotDist > worst {
			worst = r.slotDist
		}
	}
	mean := sum / float64(len(rows))
	t.Logf("pitch=%.3f threshold(half)=%.3f mean slot dist=%.3f worst=%.3f",
		pitch, threshold, mean, worst)

	if mean >= threshold {
		t.Errorf("formation grid not resolved: mean slot distance %.3f >= half-pitch %.3f "+
			"(slot attraction too weak for this spacing under gs.Tick)", mean, threshold)
	}
}
