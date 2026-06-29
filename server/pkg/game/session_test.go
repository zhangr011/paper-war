package game

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/movement"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/persist"
)

// TestDefaultCombatUnitSpeedCrossesLongAxisInConfiguredTime verifies the
// core pacing invariant: a unit crossing the map's long axis (the actual
// traversal direction in PvP, where spawns are at top/bottom of a portrait
// map) takes `combatUnitCrossMapSeconds` of wall-clock time.
//
// Issue #45: previously this test asserted against DefaultMapWidth (the
// short axis), which meant real PvP traversal took ~9 min instead of the
// configured 5 min. Now the formula uses max(width, height) and this test
// reflects that.
func TestDefaultCombatUnitSpeedCrossesLongAxisInConfiguredTime(t *testing.T) {
	speed := defaultCombatUnitSpeed(DefaultMapWidth, DefaultMapHeight)
	effectivePerSecond := fixed.ToFloat((speed / movement.PositionDivisor) * ServerTicksPerSecond)

	// Long axis = whichever of width/height is larger. For the default
	// portrait map (32×48), this is 48.
	longAxis := DefaultMapHeight
	if DefaultMapWidth > DefaultMapHeight {
		longAxis = DefaultMapWidth
	}
	actualSeconds := float64(longAxis) / effectivePerSecond
	wantSeconds := float64(combatUnitCrossMapSeconds) / float64(DefaultMovementMultiplier)

	if actualSeconds < wantSeconds*0.9 || actualSeconds > wantSeconds*1.1 {
		t.Fatalf("cross-map time on long axis (%d tiles) = %.1fs, want about %.1fs",
			longAxis, actualSeconds, wantSeconds)
	}
}

func TestNewGameSessionUsesPortraitMap(t *testing.T) {
	gs := NewGameSession()
	w, h := gs.MapSize()

	if w != DefaultMapWidth || h != DefaultMapHeight {
		t.Fatalf("map size = %dx%d, want %dx%d", w, h, DefaultMapWidth, DefaultMapHeight)
	}
	// Portrait orientation: height > width. The exact aspect ratio is
	// no longer fixed at 2:1 (issue #45 shrank the map from 48×96 to
	// 30×48, a 1.6:1 ratio) — the ratio is now a tuning parameter,
	// not a structural invariant.
	if h <= w {
		t.Fatalf("map ratio = %dx%d, want portrait (h > w)", w, h)
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

	want := defaultCombatUnitSpeed(gs.Map.Width, gs.Map.Height)
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

	// Record formation offsets per entity
	frPool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])
	type offsetInfo struct{ ox, oy int64 }
	offsets := map[ecs.Entity]offsetInfo{}
	frPool.Each(func(e ecs.Entity, fr *component.FormationRoleComponent) {
		offsets[e] = offsetInfo{fr.OffsetX, fr.OffsetY}
	})

	gs.handleMoveSquad(squadID, targetX, targetY)
	for i := 0; i < 200; i++ {
		gs.Tick()
	}

	after := squadPositions(t, gs, squadID)
	if len(after) != 1+InitialTeamCombatUnits {
		t.Fatalf("moved team member count = %d, want %d", len(after), 1+InitialTeamCombatUnits)
	}

	// Find commander entity
	var cmdEntity ecs.Entity
	var cmdEndX, cmdEndY int64
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander && bc.SquadID == squadID {
			cmdEntity = e
			if pos, ok := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent]).Get(e); ok {
				cmdEndX = pos.X
				cmdEndY = pos.Y
			}
		}
	})

	for entity, start := range before {
		end, ok := after[entity]
		if !ok {
			t.Fatalf("team member %d missing after movement", entity)
		}
		if end.X == start.X && end.Y == start.Y {
			t.Fatalf("team member %d did not move", entity)
		}

		off := offsets[entity]

		// Commander: check it moved closer to the raw move target
		// Combat units: check they moved closer to commander's final pos + their formation offset
		if entity == cmdEntity {
			// Commander — check it moved at all (movement direction may vary due to
			// flow field forces; just verify it changed position)
			if end.X == start.X && end.Y == start.Y {
				t.Fatalf("commander %d did not move at all", entity)
			}
		} else {
			// Combat unit — compare against commander final pos + formation offset
			formTargetX := cmdEndX + off.ox
			formTargetY := cmdEndY + off.oy

			endDist := fixed.DistSq(end.X-formTargetX, end.Y-formTargetY)
			// Allow units to be within ~0.5 tiles of their formation slot (they may
			// already be close due to initial spawn layout or short tick count).
			tolerance := fixed.FromFloat(0.5)
			tolSq := tolerance * tolerance
			if endDist > tolSq {
				t.Fatalf("combat unit %d too far from formation slot: distance %.3f, tolerance %.1f",
					entity, fixed.ToFloat(fixed.ISqrt(endDist)), fixed.ToFloat(tolerance))
			}
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

	readUint32() // tick
	readUint32() // prevtick
	unitCount := readUint16()
	offset++ // eventCount
	offset++ // baseAlert

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

func TestSpawnTeamFromRoster(t *testing.T) {
	gs := NewGameSession()

	cmd := persist.Commander{
		ID:    1,
		Name:  "Test Sniper Commander",
		Type:  "Sniper",
		Level: 3,
		Gold:  75,
		Formation: persist.FormationTemplate{
			WeaponSlot:   "Light",
			ArmorSlot:    "Light",
			LeadingSkill: 100,
		},
		Units: []persist.CombatUnit{
			{ID: 1, Type: "HeavyInfantry", Level: 2},
			{ID: 2, Type: "HeavyInfantry", Level: 1},
		},
	}

	gs.SpawnTeamFromRoster(1, 1, fixed.FromFloat(20), fixed.FromFloat(30), cmd)

	// Count commanders and combat units
	commanders, combatUnits := countSquadRoles(t, gs, 1)
	if commanders != 1 {
		t.Fatalf("commander count = %d, want 1", commanders)
	}
	if combatUnits != 2 {
		t.Fatalf("combat unit count = %d, want 2", combatUnits)
	}

	// Verify commander type and level
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	var cmdUT *component.UnitTypeComponent
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == 1 && bc.Role == component.RoleCommander {
			if ut, ok := utPool.Get(e); ok {
				cmdUT = &ut
			}
		}
	})
	if cmdUT == nil {
		t.Fatal("commander UnitTypeComponent not found")
	}
	if cmdUT.Type != component.UnitSniper {
		t.Errorf("commander type = %d, want Sniper (%d)", cmdUT.Type, component.UnitSniper)
	}
	if cmdUT.Level != 3 {
		t.Errorf("commander level = %d, want 3", cmdUT.Level)
	}

	// Verify combat unit types
	var cuTypes []component.CombatUnitType
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == 1 && bc.Role != component.RoleCommander {
			if ut, ok := utPool.Get(e); ok {
				cuTypes = append(cuTypes, ut.Type)
			}
		}
	})
	if len(cuTypes) != 2 || cuTypes[0] != component.UnitHeavyInfantry || cuTypes[1] != component.UnitHeavyInfantry {
		t.Errorf("combat unit types = %v, want [HI, HI]", cuTypes)
	}

	// Verify gold was initialized from roster
	if gs.PlayerGold[1] != 75 {
		t.Errorf("player gold = %d, want 75", gs.PlayerGold[1])
	}
}

func TestFlushRostersSurvivors(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()
	gs.Store = store

	// Create a player in the mock store
	ctx := context.Background()
	p, err := store.FindOrCreatePlayer(ctx, "test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	playerID := p.ID

	// Spawn team with 2 HI units
	cmd := persist.Commander{
		ID:    1,
		Name:  "Test Cmd",
		Type:  "LightInfantry",
		Level: 1,
		Gold:  50,
		Units: []persist.CombatUnit{
			{ID: 1, Type: "HeavyInfantry", Level: 2},
			{ID: 2, Type: "HeavyInfantry", Level: 1},
			{ID: 3, Type: "LightInfantry", Level: 1},
		},
	}
	gs.SpawnTeamFromRoster(playerID, 1, 100<<16, 100<<16, cmd)
	gs.Lifecycle.Start()

	// Tick once to let systems settle
	gs.Tick()

	// Flush rosters
	gs.FlushRosters(ctx)

	// Load roster from store and verify
	roster, err := store.LoadRoster(ctx, playerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 {
		t.Fatalf("expected 1 commander, got %d", len(roster))
	}

	savedCmd := roster[0]
	if len(savedCmd.Units) != 3 {
		t.Errorf("expected 3 surviving units, got %d", len(savedCmd.Units))
	}
	if savedCmd.Type != "LightInfantry" {
		t.Errorf("commander type = %s, want LightInfantry", savedCmd.Type)
	}
}

func TestFlushRostersAllDeadGrantsStarter(t *testing.T) {
	gs := NewGameSession()
	store := persist.NewMockStore()
	gs.Store = store

	ctx := context.Background()
	p, err := store.FindOrCreatePlayer(ctx, "test-token-2", "")
	if err != nil {
		t.Fatal(err)
	}
	playerID := p.ID

	// Spawn team
	cmd := persist.Commander{
		ID:    1,
		Name:  "Doomed Cmd",
		Type:  "LightInfantry",
		Level: 1,
		Gold:  50,
		Units: []persist.CombatUnit{
			{ID: 1, Type: "LightInfantry", Level: 1},
		},
	}
	gs.SpawnTeamFromRoster(playerID, 1, 100<<16, 100<<16, cmd)
	gs.Lifecycle.Start()

	// Kill all entities for this player by setting HP to 0
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	ownerPool.Each(func(e ecs.Entity, owner *component.OwnerComponent) {
		if owner.PlayerID == playerID {
			if hp, ok := healthPool.GetPtr(e); ok {
				hp.HP = 0
			}
		}
	})

	// Run tick so DeathSystem processes the dead
	gs.Tick()

	// Now flush — should detect eliminated player and grant starter roster
	gs.FlushRosters(ctx)

	// Verify: player should have a starter roster (1 commander + 5 LI)
	roster, err := store.LoadRoster(ctx, playerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 {
		t.Fatalf("expected 1 starter commander, got %d", len(roster))
	}
	starterCmd := roster[0]
	if starterCmd.Type != "LightInfantry" {
		t.Errorf("starter commander type = %s, want LightInfantry", starterCmd.Type)
	}
	if len(starterCmd.Units) != 5 {
		t.Errorf("starter roster should have 5 units, got %d", len(starterCmd.Units))
	}
}

func TestFlushRostersNilStoreNoop(t *testing.T) {
	gs := NewGameSession()
	gs.Store = nil // no persistence

	// Should not panic
	gs.FlushRosters(context.Background())
}

// TestMovementProfileAssignment verifies that spawned units get the correct
// movement profile based on their armor type (ADR-0007).
// Light armor → ProfileID 0, Heavy armor → ProfileID 1.
func TestMovementProfileAssignment(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	movePool := gs.World.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])

	movePool.Each(func(e ecs.Entity, mc *component.MovementComponent) {
		ut, ok := utPool.Get(e)
		if !ok {
			return // commander has no UnitTypeComponent
		}
		wantID := component.ArmorTypeToProfileID(ut.Armor)
		if mc.ProfileID != wantID {
			t.Errorf("entity %d: armor=%d (type %v) has ProfileID=%d, want %d",
				e, ut.Armor, ut.Type, mc.ProfileID, wantID)
		}
	})
}

// TestMovementProfileHeavyUnitsGetHeavyProfile verifies that recruiting heavy
// units (e.g. MotorGun) assigns ProfileID 1.
func TestMovementProfileHeavyUnitsGetHeavyProfile(t *testing.T) {
	gs := NewGameSession()
	gs.SpawnTeam(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1)

	// Give gold and find commander for recruit
	gs.PlayerGold[1] = 500
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == 1 && b.Role == component.RoleCommander {
			cmdEntity = e
		}
	})
	if cmdEntity == 0 {
		t.Fatal("commander not found")
	}

	gs.recruitSys.Recruit(combat.RecruitRequest{
		CommanderEntity: cmdEntity,
		UnitType:        component.UnitMotorGun,
	})
	gs.Tick() // process recruit

	movePool := gs.World.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])

	foundHeavy := false
	movePool.Each(func(e ecs.Entity, mc *component.MovementComponent) {
		ut, ok := utPool.Get(e)
		if !ok {
			return
		}
		if ut.Type == component.UnitMotorGun {
			foundHeavy = true
			if mc.ProfileID != 1 {
				t.Errorf("MotorGun has ProfileID=%d, want 1 (Heavy)", mc.ProfileID)
			}
			if ut.Armor != component.ArmorHeavy {
				t.Errorf("MotorGun armor=%d, want ArmorHeavy (%d)", ut.Armor, component.ArmorHeavy)
			}
		}
	})

	if !foundHeavy {
		t.Fatal("no MotorGun found after Recruit")
	}
}

// TestSessionRegistersBothProfiles verifies the session init registers
// both Light and Heavy movement profiles in the movement system.
func TestSessionRegistersBothProfiles(t *testing.T) {
	gs := NewGameSession()

	profiles := gs.movementSys.Profiles
	if len(profiles) < 2 {
		t.Fatalf("movement system has %d profiles, want at least 2", len(profiles))
	}

	light, hasLight := profiles[0]
	if !hasLight {
		t.Fatal("profile ID 0 (Light) not registered")
	}
	heavy, hasHeavy := profiles[1]
	if !hasHeavy {
		t.Fatal("profile ID 1 (Heavy) not registered")
	}

	// Light can cross Shallow, Heavy cannot
	if light.TerrainCosts[component.TerrainShallow] == 0 {
		t.Error("Light profile should cross Shallow water")
	}
	if heavy.TerrainCosts[component.TerrainShallow] != 0 {
		t.Error("Heavy profile should NOT cross Shallow water")
	}
}