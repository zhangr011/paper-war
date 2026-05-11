package game

import (
	"math/rand"
	"time"

	"github.com/user/paper-war/server/pkg/ai"
	"github.com/user/paper-war/server/pkg/commander"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/movement"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/pathfinding"
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
	terrainSys   *terrain.TerrainSystem
	commanderSys *commander.CommanderSystem
	movementSys  *movement.MovementSystem
	combatSys    *combat.CombatSystem
	deathSys     *combat.DeathSystem
	FogSys       *fog.FogSystem
	AISys        *ai.AISystem

	tickCount uint32
}

func NewGameSession() *GameSession {
	gs := &GameSession{}

	// 1. Create entity manager + world
	em := ecs.NewEntityManager()
	gs.World = ecs.NewWorld(em)

	// 2. Create GameMap (64x64 generated terrain)
	gs.Map = tilemap.GenerateMap(64, 64, 42)

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

	// Build a default movement profile for terrain costs
	defaultProfile := &component.MovementProfile{
		ID: 0,
		TerrainCosts: [16]uint8{
			component.TerrainPlain:   1,
			component.TerrainRoad:    1,
			component.TerrainShallow: 3,
			component.TerrainDeep:    0, // impassable
			component.TerrainForest:  2,
			component.TerrainHill:    3,
			component.TerrainSwamp:   4,
			component.TerrainBridge:  1,
			component.TerrainWall:    0, // impassable
			component.TerrainSnow:    3,
			component.TerrainDesert:  2,
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

		// Fog system (64x64 map, per-player visibility)
		gs.FogSys = fog.NewFogSystem(64, 64)

		// AI system (player 2 is AI)
		gs.AISys = ai.NewAISystem(2, gs.FogSys, 64, 64)

	// 7. Create SnapshotGenerator and Culler
	gs.SnapGen = network.NewSnapshotGenerator()
	gs.Culler = network.NewCuller()

	// 8. Call world.Init()
	gs.World.Init()

	return gs
}

// Tick advances the game by one tick.
func (gs *GameSession) Tick() {
	gs.tickCount++
	gs.World.Tick(gs.tickCount)

	// Fog of war: compute per-player visibility from commander positions
	gs.updateFog()

	// AI: run decision loop for enemy squads, execute commands
	gs.runAI()
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
		em.Destroy(e)
	}

	// Generate new map with random seed
	seed := rand.New(rand.NewSource(time.Now().UnixNano())).Int63()
	gs.Map = tilemap.GenerateMap(64, 64, seed)
	gs.Cache = pathfinding.NewCache(gs.Map, 64)

	// Update system references
	gs.terrainSys = terrain.NewTerrainSystem(gs.Map, gs.Cache, nil)
	gs.movementSys.Gm = gs.Map
	gs.movementSys.Cache = gs.Cache

	// Reset tick counter and snapshot generator
	gs.tickCount = 0
	gs.SnapGen = network.NewSnapshotGenerator()

	// Reset fog and AI
	gs.FogSys = fog.NewFogSystem(64, 64)
	gs.AISys = ai.NewAISystem(2, gs.FogSys, 64, 64)
}

// SpawnSquad creates a commander + N units for a given player.
func (gs *GameSession) SpawnSquad(playerID uint32, squadID uint32, cx, cy int64, unitCount int) {
	em := gs.World.Entities()

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
		Speed: fixed.FromFloat(0.01),
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

	gs.addComponent(cmdEntity, component.HealthComponent{
		HP:     200,
		MaxHP:  200,
		Armor:  5,
		Morale: 100,
	})

	gs.addComponent(cmdEntity, component.AttackComponent{
		Range:      fixed.FromFloat(1.5),
		Damage:     30,
		Cooldown:   3,
		AttackType: component.AttackMelee,
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
	spacing := fixed.FromFloat(0.3)
	for i := 0; i < unitCount; i++ {
		unitEntity := em.Create()

		// Arrange units in a grid pattern around the commander
		cols := 1
		for cols*cols < unitCount {
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
			Speed: fixed.FromFloat(0.01),
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
			HP:     80,
			MaxHP:  80,
			Armor:  2,
			Morale: 100,
		})

		gs.addComponent(unitEntity, component.AttackComponent{
			Range:      fixed.FromFloat(3.0),
			Damage:     15,
			Cooldown:   3,
			AttackType: attackType,
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

// GenerateSnapshot produces a binary snapshot for a specific client.
func (gs *GameSession) GenerateSnapshot(clientID uint32, view network.Rect) []byte {
	// Gather all unit info for culling
	var units []network.UnitInfo
	var allStates []network.EntityState
	var allIDs []uint32

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	attackPool := gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		id := uint32(e)
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
		if boid, ok := boidPool.Get(e); ok {
			ui.SquadID = boid.SquadID
		}
		if health, ok := healthPool.Get(e); ok {
			state.HP = health.HP
			state.Morale = health.Morale
		}
		if attack, ok := attackPool.Get(e); ok {
			state.TargetID = attack.TargetID
		}
		units = append(units, ui)
		allStates = append(allStates, state)
		allIDs = append(allIDs, id)
	})

	// Set up a temporary client view for culling
	cv := &network.ClientView{
		ClientID: clientID,
		ViewRect: view,
	}
	visible := network.Cull(cv, units)

	// Build visible entity states and IDs
	var visStates []network.EntityState
	var visIDs []uint32
	visibleSet := make(map[uint32]bool, len(visible))
	for _, id := range visible {
		visibleSet[id] = true
	}
	for i, id := range allIDs {
		if visibleSet[id] {
			visStates = append(visStates, allStates[i])
			visIDs = append(visIDs, id)
		}
	}

	snap := gs.SnapGen.Generate(gs.tickCount, visStates, visIDs)
	return network.EncodeSnapshot(snap)
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

func (gs *GameSession) handleAttackGround(squadID uint32, targetX, targetY int64) {
	squadID = gs.resolveSquadID(squadID)
	// and set attack target to 0 (ground attack mode handled differently)
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
			attack.TargetID = 0
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
	}
}
