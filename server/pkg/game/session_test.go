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

func TestSpawnSquadUsesDefaultCombatUnitSpeed(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnSquad(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 3)

	want := defaultCombatUnitSpeed(gs.Map.Width)
	velPool := gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])

	count := 0
	velPool.Each(func(_ ecs.Entity, vel *component.VelocityComponent) {
		count++
		if vel.Speed != want {
			t.Errorf("spawned unit speed = %d, want %d", vel.Speed, want)
		}
	})

	if count != 4 {
		t.Fatalf("spawned velocity component count = %d, want 4", count)
	}
}

func TestMoveSquadCommandUpdatesSquadPathTargets(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnSquad(1, 7, fixed.FromFloat(10), fixed.FromFloat(10), 3)

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

	if updated != 4 {
		t.Fatalf("updated squad member count = %d, want 4", updated)
	}
}
