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
	DefaultMovementMultiplier = 5
	combatUnitCrossMapSeconds = 60 * 60
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

	// Build a default movement profile for terrain costs
	defaultProfile := &component.MovementProfile{
		ID: 0,
		TerrainCosts: [16]uint8{
			component.TerrainPlain:       1,
			component.TerrainRoad:        1,
			component.TerrainShallow:     3,
			component.TerrainDeep:        0, // impassable
			component.TerrainForest:      2,
			component.TerrainHill:        3,
			component.TerrainSwamp:       4,
			component.TerrainBridge:      1,
			component.TerrainWall:        0, // impassable
			component.TerrainSnow:        3,
			component.TerrainDesert:      2,
			component.TerrainStronghold1: 1,
			component.TerrainStronghold2: 1,
			component.TerrainStronghold3: 1,
			component.TerrainStronghold4: 1,
			component.TerrainStronghold5: 1,
		},
	}
	profiles := map[uint8]*component.MovementProfile{0: defaultProfile}

	// 6. Create all systems, add to world
	gs.terrainSys = terrain.NewTerrainSystem(gs.Map, gs.Cache, []*component.MovementProfile{defaultProfile})
	gs.commanderSys = &commander.CommanderSystem{Sh: gs.Sh}
	gs.movementSys = &movement.MovementSystem{
		Gm:       gs.Map,
		Cache:    gs.Cache,
		Sh:       gs.Sh,
		Profiles: profiles,
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
	gs.Lifecycle = NewMatchLifecycle(nil, nil)
	gs.Lifecycle.Start() // start immediately for PvAI
	gs.PlayerGold = make(map[uint32]int32)

		// Fog system (per-player visibility)
		gs.FogSys = fog.NewFogSystem(DefaultMapWidth, DefaultMapHeight)

		// AI system (player 2 is AI)
		gs.AISys = ai.NewAISystem(2, gs.FogSys, DefaultMapWidth, DefaultMapHeight)

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
}

func (gs *GameSession) updateFog() {
	if gs.FogSys == nil {
		return
	}
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	// Collect alive commander positions per player
	type cmdInfo struct {
		playerID    uint32
		tileX, tileY int32
	}
	var commanders []cmdInfo
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
		commanders = append(commanders, cmdInfo{
			playerID: owner.PlayerID,
			tileX:    int32(pos.X >> 12),
			tileY:    int32(pos.Y >> 12),
		})
	})

	// Clear all grids and reveal around each commander
	for pid := range gs.FogSys.Grids {
		gs.FogSys.Grids[pid].Clear()
	}
	for _, c := range commanders {
		grid := gs.FogSys.GetOrCreateGrid(c.playerID)
		grid.Reveal(c.tileX, c.tileY)
	}
}

func (gs *GameSession) runAI() {
	if gs.AISys == nil {
		return
	}
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	aiCmds := gs.AISys.Update(gs.tickCount, cmdPool, posPool, ownerPool, healthPool)
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

// Reset clears all entities, generates a new random map, and resets state.
func (gs *GameSession) Reset() {
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

	// Generate new map with random seed
	seed := rand.New(rand.NewSource(time.Now().UnixNano())).Int63()
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
	cmdRange := cmdStats.Range

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

	gs.addComponent(cmdEntity, component.MovementComponent{ProfileID: 0})
	gs.addComponent(cmdEntity, component.PathfindingComponent{})
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

	// --- Combat units ---
	gs.spawnCombatUnitsWithType(squadID, cx, cy, 0, unitCount, unitCount, playerID, faction, cmdType)
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
	spacing := fixed.FromFloat(0.3)

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
		ox := fixed.Mul(int64(col-(cols-1)/2), spacing)
		oy := fixed.Mul(int64(row+1), spacing)

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
			Range:      stats.Range,
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

		gs.addComponent(unitEntity, component.MovementComponent{ProfileID: 0})
		gs.addComponent(unitEntity, component.PathfindingComponent{})
		gs.addComponent(unitEntity, component.FormationRoleComponent{
			Role: role,
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
	snapshotBytes := network.EncodeSnapshot(snap)

	// Append fog grid data: marker 0xFF 0xFD + w(uint16) + h(uint16) + visible bytes
	if fogGrid != nil {
		fogData := make([]byte, 0, 6+len(fogGrid.Visible))
		fogData = append(fogData, 0xFF, 0xFD)
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

// MapData returns the terrain types as a flat byte array for client download.
func (gs *GameSession) MapData() []byte {
	size := gs.Map.Width * gs.Map.Height
	data := make([]byte, size)
	for i, tile := range gs.Map.Tiles {
		data[i] = byte(tile.TerrainType)
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
		for _, ci := range cmds {
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
