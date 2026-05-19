package game

import (
	"encoding/binary"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/movement"
	"github.com/user/paper-war/server/pkg/network"
)

func TestDefaultCombatUnitSpeedUsesFiveTimesMovement(t *testing.T) {
	speed := defaultCombatUnitSpeed(DefaultMapWidth)
	effectivePerSecond := fixed.ToFloat((speed / movement.PositionDivisor) * ServerTicksPerSecond)
	actualSeconds := float64(DefaultMapWidth) / effectivePerSecond
	wantSeconds := float64(combatUnitCrossMapSeconds) / float64(DefaultMovementMultiplier)

	if actualSeconds < wantSeconds*0.9 || actualSeconds > wantSeconds*1.1 {
		t.Fatalf("cross-map time = %.1fs, want about %.1fs at %dx movement",
			actualSeconds, wantSeconds, DefaultMovementMultiplier)
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
		{level: 0, want: InitialTeamCombatUnits},
		{level: 1, want: InitialTeamCombatUnits},
		{level: 2, want: InitialTeamCombatUnits + CombatUnitsPerTeamLevel},
		{level: 3, want: InitialTeamCombatUnits + 2*CombatUnitsPerTeamLevel},
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

func TestResetRemovesPreviousTeamComponents(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)
	gs.Reset()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	if got := len(squadPositions(t, gs, 1)); got != 1+InitialTeamCombatUnits {
		t.Fatalf("team member count after reset and respawn = %d, want %d", got, 1+InitialTeamCombatUnits)
	}
}

func TestMovingSnapshotIncludesVelocityAndMovingState(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)
	gs.handleMoveSquad(1, fixed.FromFloat(10), fixed.FromFloat(20))
	gs.Tick()

	data := gs.GenerateSnapshot(1, network.Rect{
		X: 0,
		Y: 0,
		W: fixed.FromFloat(float64(DefaultMapWidth)),
		H: fixed.FromFloat(float64(DefaultMapHeight)),
	})

	if !snapshotHasMovingUnit(t, data) {
		t.Fatalf("moving snapshot did not include a nonzero velocity and moving state")
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

func snapshotHasMovingUnit(t *testing.T, data []byte) bool {
	t.Helper()

	offset := 0
	readUint32 := func() uint32 {
		v := binary.LittleEndian.Uint32(data[offset:])
		offset += 4
		return v
	}
	readUint16 := func() uint16 {
		v := binary.LittleEndian.Uint16(data[offset:])
		offset += 2
		return v
	}
	readInt64 := func() int64 {
		v := int64(binary.LittleEndian.Uint64(data[offset:]))
		offset += 8
		return v
	}

	readUint32()
	readUint32()
	unitCount := readUint16()
	offset++

	for i := 0; i < int(unitCount); i++ {
		readUint32()
		mask := data[offset]
		offset++

		if mask&network.ChangedPosition != 0 {
			readInt64()
			readInt64()
		}

		vx := int64(0)
		vy := int64(0)
		if mask&network.ChangedVelocity != 0 {
			vx = readInt64()
			vy = readInt64()
		}
		if mask&network.ChangedAngle != 0 {
			offset += 2
		}
		if mask&network.ChangedHP != 0 {
			offset += 4
		}
		if mask&network.ChangedTargetID != 0 {
			offset += 4
		}
		if mask&network.ChangedMorale != 0 {
			offset += 4
		}

		state := uint8(0)
		hasState := mask&network.ChangedState != 0
		if hasState {
			state = data[offset]
			offset++
		}
		if mask&network.ChangedSquadID != 0 {
			offset += 4
		}

		if (vx != 0 || vy != 0) && hasState && state == 1 {
			return true
		}
	}

	return false
}
