package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/persist"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestRosterCombatUnitsHaveFormationAttraction is a regression test for bug #04.
//
// The bug: SpawnTeamFromRoster (solo-mode spawn path) created combat units
// with BoidComponent.FormationW == 0 (the Go zero value, because the field
// was never explicitly set) and no FormationRoleComponent.  Without the
// formation attraction force, when the commander moved forward, the only
// remaining force on combat units was separation from the commander
// (now in front of them) — which shoved them backward.
//
// The fix:
//   1. Set FormationW = 2.0 (matching spawnCombatUnitsWithType).
//   2. Add FormationRoleComponent with non-zero OffsetX/OffsetY.
//
// This test exercises the roster spawn path specifically (the existing
// movement_direction_test.go uses SpawnTeamWithType, the other path).
func TestRosterCombatUnitsHaveFormationAttraction(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0}
	gs.objectiveSys = nil

	if gs.Lifecycle.Phase != PhasePlaying {
		gs.Lifecycle.Start()
	}

	spawnX := fixed.FromFloat(24.0)
	spawnY := fixed.FromFloat(48.0)

	// Build a roster commander with 6 combat units — the structure that
	// would come from the persistent store in solo mode.
	rosterCmd := persist.Commander{
		ID:    1,
		Name:  "Test Commander",
		Type:  "LightInfantry",
		Level: 1,
		Units: []persist.CombatUnit{
			{ID: 1, Type: "LightInfantry", Level: 1},
			{ID: 2, Type: "LightInfantry", Level: 1},
			{ID: 3, Type: "LightInfantry", Level: 1},
			{ID: 4, Type: "LightInfantry", Level: 1},
			{ID: 5, Type: "LightInfantry", Level: 1},
			{ID: 6, Type: "LightInfantry", Level: 1},
		},
	}

	cmdEntity := gs.SpawnTeamFromRoster(1, 1, spawnX, spawnY, rosterCmd)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	frPool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])

	// ---- Static assertions on the spawned combat units ----
	type cuInfo struct {
		entity          ecs.Entity
		formationW      int64
		hasFormationRole bool
		offsetX         int64
		offsetY         int64
	}
	var combatUnits []cuInfo

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 || bc.Role == component.RoleCommander {
			return
		}
		info := cuInfo{entity: e, formationW: bc.FormationW}
		if fr, ok := frPool.Get(e); ok {
			info.hasFormationRole = true
			info.offsetX = fr.OffsetX
			info.offsetY = fr.OffsetY
		}
		combatUnits = append(combatUnits, info)
	})

	if len(combatUnits) != 6 {
		t.Fatalf("expected 6 combat units from roster, got %d", len(combatUnits))
	}

	zeroFormationW := 0
	noFormationRole := 0
	zeroOffsets := 0
	for _, u := range combatUnits {
		if u.formationW == 0 {
			zeroFormationW++
		}
		if !u.hasFormationRole {
			noFormationRole++
		}
		if u.hasFormationRole && u.offsetX == 0 && u.offsetY == 0 {
			zeroOffsets++
		}
	}
	if zeroFormationW > 0 {
		t.Errorf("%d/%d roster combat units have FormationW=0 — bug #04 regressed (no attraction to commander)",
			zeroFormationW, len(combatUnits))
	}
	if noFormationRole > 0 {
		t.Errorf("%d/%d roster combat units have no FormationRoleComponent — bug #04 regressed",
			noFormationRole, len(combatUnits))
	}
	if zeroOffsets > 0 {
		t.Errorf("%d/%d roster combat units have zero formation offsets — bug #01-style collapse",
			zeroOffsets, len(combatUnits))
	}

	// ---- Dynamic assertion: issue a forward move and confirm units advance ----
	gs.handleMoveSquad(1, spawnX, fixed.FromFloat(70.0))

	type posSample struct {
		id     uint32
		yBefore float64
		yAfter  float64
	}
	var samples []posSample
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != 1 || bc.Role == component.RoleCommander {
			return
		}
		pos, _ := posPool.Get(e)
		samples = append(samples, posSample{
			id:      uint32(e),
			yBefore: fixed.ToFloat(pos.Y),
		})
	})

	for i := 0; i < 200; i++ {
		gs.Tick()
	}

	for i := range samples {
		pos, _ := posPool.Get(ecs.Entity(samples[i].id))
		samples[i].yAfter = fixed.ToFloat(pos.Y)
	}

	backward := 0
	for _, s := range samples {
		delta := s.yAfter - s.yBefore
		t.Logf("roster unit %d: Y %.2f -> %.2f (delta=%+.2f)", s.id, s.yBefore, s.yAfter, delta)
		if delta < -0.1 {
			t.Errorf("roster unit %d moved BACKWARD: %.2f -> %.2f (delta=%+.2f) — bug #04 regressed",
				s.id, s.yBefore, s.yAfter, delta)
			backward++
		}
	}
	if backward > 0 {
		t.Errorf("%d/%d roster combat units moved backward", backward, len(samples))
	}

	// Average Y must increase (proves net forward motion, not stagnation)
	avgBefore, avgAfter := 0.0, 0.0
	for _, s := range samples {
		avgBefore += s.yBefore
		avgAfter += s.yAfter
	}
	avgBefore /= float64(len(samples))
	avgAfter /= float64(len(samples))
	t.Logf("roster avg Y: %.2f -> %.2f (delta=%+.2f)", avgBefore, avgAfter, avgAfter-avgBefore)
	if avgAfter <= avgBefore+0.5 {
		t.Errorf("roster combat units failed to move forward: avgY %.2f -> %.2f", avgBefore, avgAfter)
	}

	// Sanity: commander itself should be in the squad and accounted for
	_ = cmdEntity // suppress unused warning if no commander-specific check needed
}
