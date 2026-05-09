package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/movement"
)

func TestDefaultCombatUnitSpeedCrossesMapInAboutOneHour(t *testing.T) {
	speed := defaultCombatUnitSpeed(DefaultMapWidth)
	effectivePerSecond := fixed.ToFloat((speed / movement.PositionDivisor) * ServerTicksPerSecond)
	actualSeconds := float64(DefaultMapWidth) / effectivePerSecond

	if actualSeconds < 55*60 || actualSeconds > 65*60 {
		t.Fatalf("cross-map time = %.1fs, want about one hour", actualSeconds)
	}
}

func TestNewGameSessionUsesPortraitMap(t *testing.T) {
	gs := NewGameSession()
	w, h := gs.MapSize()

	if w != DefaultMapWidth || h != DefaultMapHeight {
		t.Fatalf("map size = %dx%d, want %dx%d", w, h, DefaultMapWidth, DefaultMapHeight)
	}
	if h != w*2 {
		t.Fatalf("map ratio = %dx%d, want vertical 2:1", w, h)
	}
}

func TestCombatUnitCountForTeamLevel(t *testing.T) {
	tests := []struct {
		level uint8
		want  int
	}{
		{level: 0, want: 2},
		{level: 1, want: 2},
		{level: 2, want: 4},
		{level: 3, want: 6},
	}

	for _, tt := range tests {
		if got := CombatUnitCountForTeamLevel(tt.level); got != tt.want {
			t.Fatalf("CombatUnitCountForTeamLevel(%d) = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestSpawnTeamCreatesCommanderAndInitialCombatUnits(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 3, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	commanders, combatUnits := countSquadRoles(t, gs, 3)
	if commanders != 1 {
		t.Fatalf("commander count = %d, want 1", commanders)
	}
	if combatUnits != InitialTeamCombatUnits {
		t.Fatalf("combat unit count = %d, want %d", combatUnits, InitialTeamCombatUnits)
	}
}

func TestSpawnSquadUsesDefaultCombatUnitSpeed(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	want := defaultCombatUnitSpeed(gs.Map.Width)
	velPool := gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])

	count := 0
	velPool.Each(func(_ ecs.Entity, vel *component.VelocityComponent) {
		count++
		if vel.Speed != want {
			t.Errorf("spawned unit speed = %d, want %d", vel.Speed, want)
		}
	})

	wantCount := 1 + InitialTeamCombatUnits
	if count != wantCount {
		t.Fatalf("spawned velocity component count = %d, want %d", count, wantCount)
	}
}

func TestMoveSquadCommandUpdatesSquadPathTargets(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 7, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	targetX := fixed.FromFloat(20)
	targetY := fixed.FromFloat(30)
	gs.handleMoveSquad(7, targetX, targetY)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

	updated := 0
	boidPool.Each(func(e ecs.Entity, boid *component.BoidComponent) {
		if boid.SquadID != 7 {
			return
		}
		path, ok := pathPool.Get(e)
		if !ok {
			t.Errorf("squad entity %d missing pathfinding component", e)
			return
		}
		if path.TargetX != targetX || path.TargetY != targetY {
			t.Errorf("entity %d target = (%d,%d), want (%d,%d)",
				e, path.TargetX, path.TargetY, targetX, targetY)
		}
		updated++
	})

	wantCount := 1 + InitialTeamCombatUnits
	if updated != wantCount {
		t.Fatalf("updated squad member count = %d, want %d", updated, wantCount)
	}
}

func TestTeamMoveCommandMovesTeamMembersTowardTarget(t *testing.T) {
	gs := NewGameSession()
	squadID := uint32(11)
	gs.SpawnTeam(1, squadID, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	targetX := fixed.FromFloat(10)
	targetY := fixed.FromFloat(20)
	before := squadPositions(t, gs, squadID)

	gs.handleMoveSquad(squadID, targetX, targetY)
	for i := 0; i < 100; i++ {
		gs.Tick()
	}

	after := squadPositions(t, gs, squadID)
	if len(after) != 1+InitialTeamCombatUnits {
		t.Fatalf("moved team member count = %d, want %d", len(after), 1+InitialTeamCombatUnits)
	}

	for entity, start := range before {
		end, ok := after[entity]
		if !ok {
			t.Fatalf("team member %d missing after movement", entity)
		}
		if end.X == start.X && end.Y == start.Y {
			t.Fatalf("team member %d did not move", entity)
		}

		startDist := fixed.DistSq(start.X-targetX, start.Y-targetY)
		endDist := fixed.DistSq(end.X-targetX, end.Y-targetY)
		if endDist >= startDist {
			t.Fatalf("team member %d did not move closer to target: start distance %.3f, end distance %.3f",
				entity, fixed.ToFloat(fixed.ISqrt(startDist)), fixed.ToFloat(fixed.ISqrt(endDist)))
		}
	}
}

func TestUpgradeTeamAddsCombatUnitsWithoutAddingCommander(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 9, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	added := gs.UpgradeTeam(9, 2)
	if added != CombatUnitsPerTeamLevel {
		t.Fatalf("added combat units = %d, want %d", added, CombatUnitsPerTeamLevel)
	}

	commanders, combatUnits := countSquadRoles(t, gs, 9)
	if commanders != 1 {
		t.Fatalf("commander count after upgrade = %d, want 1", commanders)
	}
	if combatUnits != CombatUnitCountForTeamLevel(2) {
		t.Fatalf("combat unit count after upgrade = %d, want %d", combatUnits, CombatUnitCountForTeamLevel(2))
	}

	added = gs.UpgradeTeam(9, 2)
	if added != 0 {
		t.Fatalf("same-level upgrade added %d combat units, want 0", added)
	}
}

func countSquadRoles(t *testing.T, gs *GameSession, squadID uint32) (int, int) {
	t.Helper()

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	commanders := 0
	combatUnits := 0
	boidPool.Each(func(_ ecs.Entity, boid *component.BoidComponent) {
		if boid.SquadID != squadID {
			return
		}
		if boid.Role == component.RoleCommander {
			commanders++
			return
		}
		combatUnits++
	})
	return commanders, combatUnits
}

func squadPositions(t *testing.T, gs *GameSession, squadID uint32) map[ecs.Entity]component.PositionComponent {
	t.Helper()

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])

	positions := make(map[ecs.Entity]component.PositionComponent)
	boidPool.Each(func(e ecs.Entity, boid *component.BoidComponent) {
		if boid.SquadID != squadID {
			return
		}
		pos, ok := posPool.Get(e)
		if !ok {
			t.Fatalf("team member %d missing position component", e)
		}
		positions[e] = pos
	})
	return positions
}
