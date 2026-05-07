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
