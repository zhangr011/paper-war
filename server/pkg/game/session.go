package game

import (
	"context"
	"math/rand"
	"time"

	"github.com/user/paper-war/server/pkg/ai"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/commander"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/movement"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/objective"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/persist"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/terrain"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type GameSession struct {
	World   *ecs.World
	Map     *tilemap.GameMap
	Cache   *pathfinding.Cache
	Sh      *spatial.Hash
	SnapGen *network.SnapshotGenerator
	Culler  *network.Culler

	// Systems (kept as references for command handling)
	terrainSys    *terrain.TerrainSystem
	commanderSys  *commander.CommanderSystem
	movementSys   *movement.MovementSystem
	combatSys     *combat.CombatSystem
	deathSys      *combat.DeathSystem
	levelingSys   *combat.LevelingSystem      // v1
	objectiveSys  *objective.ObjectiveSystem   // v1
	recruitSys    *combat.RecruitmentSystem    // v1
	FogSys        *fog.FogSystem
	AISys         *ai.AISystem
	AISys2        *ai.AISystem // second AI for clash mode (player 1)
	Lifecycle     *MatchLifecycle              // v1
	PlayerGold    map[uint32]int32             // v1: gold per player
	lastSentGold  map[uint32]int32             // track what was last sent to client
	Store         persist.Store                // v1: persistence (nil = no persistence)

	tickCount uint32
}

const (
	ServerTicksPerSecond      = 10
	DefaultMapWidth           = 48
	DefaultMapHeight          = 96
	InitialTeamCombatUnits    = 5  // v1: starter roster is 1 Cmd + 5 LI
	CombatUnitsPerTeamLevel   = 2
	DefaultMovementMultiplier = 1
	combatUnitCrossMapSeconds = 300
	StartGold                 = 50 // v1: 50 gold start
)

func CombatUnitCountForTeamLevel(level uint8) int {
	if level == 0 {
		level = 1
	}
	return InitialTeamCombatUnits + int(level-1)*CombatUnitsPerTeamLevel
}

func defaultCombatUnitSpeed(mapWidth int32) int64 {
	ticks := int64(ServerTicksPerSecond * combatUnitCrossMapSeconds)
	distance := int64(mapWidth) << fixed.FractionBits
	speed := distance * movement.PositionDivisor * DefaultMovementMultiplier / ticks

	// Movement applies velocity with integer division by movement.PositionDivisor.
	// Round up to the next divisor step so the effective speed remains near the
	// configured side-to-side target after truncation.
	if rem := speed % movement.PositionDivisor; rem != 0 {
		speed += movement.PositionDivisor - rem
	}
	if speed < movement.PositionDivisor {
		return movement.PositionDivisor
	}
	return speed
}

func NewGameSession() *GameSession {
	gs := &GameSession{}

	// 1. Create entity manager + world
	em := ecs.NewEntityManager()
	gs.World = ecs.NewWorld(em)

	// 2. Create GameMap
	gs.Map = tilemap.GenerateMap(DefaultMapWidth, DefaultMapHeight, 42)

	// 3. Create Spatial Hash (cell size = 2 world units in fixed-point)
	gs.Sh = spatial.NewHash(fixed.FromFloat(2.0))

	// 4. Create Flow Field Cache
	gs.Cache = pathfinding.NewCache(gs.Map, 64)

	// 5. Create all component pools and register with world
	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	cmdPool := ecs.NewComponentPool[component.CommanderComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()
	formationPool := ecs.NewComponentPool[component.FormationComponent]()
	formationRolePool := ecs.NewComponentPool[component.FormationRoleComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
		projPool := ecs.NewComponentPool[component.ProjectileComponent]()
	killPointsPool := ecs.NewComponentPool[component.KillPointsComponent]()
	unitTypePool := ecs.NewComponentPool[component.UnitTypeComponent]()

		gs.World.RegisterPool(component.PositionComponent{}, posPool)
	gs.World.RegisterPool(component.VelocityComponent{}, velPool)
	gs.World.RegisterPool(component.BoidComponent{}, boidPool)
	gs.World.RegisterPool(component.HealthComponent{}, healthPool)
	gs.World.RegisterPool(component.AttackComponent{}, attackPool)
	gs.World.RegisterPool(component.CommanderComponent{}, cmdPool)
	gs.World.RegisterPool(component.MovementComponent{}, movePool)
	gs.World.RegisterPool(component.PathfindingComponent{}, pathPool)
	gs.World.RegisterPool(component.FormationComponent{}, formationPool)
	gs.World.RegisterPool(component.FormationRoleComponent{}, formationRolePool)
	gs.World.RegisterPool(component.OwnerComponent{}, ownerPool)
		gs.World.RegisterPool(component.ProjectileComponent{}, projPool)
	gs.World.RegisterPool(component.KillPointsComponent{}, killPointsPool)
	gs.World.RegisterPool(component.UnitTypeComponent{}, unitTypePool)

	// Build movement profiles from the standard Light/Heavy definitions.
	// Light (ID 0): infantry — faster terrain traversal, can ford Shallow water.
	// Heavy (ID 1): motorized — slower on difficult terrain, cannot cross Shallow.
	stdProfiles := component.StandardMovementProfiles()
	profilesMap := make(map[uint8]*component.MovementProfile, len(stdProfiles))
	for _, p := range stdProfiles {
		profilesMap[p.ID] = p
	}

	// 6. Create all systems, add to world
	gs.terrainSys = terrain.NewTerrainSystem(gs.Map, gs.Cache, stdProfiles)
	gs.commanderSys = &commander.CommanderSystem{Sh: gs.Sh}
	gs.movementSys = &movement.MovementSystem{
		Gm:       gs.Map,
		Cache:    gs.Cache,
		Sh:       gs.Sh,
		Profiles: profilesMap,
	}
	gs.combatSys = &combat.CombatSystem{Sh: gs.Sh}
	gs.deathSys = &combat.DeathSystem{}

	gs.World.AddSystem(gs.terrainSys)
	gs.World.AddSystem(gs.commanderSys)
	gs.World.AddSystem(gs.movementSys)
	gs.World.AddSystem(gs.combatSys)
	gs.World.AddSystem(&combat.ProjectileSystem{})
	gs.World.AddSystem(gs.deathSys)

	// v1 systems
	gs.levelingSys = &combat.LevelingSystem{}
	gs.objectiveSys = objective.NewObjectiveSystem(gs.Map)
	gs.recruitSys = &combat.RecruitmentSystem{}
	gs.World.AddSystem(gs.levelingSys)
	gs.World.AddSystem(gs.objectiveSys)
	gs.World.AddSystem(gs.recruitSys)

	// v1: lifecycle and gold
	gs.Lifecycle = NewMatchLifecycle(nil, func(winnerFaction uint8, reason string) {
		// Final roster flush on match end
		gs.FlushRosters(context.Background())
	})
	gs.Lifecycle.Start() // start immediately for PvAI
	gs.PlayerGold = make(map[uint32]int32)

		// Fog system (per-player visibility)
		gs.FogSys = fog.NewFogSystem(DefaultMapWidth, DefaultMapHeight)

		// AI system (player 2 is AI)
		gs.AISys = ai.NewAISystem(2, gs.FogSys, DefaultMapWidth, DefaultMapHeight)
		gs.AISys.PlayerGold = gs.PlayerGold

	// 7. Create SnapshotGenerator and Culler
	gs.SnapGen = network.NewSnapshotGenerator()
	gs.Culler = network.NewCuller()

	// 8. Call world.Init()
	gs.World.Init()

	return gs
}

// Tick advances the game by one tick.
func (gs *GameSession) Tick() {
	if gs.Lifecycle.Phase != PhasePlaying {
		return
	}

	gs.tickCount++

	// Set gold on recruit system before tick
	if gs.recruitSys != nil {
		gs.recruitSys.PlayerGold = gs.PlayerGold
	}

	gs.World.Tick(gs.tickCount)

	// Deduct Gold from successful recruits
	if gs.recruitSys != nil {
		for playerID, deducted := range gs.recruitSys.GoldDeductions {
			if deducted > 0 {
				gs.PlayerGold[playerID] -= deducted
			}
		}
	}

	// Award Gold bounties from DeathSystem
	if gs.deathSys != nil {
		for playerID, bounty := range gs.deathSys.GoldBounties {
			if bounty > 0 {
				gs.PlayerGold[playerID] += bounty
				if gs.lastSentGold == nil {
					gs.lastSentGold = make(map[uint32]int32)
				}
			}
		}

		// Sync AI states with promoted commanders
		for squadID, newCmd := range gs.deathSys.Promotions {
			if gs.AISys != nil {
				if state, ok := gs.AISys.States[squadID]; ok {
					state.CommanderID = uint32(newCmd)
				}
			}
			if gs.AISys2 != nil {
				if state, ok := gs.AISys2.States[squadID]; ok {
					state.CommanderID = uint32(newCmd)
				}
			}
		}
	}

	// Fog of war: compute per-player visibility from commander positions
	gs.updateFog()

	// AI: run decision loop for enemy squads, execute commands
	gs.runAI()

	// v1: Check objectives
	if gs.objectiveSys != nil {
		result := gs.objectiveSys.CheckResult()
		if result.Finished {
			gs.Lifecycle.End(result.WinnerFaction, result.Reason)
		}
	}

	// v1: Periodic mid-match roster flush (every 300 ticks = 30s at 10Hz)
	if gs.tickCount%300 == 0 && gs.Store != nil {
		gs.FlushRosters(context.Background())
	}
}

func (gs *GameSession) updateFog() {
	if gs.FogSys == nil {
		return
	}
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	// Vision source: playerID + tile position + radius
	type visionSrc struct {
		pid        uint32
		tileX, tileY int32
		radius     int32
	}
	var sources []visionSrc

	// --- Commander vision (radius 12) ---
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		hp, hasHP := healthPool.Get(e)
		if hasHP && hp.HP <= 0 {
			return
		}
		pos, hasPos := posPool.Get(e)
		if !hasPos {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner {
			return
		}
		sources = append(sources, visionSrc{
			pid:    owner.PlayerID,
			tileX:  int32(pos.X >> 12),
			tileY:  int32(pos.Y >> 12),
			radius: fog.VisionRadiusTiles,
		})
	})

	// --- Combat unit vision (radius 6) ---
	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	utPool.Each(func(e ecs.Entity, ut *component.UnitTypeComponent) {
		hp, hasHP := healthPool.Get(e)
		if hasHP && hp.HP <= 0 {
			return
		}
		pos, hasPos := posPool.Get(e)
		if !hasPos {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner {
			return
		}
		sources = append(sources, visionSrc{
			pid:    owner.PlayerID,
			tileX:  int32(pos.X >> 12),
			tileY:  int32(pos.Y >> 12),
			radius: fog.UnitVisionRadiusTiles,
		})
	})

	// Single-pass: clear all grids, then reveal from every source
	for pid := range gs.FogSys.Grids {
		gs.FogSys.Grids[pid].Clear()
	}
	for _, s := range sources {
		grid := gs.FogSys.GetOrCreateGrid(s.pid)
		grid.RevealRadius(s.tileX, s.tileY, s.radius)
	}
}

func (gs *GameSession) runAI() {
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	runAISys := func(aiSys *ai.AISystem) {
		if aiSys == nil {
			return
		}
		aiCmds := aiSys.Update(gs.tickCount, cmdPool, posPool, ownerPool, healthPool)
		for _, cmd := range aiCmds {
			switch cmd.Type {
			case ai.CmdMove:
				gs.handleMoveSquad(cmd.SquadID, cmd.TargetX, cmd.TargetY)
			case ai.CmdAttack:
				gs.handleAttackTarget(cmd.SquadID, cmd.TargetID)
			case ai.CmdRecruit:
				// v1: handle AI recruitment
				gs.handleAIRecruit(cmd.UnitType)
			}
		}
	}

	runAISys(gs.AISys)
	runAISys(gs.AISys2)
}

// Reset clears all entities, generates a new random map, and resets state.
func (gs *GameSession) Reset() {
	seed := rand.New(rand.NewSource(time.Now().UnixNano())).Int63()
	gs.ResetWithSeed(seed)
}

// ResetWithMap clears all entities and installs a pre-built map.
func (gs *GameSession) ResetWithMap(m *tilemap.GameMap) {
	// Destroy all entities
	em := gs.World.Entities()
	var ids []ecs.Entity
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	posPool.Each(func(e ecs.Entity, _ *component.PositionComponent) {
		ids = append(ids, e)
	})
	for _, e := range ids {
		gs.removeComponents(e)
		em.Destroy(e)
	}

	gs.Map = m
	gs.Cache = pathfinding.NewCache(gs.Map, 64)

	gs.terrainSys = terrain.NewTerrainSystem(gs.Map, gs.Cache, nil)
	gs.movementSys.Gm = gs.Map
	gs.movementSys.Cache = gs.Cache

	gs.tickCount = 0
	gs.SnapGen = network.NewSnapshotGenerator()

	gs.FogSys = fog.NewFogSystem(DefaultMapWidth, DefaultMapHeight)
	gs.AISys = ai.NewAISystem(2, gs.FogSys, DefaultMapWidth, DefaultMapHeight)
	gs.AISys.PlayerGold = gs.PlayerGold
	gs.AISys2 = nil

	gs.objectiveSys.Reset(gs.Map)

	// Reset gold state
	gs.PlayerGold = make(map[uint32]int32)
	gs.lastSentGold = make(map[uint32]int32)

	// Reset lifecycle back to playing so the game loop keeps ticking
	if gs.Lifecycle != nil {
		gs.Lifecycle.Phase = PhasePlaying
		gs.Lifecycle.MatchResultSent = false
	}
}

// ResetWithSeed clears all entities and generates a map with the given seed.
func (gs *GameSession) ResetWithSeed(seed int64) {
	// Destroy all entities
	em := gs.World.Entities()
	// Collect all entity IDs first
	var ids []ecs.Entity
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	posPool.Each(func(e ecs.Entity, _ *component.PositionComponent) {
		ids = append(ids, e)
	})
	for _, e := range ids {
		gs.removeComponents(e)
		em.Destroy(e)
	}

	// Generate new map with given seed
	gs.Map = tilemap.GenerateMap(DefaultMapWidth, DefaultMapHeight, seed)
	gs.Cache = pathfinding.NewCache(gs.Map, 64)

	// Update system references
	gs.terrainSys = terrain.NewTerrainSystem(gs.Map, gs.Cache, nil)
	gs.movementSys.Gm = gs.Map
	gs.movementSys.Cache = gs.Cache

	// Reset tick counter and snapshot generator
	gs.tickCount = 0
	gs.SnapGen = network.NewSnapshotGenerator()

	// Reset fog and AI
	gs.FogSys = fog.NewFogSystem(DefaultMapWidth, DefaultMapHeight)
	gs.AISys = ai.NewAISystem(2, gs.FogSys, DefaultMapWidth, DefaultMapHeight)
	gs.AISys.PlayerGold = gs.PlayerGold
	gs.AISys2 = nil // reset clash AI

	// Reset objective system (reuse existing, update map)
	gs.objectiveSys.Reset(gs.Map)

	// Reset gold state
	gs.PlayerGold = make(map[uint32]int32)
	gs.lastSentGold = make(map[uint32]int32)

	// Reset lifecycle back to playing so the game loop keeps ticking
	if gs.Lifecycle != nil {
		gs.Lifecycle.Phase = PhasePlaying
		gs.Lifecycle.MatchResultSent = false
	}
}

// EnableClashMode activates AI for player 1 as well, creating a second AI system.
// Both teams are fully AI-controlled. The spectator (playerID=0) sees everything.
func (gs *GameSession) EnableClashMode() {
	// Clash mode: no fog for either AI so they always see each other
	gs.AISys.FogSystem = nil
	gs.AISys2 = ai.NewAISystem(1, nil, DefaultMapWidth, DefaultMapHeight)
	gs.AISys2.PlayerGold = gs.PlayerGold
}

// SpawnTeam creates the standard team composition for the given level.
func (gs *GameSession) SpawnTeam(playerID uint32, squadID uint32, cx, cy int64, level uint8) {
	gs.SpawnTeamWithType(playerID, squadID, cx, cy, level, component.UnitLightInfantry)
}

// SpawnTeamWithType creates a team with a specific commander type.
func (gs *GameSession) SpawnTeamWithType(playerID uint32, squadID uint32, cx, cy int64, level uint8, cmdType component.CombatUnitType) {
	gs.SpawnSquadWithType(playerID, squadID, cx, cy, CombatUnitCountForTeamLevel(level), cmdType)
	// Initialize gold for this player if not set
	if _, ok := gs.PlayerGold[playerID]; !ok {
		gs.PlayerGold[playerID] = StartGold
	}
}

// SpawnSquad creates a commander + N combat units for a given player.
func (gs *GameSession) SpawnSquad(playerID uint32, squadID uint32, cx, cy int64, unitCount int) {
	gs.SpawnSquadWithType(playerID, squadID, cx, cy, unitCount, component.UnitLightInfantry)
}

// SpawnSquadWithType creates a commander of the given type + N combat units.
func (gs *GameSession) SpawnSquadWithType(playerID uint32, squadID uint32, cx, cy int64, unitCount int, cmdType component.CombatUnitType) {
	em := gs.World.Entities()
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width)

	// --- Commander ---
	cmdEntity := em.Create()

	cmdPos := component.PositionComponent{
		X:     cx,
		Y:     cy,
		Angle: 0,
	}
	gs.addComponent(cmdEntity, cmdPos)

	gs.addComponent(cmdEntity, component.VelocityComponent{
		Vx:    0,
		Vy:    0,
		Speed: unitSpeed,
	})

	gs.addComponent(cmdEntity, component.BoidComponent{
		SquadID:       squadID,
		Role:          component.RoleCommander,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(0.8),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(2.0),
	})

	// Commander stats from type table, scaled up (3x HP, 2x dmg)
	cmdStats := component.CombatUnitTypeTable[cmdType]
	cmdHP := cmdStats.HP * 3
	cmdDmg := cmdStats.Damage * 2
	cmdRange := fixed.FromFloat(float64(cmdStats.Range))

	gs.addComponent(cmdEntity, component.HealthComponent{
		HP:     cmdHP,
		MaxHP:  cmdHP,
		Armor:  5,
		Morale: 100,
	})

	gs.addComponent(cmdEntity, component.AttackComponent{
		Range:      cmdRange,
		Damage:     cmdDmg,
		Cooldown:   cmdStats.Cooldown,
		AttackType: component.AttackMelee,
	})

	gs.addComponent(cmdEntity, component.UnitTypeComponent{
		Type:   cmdType,
		Weapon: cmdStats.Weapon,
		Armor:  cmdStats.Armor,
		Level:  1,
	})

	gs.addComponent(cmdEntity, component.CommanderComponent{
		SquadID:         squadID,
		AuraRadius:      fixed.FromFloat(3.0),
		AuraMoraleBonus: 20,
		TacticalState:   component.TacticalFollow,
		IsAlive:         true,
	})

	gs.addComponent(cmdEntity, component.MovementComponent{ProfileID: component.ArmorTypeToProfileID(cmdStats.Armor)})
	gs.addComponent(cmdEntity, component.PathfindingComponent{TargetX: cx, TargetY: cy})
	gs.addComponent(cmdEntity, component.FormationRoleComponent{
		Role: component.RoleCommander,
	})

	faction := component.FactionPlayer
	if playerID == 2 {
		faction = component.FactionEnemy
	}
	gs.addComponent(cmdEntity, component.OwnerComponent{
		PlayerID: playerID,
		Faction:  faction,
	})

	// Register with AI system if this is an AI player
	if gs.AISys != nil && playerID == gs.AISys.AIPlayerID {
		gs.AISys.RegisterSquad(squadID, uint32(cmdEntity))
	}
	if gs.AISys2 != nil && playerID == gs.AISys2.AIPlayerID {
		gs.AISys2.RegisterSquad(squadID, uint32(cmdEntity))
	}

	// --- Combat units (always LI for starter roster) ---
	gs.spawnCombatUnitsWithType(squadID, cx, cy, 0, unitCount, unitCount, playerID, faction, component.UnitLightInfantry)
}

// SpawnTeamFromRoster creates a commander + combat units from a persisted Commander roster entry.
// If the roster has no commanders (new player), it calls CreateStarterRoster first.
// Returns the spawned commander entity.
func (gs *GameSession) SpawnTeamFromRoster(playerID uint32, squadID uint32, cx, cy int64, cmd persist.Commander) ecs.Entity {
	em := gs.World.Entities()
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width)

	// Parse commander type
	cmdType, _ := component.ParseCombatUnitType(cmd.Type)
	cmdStats := component.CombatUnitTypeTable[cmdType]

	// --- Commander ---
	cmdEntity := em.Create()

	gs.addComponent(cmdEntity, component.PositionComponent{
		X:     cx,
		Y:     cy,
		Angle: 0,
	})

	gs.addComponent(cmdEntity, component.VelocityComponent{
		Speed: unitSpeed,
	})

	gs.addComponent(cmdEntity, component.BoidComponent{
		SquadID:       squadID,
		Role:          component.RoleCommander,
		SeparationW:   fixed.FromFloat(1.5),
		CohesionW:     fixed.FromFloat(0.8),
		AlignmentW:    fixed.FromFloat(1.0),
		FormationW:    fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(2.0),
	})

	// Commander stats: HP and Damage scale with level
	cmdHP := cmdStats.HP * 3 // base 3x HP for commanders
	if cmd.Level > 1 {
		cmdHP = cmdHP + cmdHP*int32(cmd.Level-1)/4 // +25% per level above 1
	}
	cmdDmg := cmdStats.Damage * 2 // base 2x dmg for commanders
	if cmd.Level > 1 {
		cmdDmg = cmdDmg + cmdDmg*int32(cmd.Level-1)/4
	}

	gs.addComponent(cmdEntity, component.HealthComponent{
		HP:     cmdHP,
		MaxHP:  cmdHP,
		Armor:  5,
		Morale: 100,
	})

	gs.addComponent(cmdEntity, component.AttackComponent{
		Range:      cmdStats.Range,
		Damage:     cmdDmg,
		Cooldown:   cmdStats.Cooldown,
		AttackType: component.AttackMelee,
	})

	gs.addComponent(cmdEntity, component.UnitTypeComponent{
		Type:   cmdType,
		Weapon: cmdStats.Weapon,
		Armor:  cmdStats.Armor,
		Level:  cmd.Level,
	})

	gs.addComponent(cmdEntity, component.CommanderComponent{
		SquadID:         squadID,
		AuraRadius:      fixed.FromFloat(3.0),
		AuraMoraleBonus: 20,
		TacticalState:   component.TacticalFollow,
		IsAlive:         true,
	})

	gs.addComponent(cmdEntity, component.MovementComponent{ProfileID: component.ArmorTypeToProfileID(cmdStats.Armor)})
	gs.addComponent(cmdEntity, component.PathfindingComponent{TargetX: cx, TargetY: cy})
	gs.addComponent(cmdEntity, component.FormationRoleComponent{
		Role: component.RoleCommander,
	})

	faction := component.FactionPlayer
	if playerID == 2 {
		faction = component.FactionEnemy
	}
	gs.addComponent(cmdEntity, component.OwnerComponent{
		PlayerID: playerID,
		Faction:  faction,
	})

	// Register with AI system if this is an AI player
	if gs.AISys != nil && playerID == gs.AISys.AIPlayerID {
		gs.AISys.RegisterSquad(squadID, uint32(cmdEntity))
	}

	// --- Spawn CombatUnits from roster ---
	// Formation grid: same layout as spawnCombatUnitsWithType
	unitCount := len(cmd.Units)
	formCols := 1
	for formCols*formCols < unitCount {
		formCols++
	}
	formSpacing := fixed.FromFloat(0.6)

	for i, cu := range cmd.Units {
		cuType, ok := component.ParseCombatUnitType(cu.Type)
		if !ok {
			cuType = component.UnitLightInfantry
		}
		cuStats := component.CombatUnitTypeTable[cuType]

		// Formation offset: grid around commander
		col := i % formCols
		row := i / formCols
		ox := int64(col-(formCols-1)/2) * formSpacing
		oy := int64(row+1) * formSpacing

		// Alternate melee/ranged roles
		role := component.RoleMelee
		attackType := component.AttackMelee
		if i%2 == 1 {
			role = component.RoleRanged
			attackType = component.AttackRanged
		}

		cuEntity := em.Create()
		gs.addComponent(cuEntity, component.PositionComponent{
			X: cx + ox,
			Y: cy + oy,
		})
		gs.addComponent(cuEntity, component.VelocityComponent{
			Speed: unitSpeed,
		})
		gs.addComponent(cuEntity, component.BoidComponent{
			SquadID:       squadID,
			Role:          role,
			SeparationW:   fixed.FromFloat(1.5),
			CohesionW:     fixed.FromFloat(0.8),
			AlignmentW:    fixed.FromFloat(1.0),
			FormationW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(2.0),
		})

		// Scale HP by level (each level adds ~15% HP)
		cuHP := cuStats.HP
		if cu.Level > 1 {
			cuHP = cuHP + cuHP*int32(cu.Level-1)*15/100
		}
		gs.addComponent(cuEntity, component.HealthComponent{
			HP:    cuHP,
			MaxHP: cuHP,
		})
		gs.addComponent(cuEntity, component.AttackComponent{
			Damage:     cuStats.Damage,
			Range:      fixed.FromFloat(float64(cuStats.Range)),
			Cooldown:   cuStats.Cooldown,
			AttackType: attackType,
		})
		gs.addComponent(cuEntity, component.UnitTypeComponent{
			Type:   cuType,
			Weapon: cuStats.Weapon,
			Armor:  cuStats.Armor,
			Level:  cu.Level,
		})
		gs.addComponent(cuEntity, component.MovementComponent{ProfileID: component.ArmorTypeToProfileID(cuStats.Armor)})
		gs.addComponent(cuEntity, component.PathfindingComponent{TargetX: cx, TargetY: cy})
		gs.addComponent(cuEntity, component.FormationRoleComponent{
			Role:    role,
			OffsetX: ox,
			OffsetY: oy,
		})
		gs.addComponent(cuEntity, component.OwnerComponent{
			PlayerID: playerID,
			Faction:  faction,
		})
	}

	// Initialize gold from roster
	if _, ok := gs.PlayerGold[playerID]; !ok {
		gs.PlayerGold[playerID] = cmd.Gold
	}

	return cmdEntity
}

// FlushRosters collects all living entities per player and saves them back to the Store.
// Dead units are permanently removed (Permadeath). If a player has zero commanders,
// a starter roster is granted.
// Call this on match end (final flush) or periodically during match.
func (gs *GameSession) FlushRosters(ctx context.Context) {
	if gs.Store == nil {
		return
	}

	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	unitTypePool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	kpPool := gs.World.Pool(component.KillPointsComponent{}).(*ecs.ComponentPool[component.KillPointsComponent])

	// Group living entities by playerID
	type playerCmd struct {
		entity    ecs.Entity
		squadID   uint32
		cmdComp   component.CommanderComponent
		unitType  component.UnitTypeComponent
	}
	// playerID -> commander data
	playerCmds := make(map[uint32]playerCmd)
	// playerID -> squadID -> []CombatUnit
	playerUnits := make(map[uint32]map[uint32][]persist.CombatUnit)

	// Collect living commanders
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		hp, hasHP := healthPool.Get(e)
		if hasHP && hp.HP <= 0 {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner {
			return
		}
		ut, hasUT := unitTypePool.Get(e)
		if !hasUT {
			return
		}
		playerCmds[owner.PlayerID] = playerCmd{
			entity:   e,
			squadID:  cmd.SquadID,
			cmdComp:  *cmd,
			unitType: ut,
		}
		if _, ok := playerUnits[owner.PlayerID]; !ok {
			playerUnits[owner.PlayerID] = make(map[uint32][]persist.CombatUnit)
		}
	})

	// Collect living combat units (non-commander entities with BoidComponent)
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander {
			return
		}
		hp, hasHP := healthPool.Get(e)
		if !hasHP || hp.HP <= 0 {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner {
			return
		}
		ut, hasUT := unitTypePool.Get(e)
		if !hasUT {
			return
		}

		pid := owner.PlayerID
		sid := bc.SquadID
		if _, ok := playerUnits[pid]; !ok {
			playerUnits[pid] = make(map[uint32][]persist.CombatUnit)
		}

		kp := int32(0)
		if kpComp, ok := kpPool.Get(e); ok {
			kp = kpComp.Points
		}

		playerUnits[pid][sid] = append(playerUnits[pid][sid], persist.CombatUnit{
			ID:         uint8(len(playerUnits[pid][sid]) + 1),
			Type:       component.CombatUnitTypeName(ut.Type),
			Level:      ut.Level,
			KillPoints: kp,
		})
	})

	// Save each player's roster
	for pid, pc := range playerCmds {
		units := playerUnits[pid][pc.squadID]
		if units == nil {
			units = []persist.CombatUnit{}
		}

		kp := int32(0)
		if kpComp, ok := kpPool.Get(pc.entity); ok {
			kp = kpComp.Points
		}

		cmd := persist.Commander{
			ID:    1, // v1: single commander per player
			Name:  "Commander",
			Type:  component.CombatUnitTypeName(pc.unitType.Type),
			Level: pc.unitType.Level,
			Gold:  gs.PlayerGold[pid],
			Formation: persist.FormationTemplate{
				WeaponSlot:   "Light",
				ArmorSlot:    "Light",
				LeadingSkill: 100,
			},
			Units: units,
		}
		_ = kp // kill points tracked in units already

		if err := gs.Store.SaveCommander(ctx, pid, cmd); err != nil {
			// Log but don't crash — persistence failures shouldn't kill the match
			continue
		}
	}

	// Check for eliminated players (had entities at some point but now have none)
	// These are players who had gold assigned but no living commanders
	for pid := range gs.PlayerGold {
		if _, ok := playerCmds[pid]; ok {
			continue // still has a living commander
		}
		// Player eliminated — delete commander and grant starter roster
		_ = gs.Store.DeleteCommander(ctx, pid, 1)
		_ = gs.Store.CreateStarterRoster(ctx, pid)
	}
}

// UpgradeTeam grows a team to the combat unit count for the requested level.
// It returns the number of units added.
func (gs *GameSession) UpgradeTeam(squadID uint32, level uint8) int {
	wantCombatUnits := CombatUnitCountForTeamLevel(level)
	currentCombatUnits := gs.countCombatUnits(squadID)
	if currentCombatUnits >= wantCombatUnits {
		return 0
	}

	cx, cy, ok := gs.teamCommanderPosition(squadID)
	if !ok {
		return 0
	}

	added := wantCombatUnits - currentCombatUnits

	// Look up owner info from the squad's commander
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	var playerID uint32
	var faction uint8
	boidPool.Each(func(e ecs.Entity, boid *component.BoidComponent) {
		if boid.SquadID == squadID && boid.Role == component.RoleCommander {
			if owner, ok := ownerPool.Get(e); ok {
				playerID = owner.PlayerID
				faction = owner.Faction
			}
		}
	})

	gs.spawnCombatUnits(squadID, cx, cy, currentCombatUnits, added, wantCombatUnits, playerID, faction)
	return added
}

func (gs *GameSession) spawnCombatUnits(squadID uint32, cx, cy int64, startIndex, count, formationCount int, playerID uint32, faction uint8) {
	gs.spawnCombatUnitsWithType(squadID, cx, cy, startIndex, count, formationCount, playerID, faction, component.UnitLightInfantry)
}

func (gs *GameSession) spawnCombatUnitsWithType(squadID uint32, cx, cy int64, startIndex, count, formationCount int, playerID uint32, faction uint8, unitType component.CombatUnitType) {
	em := gs.World.Entities()
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width)
	spacing := fixed.FromFloat(0.6)

	stats := component.CombatUnitTypeTable[unitType]

	for i := startIndex; i < startIndex+count; i++ {
		unitEntity := em.Create()

		// Arrange units in a grid pattern around the commander
		cols := 1
		for cols*cols < formationCount {
			cols++
		}
		row := i / cols
		col := i % cols
		ox := int64(col-(cols-1)/2) * spacing
		oy := int64(row+1) * spacing

		gs.addComponent(unitEntity, component.PositionComponent{
			X: cx + ox,
			Y: cy + oy,
		})

		gs.addComponent(unitEntity, component.VelocityComponent{
			Vx:    0,
			Vy:    0,
			Speed: unitSpeed,
		})

		// Alternate melee and ranged roles
		role := component.RoleMelee
		attackType := component.AttackMelee
		if i%2 == 1 {
			role = component.RoleRanged
			attackType = component.AttackRanged
		}

		gs.addComponent(unitEntity, component.BoidComponent{
			SquadID:       squadID,
			Role:          role,
			SeparationW:   fixed.FromFloat(1.5),
			CohesionW:     fixed.FromFloat(0.8),
			AlignmentW:    fixed.FromFloat(1.0),
			FormationW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(2.0),
		})

		gs.addComponent(unitEntity, component.HealthComponent{
			HP:     stats.HP,
			MaxHP:  stats.HP,
			Armor:  2,
			Morale: 100,
		})

		gs.addComponent(unitEntity, component.AttackComponent{
			Range:      fixed.FromFloat(float64(stats.Range)),
			Damage:     stats.Damage,
			Cooldown:   stats.Cooldown,
			AttackType: attackType,
		})

		gs.addComponent(unitEntity, component.UnitTypeComponent{
			Type:   unitType,
			Weapon: stats.Weapon,
			Armor:  stats.Armor,
			Level:  1,
		})

		gs.addComponent(unitEntity, component.MovementComponent{ProfileID: component.ArmorTypeToProfileID(stats.Armor)})
		gs.addComponent(unitEntity, component.PathfindingComponent{TargetX: cx, TargetY: cy})
		gs.addComponent(unitEntity, component.FormationRoleComponent{
			Role:    role,
			OffsetX: ox,
			OffsetY: oy,
		})
		gs.addComponent(unitEntity, component.OwnerComponent{
			PlayerID: playerID,
			Faction:  faction,
		})
	}
}

func (gs *GameSession) countCombatUnits(squadID uint32) int {
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	count := 0
	boidPool.Each(func(_ ecs.Entity, boid *component.BoidComponent) {
		if boid.SquadID == squadID && boid.Role != component.RoleCommander {
			count++
		}
	})
	return count
}

func (gs *GameSession) teamCommanderPosition(squadID uint32) (int64, int64, bool) {
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])

	var x, y int64
	found := false
	boidPool.Each(func(e ecs.Entity, boid *component.BoidComponent) {
		if found || boid.SquadID != squadID || boid.Role != component.RoleCommander {
			return
		}
		pos, ok := posPool.Get(e)
		if !ok {
			return
		}
		x = pos.X
		y = pos.Y
		found = true
	})
	return x, y, found
}

func (gs *GameSession) removeComponents(e ecs.Entity) {
	gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent]).Remove(e)
	gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent]).Remove(e)
	gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent]).Remove(e)
	gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent]).Remove(e)
	gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent]).Remove(e)
	gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent]).Remove(e)
	gs.World.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent]).Remove(e)
	gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent]).Remove(e)
	gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent]).Remove(e)
}

// HandleCommand processes a player command from the network.
func (gs *GameSession) HandleCommand(clientID uint32, cmd *network.Command) {
	switch cmd.Type {
	case network.CmdMoveSquad:
		gs.handleMoveSquad(cmd.SquadID, int64(cmd.TargetX), int64(cmd.TargetY))
	case network.CmdAttackTarget:
		gs.handleAttackTarget(cmd.SquadID, cmd.TargetID)
	case network.CmdAttackGround:
		gs.handleAttackGround(cmd.SquadID, int64(cmd.TargetX), int64(cmd.TargetY))
	case network.CmdChangeFormation:
		gs.handleChangeFormation(cmd.SquadID, cmd.FormationType)
	case network.CmdTacticalOrder:
		gs.handleTacticalOrder(cmd.SquadID, cmd.OrderType)
	}
}

// GenerateSnapshot produces a binary snapshot for a specific player.
func (gs *GameSession) GenerateSnapshot(playerID uint32, view network.Rect) []byte {

	var fogGrid *fog.FogGrid
	if gs.FogSys != nil {
		fogGrid = gs.FogSys.GetGrid(playerID)
	}

	var units []network.UnitInfo
	var allStates []network.EntityState
	var allIDs []uint32

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	attackPool := gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	// Track own commander entities for always-include
	ownCommanders := make(map[uint32]bool)
	velPool := gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])

	utPool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])

	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		id := uint32(e)

		// Fog filtering: own units always visible, enemy only if in fog-visible tile
		if fogGrid != nil {
			owner, hasOwner := ownerPool.Get(e)
			if hasOwner && owner.PlayerID != playerID {
				tileX := int32(pos.X >> 12)
				tileY := int32(pos.Y >> 12)
				if !fogGrid.IsVisible(tileX, tileY) {
					return
				}
			}
		}

		ui := network.UnitInfo{
			EntityID: id,
			X:        pos.X,
			Y:        pos.Y,
		}
		state := network.EntityState{
			X:     pos.X,
			Y:     pos.Y,
			Angle: pos.Angle,
		}
		if vel, ok := velPool.Get(e); ok {
			state.Vx = vel.Vx
			state.Vy = vel.Vy
			if vel.Vx != 0 || vel.Vy != 0 {
				state.State = 1
			}
		}
		if boid, ok := boidPool.Get(e); ok {
			ui.SquadID = boid.SquadID
			state.SquadID = boid.SquadID
		}
		if health, ok := healthPool.Get(e); ok {
			state.HP = health.HP
			state.Morale = health.Morale
		}
		if attack, ok := attackPool.Get(e); ok {
			state.TargetID = attack.TargetID
		}
		if ut, ok := utPool.Get(e); ok {
			state.UnitType = uint8(ut.Type)
		}
		if owner, hasOwner := ownerPool.Get(e); hasOwner {
			state.Team = uint8(owner.PlayerID)
		}
		units = append(units, ui)
		allStates = append(allStates, state)
		allIDs = append(allIDs, id)

		// Track own commanders
		if owner, hasOwner := ownerPool.Get(e); hasOwner && owner.PlayerID == playerID {
			if _, isCmd := cmdPool.Get(e); isCmd {
				ownCommanders[id] = true
			}
		}
	})

	// Always include own commanders (even off-screen)
	_ = ownCommanders // tracked above, already included since we iterate all positions

	cv := &network.ClientView{
		ClientID: playerID,
		ViewRect: view,
	}
	visible := network.Cull(cv, units)

	var visStates []network.EntityState
	var visIDs []uint32
	visibleSet := make(map[uint32]bool, len(visible))
	for _, id := range visible {
		visibleSet[id] = true
	}
	// Also include own commanders even if culled
	for id := range ownCommanders {
		visibleSet[id] = true
	}
	for i, id := range allIDs {
		if visibleSet[id] {
			visStates = append(visStates, allStates[i])
			visIDs = append(visIDs, id)
		}
	}

	snap := gs.SnapGen.Generate(gs.tickCount, visStates, visIDs)

	// Attach death events from DeathSystem
	if gs.deathSys != nil && len(gs.deathSys.Deaths) > 0 {
		for _, entityID := range gs.deathSys.Deaths {
			snap.Events = append(snap.Events, network.Event{
				Type: network.EventDeath,
				Data: []byte{byte(entityID), byte(entityID >> 8), byte(entityID >> 16), byte(entityID >> 24)},
			})
		}
		// Clean up snapshot generator's prevStates for dead entities
		gs.SnapGen.ClearPrevStates(gs.deathSys.Deaths)
	}
	snapshotBytes := network.EncodeSnapshot(snap)

	// Append fog grid data: marker 0xFF 0xFE 0xFD 0xFC + w(uint16) + h(uint16) + visible bytes
	if fogGrid != nil {
		fogData := make([]byte, 0, 8+len(fogGrid.Visible))
		fogData = append(fogData, 0xFF, 0xFE, 0xFD, 0xFC)
		fogData = appendUint16(fogData, uint16(fogGrid.Width))
		fogData = appendUint16(fogData, uint16(fogGrid.Height))
		fogData = append(fogData, fogGrid.Visible...)
		snapshotBytes = append(snapshotBytes, fogData...)
	}

	return snapshotBytes
}

func appendUint16(buf []byte, v uint16) []byte {
	return append(buf, byte(v), byte(v>>8))
}

// --- Helper methods for command handling ---

// resolveSquadID accepts either a real squadID or an entityID and returns the
// actual squadID by looking up the entity's BoidComponent if needed.
func (gs *GameSession) resolveSquadID(id uint32) uint32 {
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	// First check if id matches an actual squadID
	found := false
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == id {
			found = true
		}
	})
	if found {
		return id
	}
	// Otherwise treat id as entityID and look up its squad
	if bc, ok := boidPool.Get(ecs.Entity(id)); ok {
		return bc.SquadID
	}
	return id
}

func (gs *GameSession) handleMoveSquad(squadID uint32, targetX, targetY int64) {
	squadID = gs.resolveSquadID(squadID)
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if path, ok := pathPool.GetPtr(e); ok {
			path.TargetX = targetX
			path.TargetY = targetY
		}
	})
}

func (gs *GameSession) handleAttackTarget(squadID uint32, targetID uint32) {
	squadID = gs.resolveSquadID(squadID)
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	attackPool := gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if attack, ok := attackPool.GetPtr(e); ok {
			attack.TargetID = targetID
		}
	})
}

// handleAIRecruit processes an AI recruit command.
func (gs *GameSession) handleAIRecruit(unitType component.CombatUnitType) {
	if gs.recruitSys == nil {
		return
	}
	// Find the AI commander entity
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander {
			if owner, ok := ownerPool.Get(e); ok && owner.PlayerID == gs.AISys.AIPlayerID {
				cmdEntity = e
			}
		}
	})
	if cmdEntity == 0 {
		return
	}
	gs.recruitSys.Recruit(combat.RecruitRequest{
		CommanderEntity: cmdEntity,
		UnitType:        unitType,
	})
}

func (gs *GameSession) handleAttackGround(squadID uint32, targetX, targetY int64) {
	squadID = gs.resolveSquadID(squadID)
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	attackPool := gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if path, ok := pathPool.GetPtr(e); ok {
			path.TargetX = targetX
			path.TargetY = targetY
		}
		if attack, ok := attackPool.GetPtr(e); ok {
			attack.TargetID = 0 // clear entity target
			attack.GroundTargetX = targetX
			attack.GroundTargetY = targetY
		}
	})
}

func (gs *GameSession) handleChangeFormation(squadID uint32, formationType uint8) {
	squadID = gs.resolveSquadID(squadID)
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	formationPool := gs.World.Pool(component.FormationComponent{}).(*ecs.ComponentPool[component.FormationComponent])

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if formation, ok := formationPool.GetPtr(e); ok {
			formation.FormationType = component.FormationType(formationType)
		}
	})
}

func (gs *GameSession) handleTacticalOrder(squadID uint32, orderType uint8) {
	squadID = gs.resolveSquadID(squadID)
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if cmd.SquadID != squadID {
			return
		}
		cmd.TacticalState = component.TacticalState(orderType)
	})
}

// MapData returns the terrain types and elevation as a flat byte array for
// client download.  Layout: [terrain0, elev0, terrain1, elev1, ...] (2*w*h bytes).
func (gs *GameSession) MapData() []byte {
	size := gs.Map.Width * gs.Map.Height
	data := make([]byte, size*2)
	for i, tile := range gs.Map.Tiles {
		data[i*2] = byte(tile.TerrainType)
		data[i*2+1] = uint8(tile.Elevation)
	}
	return data
}

// MapSize returns the map dimensions.
func (gs *GameSession) MapSize() (int32, int32) {
	return gs.Map.Width, gs.Map.Height
}
func addComponent[T any](w *ecs.World, e ecs.Entity, comp T) {
	pool := w.Pool(comp).(*ecs.ComponentPool[T])
	pool.Add(e, comp)
}

func (gs *GameSession) addComponent(e ecs.Entity, comp interface{}) {
	pool := gs.World.Pool(comp)
	switch p := pool.(type) {
	case *ecs.ComponentPool[component.PositionComponent]:
		p.Add(e, comp.(component.PositionComponent))
	case *ecs.ComponentPool[component.VelocityComponent]:
		p.Add(e, comp.(component.VelocityComponent))
	case *ecs.ComponentPool[component.BoidComponent]:
		p.Add(e, comp.(component.BoidComponent))
	case *ecs.ComponentPool[component.HealthComponent]:
		p.Add(e, comp.(component.HealthComponent))
	case *ecs.ComponentPool[component.AttackComponent]:
		p.Add(e, comp.(component.AttackComponent))
	case *ecs.ComponentPool[component.CommanderComponent]:
		p.Add(e, comp.(component.CommanderComponent))
	case *ecs.ComponentPool[component.MovementComponent]:
		p.Add(e, comp.(component.MovementComponent))
	case *ecs.ComponentPool[component.PathfindingComponent]:
		p.Add(e, comp.(component.PathfindingComponent))
	case *ecs.ComponentPool[component.FormationComponent]:
		p.Add(e, comp.(component.FormationComponent))
	case *ecs.ComponentPool[component.FormationRoleComponent]:
		p.Add(e, comp.(component.FormationRoleComponent))
	case *ecs.ComponentPool[component.OwnerComponent]:
		p.Add(e, comp.(component.OwnerComponent))
	case *ecs.ComponentPool[component.ProjectileComponent]:
		p.Add(e, comp.(component.ProjectileComponent))
	case *ecs.ComponentPool[component.UnitTypeComponent]:
		p.Add(e, comp.(component.UnitTypeComponent))
	case *ecs.ComponentPool[component.KillPointsComponent]:
		p.Add(e, comp.(component.KillPointsComponent))
	}
}

// GetGoldUpdates returns player→gold pairs that changed since last call.
// Used by the network layer to send MsgGoldUpdate only when gold changed.
func (gs *GameSession) GetGoldUpdates() map[uint32]int32 {
	if gs.lastSentGold == nil {
		gs.lastSentGold = make(map[uint32]int32)
	}
	result := make(map[uint32]int32)
	for pid, gold := range gs.PlayerGold {
		if last, ok := gs.lastSentGold[pid]; !ok || last != gold {
			result[pid] = gold
			gs.lastSentGold[pid] = gold
		}
	}
	return result
}

// FlushRoster collects surviving entities and saves them to the Store.
// Dead units are permanently removed (Permadeath). If a player has zero
// commanders, they get a starter roster.
// This is called on match end and periodically during the match.
func (gs *GameSession) FlushRoster() {
	if gs.Store == nil {
		return
	}

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	unitTypePool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	kpPool := gs.World.Pool(component.KillPointsComponent{}).(*ecs.ComponentPool[component.KillPointsComponent])

	// Collect commanders per player
	type cmdInfo struct {
		entity ecs.Entity
		squad  uint32
		owner  uint32
	}
	playerCmds := make(map[uint32][]cmdInfo)

	cmdPool.Each(func(e ecs.Entity, cc *component.CommanderComponent) {
		if !cc.IsAlive {
			return
		}
		owner, _ := ownerPool.Get(e)
		bc, _ := boidPool.Get(e)
		playerCmds[owner.PlayerID] = append(playerCmds[owner.PlayerID], cmdInfo{
			entity: e, squad: bc.SquadID, owner: owner.PlayerID,
		})
	})

	ctx := context.Background()

	for playerID, cmds := range playerCmds {
		// Clear old roster — this flush replaces everything
		if p, ok := gs.Store.(*persist.MockStore); ok {
			if player, ok2 := p.Players[playerID]; ok2 {
				player.Commanders = nil
			}
		}

		for cmdIdx, ci := range cmds {
			ut, _ := unitTypePool.Get(ci.entity)

			// Collect surviving combat units in this squad
			var units []persist.CombatUnit
			boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
				if bc.SquadID != ci.squad || bc.Role == component.RoleCommander {
					return
				}
				if u, ok := unitTypePool.Get(e); ok {
					var ukp int32
					if kpc, ok := kpPool.Get(e); ok {
						ukp = kpc.Points
					}
					units = append(units, persist.CombatUnit{
						Type:       unitTypeName(u.Type),
						Level:      u.Level,
						KillPoints: ukp,
					})
				}
			})

			cmd := persist.Commander{
				ID:    uint8(cmdIdx + 1),
				Type:  unitTypeName(ut.Type),
				Level: ut.Level,
				Gold:  gs.PlayerGold[playerID],
				Formation: persist.FormationTemplate{
					LeadingSkill: 5 + int32(ut.Level)*2,
				},
				Units: units,
			}

			gs.Store.SaveCommander(ctx, playerID, cmd)
		}

		// Check if player has zero commanders → grant starter roster
		if len(cmds) == 0 {
			gs.Store.CreateStarterRoster(ctx, playerID)
		}
	}

	// Handle players with no commanders at all (all dead)
	for playerID := range gs.PlayerGold {
		if _, ok := playerCmds[playerID]; !ok {
			gs.Store.CreateStarterRoster(ctx, playerID)
		}
	}
}

// unitTypeName converts a CombatUnitType to its string name.
func unitTypeName(ut component.CombatUnitType) string {
	switch ut {
	case component.UnitLightInfantry:
		return "LightInfantry"
	case component.UnitHeavyInfantry:
		return "HeavyInfantry"
	case component.UnitSniper:
		return "Sniper"
	case component.UnitAntiArmorInfantry:
		return "AntiArmorInfantry"
	case component.UnitMotorGun:
		return "MotorGun"
	case component.UnitMotorArtillery:
		return "MotorArtillery"
	case component.UnitMotorMissile:
		return "MotorMissile"
	default:
		return "LightInfantry"
	}
}
