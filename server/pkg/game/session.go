package game

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/user/paper-war/server/pkg/ai"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/commander"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/formation"
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
	terrainSys   *terrain.TerrainSystem
	commanderSys *commander.CommanderSystem
	movementSys  *movement.MovementSystem
	combatSys    *combat.CombatSystem
	deathSys     *combat.DeathSystem
	levelingSys  *combat.LevelingSystem // v1
	buildSys     *combat.BuildSystem
	objectiveSys *objective.ObjectiveSystem // v1
	recruitSys   *combat.RecruitmentSystem  // v1
	FogSys       *fog.FogSystem
	AISys        *ai.AISystem
	AISys2       *ai.AISystem     // second AI for clash mode (player 1)
	Lifecycle    *MatchLifecycle  // v1
	PlayerGold   map[uint32]int32 // v1: gold per player
	lastSentGold        map[uint32]int32 // track what was last sent to client
	lastStrongholdJSON  string           // dedupe stronghold_state broadcasts (#54 1B)
	Store        persist.Store    // v1: persistence (nil = no persistence)
	stats        *MatchStats      // v1: cumulative match statistics (AAR)

	tickCount uint32
}

const (
	ServerTicksPerSecond      = 10
	// DefaultMapWidth/Height are shrunk from the original 48×96 (issue #45).
	// The 30×48 portrait map with axis-aware speed tuning hits all four
	// pacing invariants: cross-map ≤ 240 s, PvP first-contact ≤ 120 s,
	// commander vision ≥ 25% of long axis, starter roster ≥ 0.4% of area.
	// See docs/adr/0020-map-scale-rebalance.md for the full rationale.
	DefaultMapWidth           = 30
	DefaultMapHeight          = 48
	InitialTeamCombatUnits    = 5 // v1: starter roster is 1 Cmd + 5 LI
	CombatUnitsPerTeamLevel   = 2
	DefaultMovementMultiplier = 1
	// combatUnitCrossMapSeconds is the wall-clock time a unit takes to
	// traverse the map's long axis. Reduced from 300 → 240 in issue #45
	// to hit the ≤ 4 min cross-map target after the axis-aware speed fix.
	combatUnitCrossMapSeconds = 240
	StartGold                 = 50 // v1: 50 gold start
)

func CombatUnitCountForTeamLevel(level uint8) int {
	if level == 0 {
		level = 1
	}
	return InitialTeamCombatUnits + int(level-1)*CombatUnitsPerTeamLevel
}

// defaultCombatUnitSpeed computes the velocity (in 12.4 fixed-point) that
// a combat unit should move per tick so that it crosses the map's longest
// axis in `combatUnitCrossMapSeconds` of wall-clock time.
//
// Issue #45: previously this took `mapWidth` and computed against the short
// axis, but PvP traversal actually happens on the *long* axis (spawns at
// top/bottom of a portrait map). The result was real matches taking ~9 min
// to first contact despite a 5-min configured cross-map time. Now we
// compute against max(width, height) so the formula is correct regardless
// of orientation.
//
// Callers can pass either dimension; we'll pick the longer internally.
// Movement applies velocity with integer division by PositionDivisor per
// tick, so we round up to the next divisor step to preserve the target.
func defaultCombatUnitSpeed(mapWidth, mapHeight int32) int64 {
	traversalAxis := mapWidth
	if mapHeight > mapWidth {
		traversalAxis = mapHeight
	}
	ticks := int64(ServerTicksPerSecond * combatUnitCrossMapSeconds)
	distance := int64(traversalAxis) << fixed.FractionBits
	speed := distance * movement.PositionDivisor * DefaultMovementMultiplier / ticks

	// Round up to the next divisor step so the effective speed remains near
	// the configured target after per-tick truncation.
	if rem := speed % movement.PositionDivisor; rem != 0 {
		speed += movement.PositionDivisor - rem
	}
	if speed < movement.PositionDivisor {
		return movement.PositionDivisor
	}
	return speed
}

func NewGameSession() *GameSession {
	gs := &GameSession{
		stats: NewMatchStats(),
	}

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
	structPool := ecs.NewComponentPool[component.StructureComponent]()
	strongholdPool := ecs.NewComponentPool[component.StrongholdComponent]()

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
	gs.World.RegisterPool(component.StructureComponent{}, structPool)
	gs.World.RegisterPool(component.StrongholdComponent{}, strongholdPool)

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
	gs.World.AddSystem(&combat.StrongholdSystem{}) // capture/garrison — after Combat(80), before Death(90) (#54 1B)
	gs.World.AddSystem(gs.deathSys)

	// v1 systems
	gs.levelingSys = &combat.LevelingSystem{}
	gs.objectiveSys = objective.NewObjectiveSystem(gs.Map)
	gs.recruitSys = &combat.RecruitmentSystem{}
	gs.buildSys = &combat.BuildSystem{PlayerGold: gs.PlayerGold, PlayerSpawns: map[uint32][2]int64{}}
	gs.World.AddSystem(gs.levelingSys)
	gs.World.AddSystem(gs.objectiveSys)
	gs.World.AddSystem(gs.recruitSys)
	gs.World.AddSystem(gs.buildSys)

	// v1: lifecycle and gold
	gs.Lifecycle = NewMatchLifecycle(nil, func(winnerFaction uint8, reason string) {
		// Final roster flush on match end
		gs.FlushRosters(context.Background())
	})
	gs.Lifecycle.Start() // start immediately for PvAI
	gs.PlayerGold = make(map[uint32]int32)

	// Wire shared state maps into the systems that need them.
	// recruitSys and buildSys were constructed above with the (then-nil)
	// gs.PlayerGold reference; point them at the freshly-allocated map
	// now.  Without this, gold checks + deductions in Build() and
	// Recruit() silently no-op.  (Issue found in QA pass.)
	if gs.recruitSys != nil {
		gs.recruitSys.PlayerGold = gs.PlayerGold
	}
	if gs.buildSys != nil {
		gs.buildSys.PlayerGold = gs.PlayerGold
	}

	// Fog system (per-player visibility)
	gs.FogSys = fog.NewFogSystem(DefaultMapWidth, DefaultMapHeight)

	gs.AISys = ai.NewAISystem(2, gs.FogSys, DefaultMapWidth, DefaultMapHeight)
	gs.AISys.PlayerGold = gs.PlayerGold
	gs.configureAIStrategy(gs.AISys)

	// Issue #52 — wire CombatSystem's state lookup to the AI systems so
	// squads in StateGuard hold ground instead of auto-pursuing. Both
	// AI systems (primary + clash) are queried; whichever owns the
	// squad returns the state, the other returns 0.
	gs.combatSys.StateLookup = func(squadID uint32) uint8 {
		if gs.AISys != nil {
			if st, ok := gs.AISys.States[squadID]; ok {
				return st.State
			}
		}
		if gs.AISys2 != nil {
			if st, ok := gs.AISys2.States[squadID]; ok {
				return st.State
			}
		}
		return 0
	}

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
	// Same for build system — Build() reads PlayerGold to check + deduct.
	// Without this, s.PlayerGold is nil on the BuildSystem and the gold
	// check at build.go:69-75 is silently skipped (no deduction either,
	// since line 98 is inside the same `if s.PlayerGold != nil` block).
	// Issue found in QA pass: structures were free.
	if gs.buildSys != nil {
		gs.buildSys.PlayerGold = gs.PlayerGold
	}

	gs.World.Tick(gs.tickCount)

	// Deduct Gold from successful recruits
	if gs.recruitSys != nil {
		for playerID, deducted := range gs.recruitSys.GoldDeductions {
			if deducted > 0 {
				gs.PlayerGold[playerID] -= deducted
			}
		}
		// AAR: count recruits per faction
		for playerID, count := range gs.recruitSys.SuccessfulRecruits {
			if faction := gs.FactionOfPlayer(playerID); faction != 0xFF {
				gs.stats.AddRecruits(faction, count, gs.recruitSys.GoldDeductions[playerID])
			}
		}
	}
	// Note: BuildSystem deducts gold directly inside Build() (line 98 of
	// build.go) — no post-tick reconciliation needed here.  The
	// GoldDeductions map on BuildSystem is only used to prevent
	// same-tick over-spend across multiple Build() calls.

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

		// AAR: record kill events
		for _, ke := range gs.deathSys.KillEvents {
			gs.stats.RecordKill(ke.KillerFaction, ke.DeadFaction, ke.IsCommander, ke.Bounty)
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
		pid          uint32
		tileX, tileY int32
		radius       int32
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

	// Single-pass: clear all grids, then reveal from every source.
	// Vision is gated by terrain line-of-sight (BlockLOS) when a map is
	// present — forests and walls block sight past them. Issue #55 phase 2.
	var blocksLOS func(x, y int32) bool
	if gs.Map != nil {
		blocksLOS = func(x, y int32) bool {
			t := gs.Map.TileAt(x, y)
			return t != nil && t.BlockLOS
		}
	}
	for pid := range gs.FogSys.Grids {
		gs.FogSys.Grids[pid].Clear()
	}
	for _, s := range sources {
		grid := gs.FogSys.GetOrCreateGrid(s.pid)
		grid.BlocksLOS = blocksLOS
		grid.RevealRadius(s.tileX, s.tileY, s.radius)
	}
}

// configureAIStrategy sets up the AI with map-specific strategic data:
// base position, stronghold locations, and objective awareness.
func (gs *GameSession) configureAIStrategy(aiSys *ai.AISystem) {
	if aiSys == nil || gs.Map == nil {
		return
	}

	// Set AI base position and enemy base position from map spawns.
	// Spawns[0] = player 1, Spawns[1] = player 2 (AI in PvAI).
	if len(gs.Map.Spawns) >= 2 {
		var mySp, enemySp [2]int32
		if aiSys.AIPlayerID == 2 {
			mySp = gs.Map.Spawns[1]
			enemySp = gs.Map.Spawns[0]
		} else {
			// Clash mode: AISys2 plays as player 1
			mySp = gs.Map.Spawns[0]
			enemySp = gs.Map.Spawns[1]
		}
		aiSys.SetBasePosition(fixed.FromFloat(float64(mySp[0])), fixed.FromFloat(float64(mySp[1])))
		aiSys.SetEnemyBasePosition(fixed.FromFloat(float64(enemySp[0])), fixed.FromFloat(float64(enemySp[1])))
	}

	// Wire build system spawn positions for placement range checks
	if gs.buildSys != nil {
		for pid, sp := range gs.Map.Spawns {
			gs.buildSys.PlayerSpawns[uint32(pid+1)] = [2]int64{
				fixed.FromFloat(float64(sp[0])),
				fixed.FromFloat(float64(sp[1])),
			}
		}
	}

	// Wire combat system terrain lookup for stronghold damage bonus
	if gs.combatSys != nil {
		gs.combatSys.TerrainFn = func(x, y int32) component.TerrainType {
			if x < 0 || y < 0 || x >= gs.Map.Width || y >= gs.Map.Height {
				return component.TerrainPlain
			}
			tile := gs.Map.TileAt(x, y)
			if tile == nil {
				return component.TerrainPlain
			}
			return tile.TerrainType
		}
	}

	// Stronghold positions come from generator-recorded specs now (gm.Strongholds),
	// not terrain — strongholds are entities (#54).
	var strongholds [][2]int32
	for _, s := range gs.Map.Strongholds {
		strongholds = append(strongholds, [2]int32{s.X, s.Y})
	}
	aiSys.SetStrongholds(strongholds)

	// Set objective
	aiSys.SetObjective(&gs.Map.Objective)
}

// StrongholdState is a stronghold's live, client-visible state.
type StrongholdState struct {
	X       int32 `json:"x"`       // tile coord
	Y       int32 `json:"y"`       // tile coord
	Level   uint8 `json:"level"`
	Faction uint8 `json:"faction"` // 0 player, 1 enemy, 0xFF neutral
}

// StrongholdStateIfChanged returns the current stronghold states and true when
// they differ from the last call (positions/levels/factions), for broadcasting
// a stronghold_state message. Dedupes via lastStrongholdJSON. (#54 1B.)
func (gs *GameSession) StrongholdStateIfChanged() ([]StrongholdState, bool) {
	states := gs.StrongholdStates()
	js, _ := json.Marshal(states)
	if string(js) == gs.lastStrongholdJSON {
		return nil, false
	}
	gs.lastStrongholdJSON = string(js)
	return states, true
}

// StrongholdStates returns the live state of every stronghold entity, for
// client rendering. Dead/removed strongholds are omitted. (#54 1B.)
func (gs *GameSession) StrongholdStates() []StrongholdState {
	sp, ok := gs.World.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])
	if !ok {
		return nil
	}
	hpPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	var out []StrongholdState
	sp.Each(func(e ecs.Entity, sc *component.StrongholdComponent) {
		hp, ok := hpPool.Get(e)
		if !ok || hp.HP <= 0 {
			return
		}
		pos, ok := posPool.Get(e)
		if !ok {
			return
		}
		owner, _ := ownerPool.Get(e)
		out = append(out, StrongholdState{
			X:       int32(pos.X >> 12),
			Y:       int32(pos.Y >> 12),
			Level:   sc.Level,
			Faction: owner.Faction,
		})
	})
	return out
}

// spawnStrongholdEntities creates a Stronghold Building entity for each
// generator-recorded spec in gs.Map.Strongholds. Each starts Neutral
// (FactionNeutral), with HP and garrison capacity scaled by level. Phase 1A
// (#54): the entity carries the data model; capture-by-flip + garrison are
// Phase 1B, so combat currently skips these as targets.
func (gs *GameSession) spawnStrongholdEntities() {
	if gs.Map == nil || len(gs.Map.Strongholds) == 0 {
		return
	}
	// Nil-safe pool resolution: some test worlds don't register every pool.
	posPool, ok := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	if !ok || posPool == nil {
		return
	}
	healthPool, ok := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	if !ok || healthPool == nil {
		return
	}
	ownerPool, ok := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	if !ok || ownerPool == nil {
		return
	}
	strongholdPool, ok := gs.World.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])
	if !ok || strongholdPool == nil {
		return
	}
	em := gs.World.Entities()

	for _, spec := range gs.Map.Strongholds {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{
			X: fixed.FromFloat(float64(spec.X)),
			Y: fixed.FromFloat(float64(spec.Y)),
		})
		hp := component.StrongholdHP(spec.Level)
		healthPool.Add(e, component.HealthComponent{HP: hp, MaxHP: hp, Armor: 5})
		ownerPool.Add(e, component.OwnerComponent{PlayerID: 0, Faction: component.FactionNeutral})
		strongholdPool.Add(e, component.StrongholdComponent{
			Level:    spec.Level,
			Capacity: component.StrongholdCapacity(spec.Level),
		})
	}
}

func (gs *GameSession) runAI() {
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	unitTypePool := gs.World.Pool(component.UnitTypeComponent{}).(*ecs.ComponentPool[component.UnitTypeComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])

	runAISys := func(aiSys *ai.AISystem) {
		if aiSys == nil {
			return
		}
		aiCmds := aiSys.Update(gs.tickCount, cmdPool, posPool, ownerPool, healthPool, unitTypePool, boidPool)
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
//
// Seed selection (issue #47, Fix A):
//   - If PAPER_WAR_TEST_SEED is set, use that value as the map seed.
//     This makes test runs deterministic — every match plays out on the
//     same map, so a multiplayer e2e test doesn't randomly produce a
//     slow-resolution map that times out.
//   - Otherwise, use time.Now().UnixNano() — production behavior, every
//     match gets a fresh procedural map.
//
// The env var is read on every Reset() call so a long-running server
// inherits it from the process environment. Tests set it once in
// global-setup.js; production never sets it.
func (gs *GameSession) Reset() {
	seed := seedFromEnvOrTime()
	gs.ResetWithSeed(seed)
}

// seedFromEnvOrTime returns the value of PAPER_WAR_TEST_SEED parsed as
// an int64, or a fresh time-based seed if the env var is unset or
// invalid. Errors are logged once and silently fall back — a bad test
// config should not crash the server.
//
// Exported via a small wrapper (TestSeedFromEnv) so the env-var path
// can be unit-tested.
var testSeedEnvVar = "PAPER_WAR_TEST_SEED"

func seedFromEnvOrTime() int64 {
	if v := os.Getenv(testSeedEnvVar); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
		log.Printf("PAPER_WAR_TEST_SEED=%q is not a valid int64 — falling back to time-based seed", v)
	}
	return rand.New(rand.NewSource(time.Now().UnixNano())).Int63()
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
	gs.configureAIStrategy(gs.AISys)
	gs.AISys2 = nil

	gs.objectiveSys.Reset(gs.Map)

	// Spawn Stronghold entities from generator-recorded specs (#54).
	gs.spawnStrongholdEntities()

	// Reset match statistics — without this, stats accumulate across matches
	// in the same server session (issue #34: result statistics error again).
	gs.stats = NewMatchStats()

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
	gs.configureAIStrategy(gs.AISys)
	gs.AISys2 = nil // reset clash AI

	// Reset objective system (reuse existing, update map)
	gs.objectiveSys.Reset(gs.Map)

	// Spawn Stronghold entities from generator-recorded specs (#54).
	gs.spawnStrongholdEntities()

	// Reset match statistics — without this, stats accumulate across matches
	// in the same server session (issue #34: result statistics error again).
	gs.stats = NewMatchStats()

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
// Recruitment is disabled so teams fight only with their initial composition.
func (gs *GameSession) EnableClashMode() {
	// Clash mode: no fog for either AI so they always see each other
	gs.AISys.FogSystem = nil
	gs.AISys.RecruitDisabled = true
	gs.AISys.MoveDisabled = true
	gs.AISys2 = ai.NewAISystem(1, nil, DefaultMapWidth, DefaultMapHeight)
	gs.AISys2.PlayerGold = gs.PlayerGold
	gs.AISys2.RecruitDisabled = true
	gs.AISys2.MoveDisabled = true

	// Give AISys2 the same strategic awareness as AISys.
	// Without SetObjective, AISys2 stays idle and never advances,
	// giving AISys (Red) a permanent initiative advantage.
	if gs.Map != nil {
		gs.AISys2.SetObjective(&gs.Map.Objective)
		// Use spawn[0] (player 1) as AISys2's base if available.
		if len(gs.Map.Spawns) >= 1 {
			sp := gs.Map.Spawns[0]
			gs.AISys2.SetBasePosition(fixed.FromFloat(float64(sp[0])), fixed.FromFloat(float64(sp[1])))
		}
	}
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
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width, gs.Map.Height)

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
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width, gs.Map.Height)

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
	if gs.AISys2 != nil && playerID == gs.AISys2.AIPlayerID {
		gs.AISys2.RegisterSquad(squadID, uint32(cmdEntity))
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
		entity   ecs.Entity
		squadID  uint32
		cmdComp  component.CommanderComponent
		unitType component.UnitTypeComponent
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
	unitSpeed := defaultCombatUnitSpeed(gs.Map.Width, gs.Map.Height)
	spacing := fixed.FromFloat(0.6)

	stats := component.CombatUnitTypeTable[unitType]

	for i := startIndex; i < startIndex+count; i++ {
		unitEntity := em.Create()

		// Arrange units in a grid pattern around the commander.
		// Use float-centred column offsets so the formation is truly
		// symmetric around the commander.  Integer division
		// (col-(cols-1)/2) produces a lopsided grid whose centre is
		// offset by half a column, which gives one faction's units a
		// systematic range advantage over the other.
		cols := 1
		for cols*cols < formationCount {
			cols++
		}
		row := i / cols
		col := i % cols
		colOffset := float64(col) - float64(cols-1)/2.0
		ox := fixed.Mul(fixed.FromFloat(colOffset), spacing)
		oy := int64(row+1) * spacing

		// Mirror formation x-offsets for the enemy faction so the
		// two formations face each other symmetrically.  Without
		// this, both teams' col=0 units end up on the same physical
		// side, giving one team a range advantage on the enemy
		// commander.
		if faction == component.FactionEnemy {
			ox = -ox
		}

		// Small random jitter (±0.3 tiles) breaks the deterministic
		// symmetry of mirror matches. Without this, the first entity
		// processed each tick has a tiny first-mover advantage that
		// compounds over hundreds of ticks, giving one faction a
		// systematic win in AI-vs-AI clash mode.
		jx := fixed.FromFloat((rand.Float64() - 0.5) * 0.6)
		jy := fixed.FromFloat((rand.Float64() - 0.5) * 0.6)

		gs.addComponent(unitEntity, component.PositionComponent{
			X: cx + ox + jx,
			Y: cy + oy + jy,
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
	// Spectators (clientID 0, clash/crash test) have no squad or gold — reject
	// any economy command (recruit/build) even if a forged client sends one.
	if clientID == 0 && (cmd.Type == network.CmdRecruit || cmd.Type == network.CmdBuild) {
		return
	}
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
	case network.CmdBuild:
		gs.handleBuild(clientID, cmd.RecruitType, int64(cmd.TargetX), int64(cmd.TargetY))
	case network.CmdRecruit:
		// Human-player recruit. Spectators were already rejected above.
		// Resolves the requesting player's commander entity and forwards
		// the recruit request to the shared recruit system (same path
		// the AI uses via handleAIRecruit). Without this case the entire
		// human recruit flow is silently dropped — players could never
		// spend gold to replace losses.
		gs.handlePlayerRecruit(clientID, component.CombatUnitType(cmd.RecruitType))
	}
}

// handlePlayerRecruit processes a human-player recruit command.
// Mirror of handleAIRecruit but resolves the commander by clientID
// instead of AISys.AIPlayerID.
func (gs *GameSession) handlePlayerRecruit(clientID uint32, unitType component.CombatUnitType) {
	if gs.recruitSys == nil {
		return
	}
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	var cmdEntity ecs.Entity
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.Role == component.RoleCommander {
			if owner, ok := ownerPool.Get(e); ok && owner.PlayerID == clientID {
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

		// Fog filtering: own units always visible, enemy only if in currently-visible tile.
		// IsCurrentlyVisible (FogVisible=2) — NOT IsVisible (which includes FogExplored=1).
		// Explored tiles show terrain but must NOT show live enemy positions.
		if fogGrid != nil {
			owner, hasOwner := ownerPool.Get(e)
			if hasOwner && owner.PlayerID != playerID {
				tileX := int32(pos.X >> 12)
				tileY := int32(pos.Y >> 12)
				if !fogGrid.IsCurrentlyVisible(tileX, tileY) {
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
		// Track owner/squad for the AI-state lookup below.
		var unitOwner uint8
		var unitSquad uint32
		var hasSquad bool
		if vel, ok := velPool.Get(e); ok {
			state.Vx = vel.Vx
			state.Vy = vel.Vy
		}
		if boid, ok := boidPool.Get(e); ok {
			ui.SquadID = boid.SquadID
			state.SquadID = boid.SquadID
			unitSquad = boid.SquadID
			hasSquad = true
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
			unitOwner = uint8(owner.PlayerID)
		}
		// Issue #28 — copy the AI squad state into the per-unit wire state.
		// Previously this was hardcoded to 0/1 from velocity, so the client
		// never saw Attack (3) / Defend (5) / etc., and the attack/die
		// animations never triggered.  Now we look up the owning player's
		// AISystem and forward its squad-level State verbatim.  Player-
		// controlled squads (no AI state) fall through to a velocity
		// heuristic so the client still sees a move/idle signal.
		if hasSquad {
			var aiSys *ai.AISystem
			// AISys owns player 2's squads; AISys2 owns player 1's (clash).
			if unitOwner == 1 && gs.AISys2 != nil {
				aiSys = gs.AISys2
			} else if gs.AISys != nil {
				aiSys = gs.AISys
			}
			if aiSys != nil {
				if aiState, ok := aiSys.States[unitSquad]; ok {
					state.State = aiState.State
				}
			}
		}
		// Velocity fallback — only when no AI state was assigned (player-
		// controlled units without an AIState entry).  Preserves the old
		// behaviour for those units so the client still gets a move/idle cue.
		if state.State == 0 && (state.Vx != 0 || state.Vy != 0) {
			state.State = 1
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
	if gs.deathSys != nil && len(gs.deathSys.DeathRecords) > 0 {
		deadIDs := make([]uint32, 0, len(gs.deathSys.DeathRecords))
		for _, rec := range gs.deathSys.DeathRecords {
			// Issue #28 — enriched EventDeath payload:
			//   entityID (uint32, 4B)
			//   X         (int64,   8B)  — fixed-point position at death
			//   Y         (int64,   8B)  — fixed-point position at death
			//   tick      (uint32,  4B)  — simulation tick of death
			// Total: 24 bytes.  The client uses X/Y to anchor the die
			// animation at the exact death location rather than at the
			// interpolated render position (which may have drifted).
			data := make([]byte, 24)
			le := binary.LittleEndian
			le.PutUint32(data[0:4], rec.EntityID)
			le.PutUint64(data[4:12], uint64(rec.X))
			le.PutUint64(data[12:20], uint64(rec.Y))
			le.PutUint32(data[20:24], rec.Tick)
			snap.Events = append(snap.Events, network.Event{
				Type: network.EventDeath,
				Data: data,
			})
			deadIDs = append(deadIDs, rec.EntityID)
		}
		// Clean up snapshot generator's prevStates for dead entities
		gs.SnapGen.ClearPrevStates(deadIDs)
	}
	// Attach attack-fire events from CombatSystem.  Each record is one
	// attack resolution this tick; the client uses it to drive the attack
	// animation as a one-shot per shot.  Reuses EventProjectile (declared
	// but previously unwired) — semantics: "this unit fired at this tick",
	// payload {entityID uint32, tick uint32} = 8 bytes.  Issue #48.
	if gs.combatSys != nil && len(gs.combatSys.AttackRecords) > 0 {
		for _, rec := range gs.combatSys.AttackRecords {
			data := make([]byte, 8)
			le := binary.LittleEndian
			le.PutUint32(data[0:4], rec.EntityID)
			le.PutUint32(data[4:8], rec.Tick)
			snap.Events = append(snap.Events, network.Event{
				Type: network.EventProjectile,
				Data: data,
			})
		}
	}
	// Compute base alert: is the player's spawn under attack?
	snap.BaseAlert = gs.checkBaseAlert(playerID)

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

// handleBuild processes a player build request for defensive structures.
func (gs *GameSession) handleBuild(clientID uint32, structType uint8, x, y int64) {
	if gs.buildSys == nil {
		return
	}
	playerID := clientID // clientID maps to playerID
	gs.buildSys.Build(combat.BuildRequest{
		PlayerID: playerID,
		Type:     component.StructureType(structType),
		X:        x,
		Y:        y,
	})
}

// checkBaseAlert returns 1 if enemy units are within 10 tiles of the player's spawn.
func (gs *GameSession) checkBaseAlert(playerID uint32) uint8 {
	if gs.Map == nil || len(gs.Map.Spawns) == 0 {
		return 0
	}
	spawnIdx := int(playerID) - 1
	if spawnIdx < 0 || spawnIdx >= len(gs.Map.Spawns) {
		return 0
	}
	sp := gs.Map.Spawns[spawnIdx]
	spawnX := fixed.FromFloat(float64(sp[0]))
	spawnY := fixed.FromFloat(float64(sp[1]))

	const alertRadius = 10.0
	alertRadiusSq := alertRadius * alertRadius

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	found := false
	ownerPool.Each(func(e ecs.Entity, oc *component.OwnerComponent) {
		if found {
			return
		}
		if oc.PlayerID == playerID {
			return // friendly
		}
		hp, ok := healthPool.Get(e)
		if !ok || hp.HP <= 0 {
			return
		}
		pos, ok := posPool.Get(e)
		if !ok {
			return
		}
		dx := fixed.ToFloat(pos.X - spawnX)
		dy := fixed.ToFloat(pos.Y - spawnY)
		if dx*dx+dy*dy <= alertRadiusSq {
			found = true
		}
	})
	if found {
		return 1
	}
	return 0
}

func (gs *GameSession) handleMoveSquad(squadID uint32, targetX, targetY int64) {
	squadID = gs.resolveSquadID(squadID)
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	strongholdPool, _ := gs.World.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])

	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if path, ok := pathPool.GetPtr(e); ok {
			path.TargetX = targetX
			path.TargetY = targetY
		}
		// Garrison exit (#54 1B): a move order off the stronghold's tile
		// releases the unit from the garrison so movement can path it out.
		if bc.GarrisonedIn != 0 {
			gs.ungarrison(strongholdPool, e, bc)
		}
	})
}

// ungarrison removes a unit from its stronghold's garrison and clears the
// garrisoned flag. Called on a move order away or on flip-eviction.
func (gs *GameSession) ungarrison(strPool *ecs.ComponentPool[component.StrongholdComponent], unit ecs.Entity, bc *component.BoidComponent) {
	if strPool == nil || bc.GarrisonedIn == 0 {
		return
	}
	shE := ecs.Entity(bc.GarrisonedIn)
	if sh, ok := strPool.GetPtr(shE); ok {
		for i, g := range sh.Garrison {
			if g == unit {
				sh.Garrison = append(sh.Garrison[:i], sh.Garrison[i+1:]...)
				break
			}
		}
	}
	bc.GarrisonedIn = 0
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
	formationRolePool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])

	// 1. Update FormationType on FormationComponent for all squad members.
	// 2. Collect roles in entity order for CalcOffsets.
	type entry struct {
		entity ecs.Entity
		role   component.BoidRole
	}
	var members []entry
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if fc, ok := formationPool.GetPtr(e); ok {
			fc.FormationType = component.FormationType(formationType)
		}
		if bc.Role != component.RoleCommander {
			members = append(members, entry{entity: e, role: bc.Role})
		}
	})

	if len(members) == 0 {
		return
	}

	// 3. Compute new offsets via formation.CalcOffsets.
	roles := make([]component.BoidRole, len(members))
	for i, m := range members {
		roles[i] = m.role
	}
	spacing := fixed.FromFloat(0.6)
	offsets := formation.CalcOffsets(component.FormationType(formationType), spacing, roles)

	// 4. Apply new offsets to FormationRoleComponent.
	for i, m := range members {
		if fr, ok := formationRolePool.GetPtr(m.entity); ok {
			fr.OffsetX = offsets[i].DX
			fr.OffsetY = offsets[i].DY
		}
	}
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

// FactionOfPlayer maps a playerID to its faction constant.
// playerID 1 = FactionPlayer (0), playerID 2 = FactionEnemy (1).
// Returns 0xFF for unknown players.
func (gs *GameSession) FactionOfPlayer(playerID uint32) uint8 {
	switch playerID {
	case 1:
		return component.FactionPlayer
	case 2:
		return component.FactionEnemy
	default:
		return 0xFF
	}
}

// GetMatchStats returns the cumulative match statistics for AAR display.
func (gs *GameSession) GetMatchStats() *MatchStats {
	return gs.stats
}
