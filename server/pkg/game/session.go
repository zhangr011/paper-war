package game

import (
	"github.com/user/paper-war/server/pkg/commander"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
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

	tickCount uint32
}

func NewGameSession() *GameSession {
	gs := &GameSession{}

	// 1. Create entity manager + world
	em := ecs.NewEntityManager()
	gs.World = ecs.NewWorld(em)

	// 2. Create GameMap (64x64 test map)
	gs.Map = tilemap.NewGameMap(64, 64)

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

	gs.World.AddSystem(gs.terrainSys)
	gs.World.AddSystem(gs.commanderSys)
	gs.World.AddSystem(gs.movementSys)
	gs.World.AddSystem(gs.combatSys)

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
		Speed: fixed.FromFloat(3.0),
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
			Speed: fixed.FromFloat(3.0),
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

func (gs *GameSession) handleMoveSquad(squadID uint32, targetX, targetY int64) {
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
	// For attack-ground, set the pathfinding target to the ground position
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
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if cmd.SquadID != squadID {
			return
		}
		cmd.TacticalState = component.TacticalState(orderType)
	})
}

// addComponent is a helper to add a component from a typed pool looked up from the world.
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
	}
}
