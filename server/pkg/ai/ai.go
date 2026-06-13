package ai

import (
	"math"
	"math/rand"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/tilemap"
)

const (
	StateIdle     uint8 = 0
	StatePatrol   uint8 = 1
	StateApproach uint8 = 2
	StateAttack   uint8 = 3
	StateRetreat  uint8 = 4
	StateDefend   uint8 = 5  // defend capture target or base
	StateScout    uint8 = 6  // exploring fogged areas
	StateCapture  uint8 = 7  // moving to capture stronghold

	EvalInterval        uint32 = 30
	RetreatHPThreshold         = 0.0 // disabled — fight to the death

	// Role definitions for recruitment strategy
	RoleFrontline = 0
	RoleRanged    = 1
	RoleHeavy     = 2

	// Strategic constants
	ExploreDuration     uint32 = 150 // ~30s at 5Hz tick
	BaseDefenseRadius          = 12.0
	BaseDefenseThreshold       = 1 // enemies near base to trigger defense
)

// roleTargetRatio is the ideal army composition ratio for each role.
var roleTargetRatio = [3]float64{0.40, 0.30, 0.30}

// unitRole maps each combat unit type to its tactical role.
var unitRole = map[component.CombatUnitType]int{
	component.UnitLightInfantry:      RoleFrontline,
	component.UnitHeavyInfantry:      RoleFrontline,
	component.UnitSniper:             RoleRanged,
	component.UnitAntiArmorInfantry:  RoleRanged,
	component.UnitMotorGun:           RoleHeavy,
	component.UnitMotorArtillery:     RoleHeavy,
	component.UnitMotorMissile:       RoleHeavy,
}

// roleUnits lists unit types available for each role, sorted by cost ascending.
var roleUnits = [3][]component.CombatUnitType{
	RoleFrontline: {component.UnitLightInfantry, component.UnitHeavyInfantry},
	RoleRanged:    {component.UnitSniper, component.UnitAntiArmorInfantry},
	RoleHeavy:     {component.UnitMotorGun, component.UnitMotorArtillery, component.UnitMotorMissile},
}

type AIState struct {
	SquadID      uint32
	CommanderID  uint32
	State        uint8
	TargetUnitID uint32
	PatrolX      int64
	PatrolY      int64
	NextEvalTick uint32
}

type AICommand struct {
	Type     uint8 // CmdMove, CmdAttack, or CmdRecruit
	SquadID  uint32
	TargetX  int64
	TargetY  int64
	TargetID uint32
	UnitType component.CombatUnitType // for CmdRecruit
}

const (
	CmdMove    uint8 = 1
	CmdAttack  uint8 = 2
	CmdRecruit uint8 = 3
)

type AISystem struct {
	States       map[uint32]*AIState
	AIPlayerID   uint32
	FogSystem    *fog.FogSystem
	MapW, MapH   int32
	Objective    *tilemap.Objective      // objective for AI awareness
	PlayerGold   map[uint32]int32        // reference to session gold pool
	BaseX        int64                   // AI home base position (fixed-point)
	BaseY        int64
	Strongholds  [][2]int32              // stronghold tile positions on map
	EnemyUnits   map[component.CombatUnitType]int // enemy composition intel
	visitedSH    map[int]bool            // strongholds already sent squads to
}

func NewAISystem(aiPlayerID uint32, fogSys *fog.FogSystem, mapW, mapH int32) *AISystem {
	return &AISystem{
		States:     make(map[uint32]*AIState),
		AIPlayerID: aiPlayerID,
		FogSystem:  fogSys,
		MapW:       mapW,
		MapH:       mapH,
		EnemyUnits: make(map[component.CombatUnitType]int),
		visitedSH:  make(map[int]bool),
	}
}

func (as *AISystem) SetObjective(obj *tilemap.Objective) {
	as.Objective = obj
}

// SetBasePosition sets the AI's home base for defense logic.
func (as *AISystem) SetBasePosition(x, y int64) {
	as.BaseX = x
	as.BaseY = y
}

// SetStrongholds provides the list of stronghold positions for capture logic.
func (as *AISystem) SetStrongholds(positions [][2]int32) {
	as.Strongholds = positions
}

func (as *AISystem) RegisterSquad(squadID, commanderID uint32) {
	state := &AIState{
		SquadID:     squadID,
		CommanderID: commanderID,
		State:       StateIdle,
	}
	as.pickPatrolTarget(state)
	as.States[squadID] = state
}

func (as *AISystem) Update(
	tick uint32,
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
) []AICommand {
	var cmds []AICommand
	var aiFog *fog.FogGrid
	if as.FogSystem != nil {
		aiFog = as.FogSystem.GetGrid(as.AIPlayerID)
	}

	// Refresh enemy composition intel each tick
	as.EnemyUnits = make(map[component.CombatUnitType]int)

	// v1: AI recruitment check
	cmds = append(cmds, as.recruitDecisions()...)

	// Track whether base is under threat
	baseThreat := false
	baseDefSq := fixed.FromFloat(BaseDefenseRadius) * fixed.FromFloat(BaseDefenseRadius)

	for squadID, state := range as.States {
		cmdEntity := ecs.Entity(state.CommanderID)
		pos, hasPos := posPool.Get(cmdEntity)
		health, hasHealth := healthPool.Get(cmdEntity)
		if !hasPos || !hasHealth {
			continue
		}

		// Emergency retreat: bypass cooldown
		hpRatio := float64(health.HP) / float64(health.MaxHP)
		if hpRatio < RetreatHPThreshold && hpRatio > 0 && state.State != StateRetreat {
			state.State = StateRetreat
			retreatX := fixed.FromFloat(1.0)
			if fixed.ToFloat(pos.X) > float64(as.MapW)/2 {
				retreatX = fixed.FromFloat(float64(as.MapW) - 2)
			}
			cmds = append(cmds, AICommand{
				Type:    CmdMove,
				SquadID: squadID,
				TargetX: retreatX,
				TargetY: pos.Y,
			})
			continue
		}

		// Cooldown
		if tick < state.NextEvalTick {
			continue
		}
		state.NextEvalTick = tick + EvalInterval

		// v1: Capture objective defense
		if as.Objective != nil && as.Objective.Type == tilemap.ObjectiveCapture {
			cmds = append(cmds, as.captureDefense(squadID, state, pos)...)
			continue
		}

		// Scan for nearest enemy and collect intel
		bestDist, bestEnemyID, bestEnemyX, bestEnemyY := as.scanEnemies(
			cmdPool, posPool, ownerPool, healthPool, unitTypePool, aiFog, pos)

		// --- BASE DEFENSE ---
		// Check if any enemy is near the AI's base
		if as.BaseX != 0 || as.BaseY != 0 {
			nearBase := as.countEnemiesNearBase(
				cmdPool, posPool, ownerPool, healthPool, unitTypePool, aiFog)
			if nearBase >= BaseDefenseThreshold {
				baseThreat = true
				// Recall this squad to defend base (unless already attacking)
				if state.State != StateAttack {
					state.State = StateDefend
					cmds = append(cmds, AICommand{
						Type:    CmdMove,
						SquadID: squadID,
						TargetX: as.BaseX,
						TargetY: as.BaseY,
					})
					continue
				}
			}
		}

		// --- COMBAT (highest priority if enemies visible) ---
		if bestEnemyID != 0 {
			state.TargetUnitID = bestEnemyID
			attackRange := fixed.FromFloat(5.0)
			attackRangeSq := attackRange * attackRange
			if bestDist <= attackRangeSq {
				state.State = StateAttack
				cmds = append(cmds, AICommand{
					Type:     CmdAttack,
					SquadID:  squadID,
					TargetID: bestEnemyID,
				})
			} else {
				state.State = StateApproach
				cmds = append(cmds, AICommand{
					Type:    CmdMove,
					SquadID: squadID,
					TargetX: bestEnemyX,
					TargetY: bestEnemyY,
				})
			}
			continue
		}

		// --- STRATEGIC BEHAVIORS (no enemy visible) ---

		// Early-game exploration: first squad scouts fogged areas
		if tick < ExploreDuration && squadID == as.firstSquadID() {
			scoutCmd := as.exploreCommand(squadID, state, pos, aiFog)
			if scoutCmd != nil {
				cmds = append(cmds, *scoutCmd)
				continue
			}
		}

		// Stronghold capture: send squad to nearest unvisited stronghold
		shCmd := as.strongholdCommand(squadID, state, pos)
		if shCmd != nil {
			cmds = append(cmds, *shCmd)
			continue
		}

		// Default: patrol
		state.State = StatePatrol
		cmds = append(cmds, AICommand{
			Type:    CmdMove,
			SquadID: squadID,
			TargetX: state.PatrolX,
			TargetY: state.PatrolY,
		})
		dx := state.PatrolX - pos.X
		dy := state.PatrolY - pos.Y
		if dx*dx+dy*dy < fixed.FromFloat(4.0)*fixed.FromFloat(4.0)>>12 {
			as.pickPatrolTarget(state)
		}
	}

	// Suppress unused warning — baseThreat could be used for future prioritization
	_ = baseThreat
	_ = baseDefSq

	return cmds
}

// scanEnemies finds the nearest enemy unit and records enemy composition.
// Returns (distSq, enemyID, enemyX, enemyY). enemyID=0 if none found.
func (as *AISystem) scanEnemies(
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	aiFog *fog.FogGrid,
	pos component.PositionComponent,
) (bestDist int64, bestEnemyID uint32, bestEnemyX, bestEnemyY int64) {
	bestDist = -1

	// Scan enemy commanders first (highest priority targets)
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner || owner.PlayerID == as.AIPlayerID {
			return
		}
		ePos, hasEPos := posPool.Get(e)
		if !hasEPos {
			return
		}
		if aiFog != nil && !aiFog.IsVisible(int32(ePos.X>>12), int32(ePos.Y>>12)) {
			return
		}
		// Record enemy commander type
		if ut, ok := unitTypePool.Get(e); ok {
			as.EnemyUnits[ut.Type]++
		}
		dx := ePos.X - pos.X
		dy := ePos.Y - pos.Y
		dist := dx*dx + dy*dy
		if bestDist < 0 || dist < bestDist {
			bestDist = dist
			bestEnemyID = uint32(e)
			bestEnemyX = ePos.X
			bestEnemyY = ePos.Y
		}
	})

	// Fall back to any enemy unit
	if bestEnemyID == 0 {
		healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
			if hp.HP <= 0 {
				return
			}
			owner, hasOwner := ownerPool.Get(e)
			if !hasOwner || owner.PlayerID == as.AIPlayerID {
				return
			}
			ePos, hasEPos := posPool.Get(e)
			if !hasEPos {
				return
			}
			if aiFog != nil && !aiFog.IsVisible(int32(ePos.X>>12), int32(ePos.Y>>12)) {
				return
			}
			// Record enemy unit type
			if ut, ok := unitTypePool.Get(e); ok {
				as.EnemyUnits[ut.Type]++
			}
			dx := ePos.X - pos.X
			dy := ePos.Y - pos.Y
			dist := dx*dx + dy*dy
			if bestDist < 0 || dist < bestDist {
				bestDist = dist
				bestEnemyID = uint32(e)
				bestEnemyX = ePos.X
				bestEnemyY = ePos.Y
			}
		})
	}

	return
}

// countEnemiesNearBase returns how many enemy units are within BaseDefenseRadius of AI base.
func (as *AISystem) countEnemiesNearBase(
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	aiFog *fog.FogGrid,
) int {
	if as.BaseX == 0 && as.BaseY == 0 {
		return 0
	}
	radiusSq := fixed.FromFloat(BaseDefenseRadius) * fixed.FromFloat(BaseDefenseRadius)
	count := 0

	healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner || owner.PlayerID == as.AIPlayerID {
			return
		}
		ePos, hasEPos := posPool.Get(e)
		if !hasEPos {
			return
		}
		dx := ePos.X - as.BaseX
		dy := ePos.Y - as.BaseY
		if dx*dx+dy*dy <= radiusSq {
			count++
		}
	})

	return count
}

// exploreCommand sends a squad to scout a fogged area of the map.
func (as *AISystem) exploreCommand(squadID uint32, state *AIState, pos component.PositionComponent, aiFog *fog.FogGrid) *AICommand {
	if aiFog == nil {
		return nil
	}

	// Pick a fogged sector to explore — try random points toward map center
	// (enemies typically spawn on the opposite side)
	for attempts := 0; attempts < 5; attempts++ {
		// Bias exploration toward the center and opposite side of the map
		enemySide := float64(as.MapW) * 0.25 // toward player's side
		if fixed.ToFloat(pos.X) > float64(as.MapW)/2 {
			enemySide = float64(as.MapW) * 0.75
		}
		tx := int32(enemySide + rand.Float64()*10 - 5)
		ty := int32(rand.Float64() * float64(as.MapH))
		if tx < 1 {
			tx = 1
		}
		if tx >= as.MapW-1 {
			tx = as.MapW - 2
		}
		if ty < 1 {
			ty = 1
		}
		if ty >= as.MapH-1 {
			ty = as.MapH - 2
		}

		if !aiFog.IsVisible(tx, ty) {
			state.State = StateScout
			return &AICommand{
				Type:    CmdMove,
				SquadID: squadID,
				TargetX: fixed.FromFloat(float64(tx)),
				TargetY: fixed.FromFloat(float64(ty)),
			}
		}
	}

	return nil
}

// strongholdCommand sends a squad to the nearest unvisited stronghold.
func (as *AISystem) strongholdCommand(squadID uint32, state *AIState, pos component.PositionComponent) *AICommand {
	if len(as.Strongholds) == 0 {
		return nil
	}

	bestIdx := -1
	bestDist := int64(-1)

	for i, sh := range as.Strongholds {
		if as.visitedSH[i] {
			continue
		}
		shX := fixed.FromFloat(float64(sh[0]))
		shY := fixed.FromFloat(float64(sh[1]))
		dx := shX - pos.X
		dy := shY - pos.Y
		dist := dx*dx + dy*dy
		if bestDist < 0 || dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}

	if bestIdx < 0 {
		// All visited — reset and allow re-visiting
		as.visitedSH = make(map[int]bool)
		return nil
	}

	as.visitedSH[bestIdx] = true
	sh := as.Strongholds[bestIdx]
	state.State = StateCapture
	return &AICommand{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: fixed.FromFloat(float64(sh[0])),
		TargetY: fixed.FromFloat(float64(sh[1])),
	}
}

// firstSquadID returns the first registered squad ID (used for scouting).
func (as *AISystem) firstSquadID() uint32 {
	for id := range as.States {
		return id
	}
	return 0
}

// captureDefense returns commands to move AI squad to the capture target.
func (as *AISystem) captureDefense(squadID uint32, state *AIState, pos component.PositionComponent) []AICommand {
	if as.Objective == nil {
		return nil
	}
	targetX := fixed.FromFloat(float64(as.Objective.TargetX))
	targetY := fixed.FromFloat(float64(as.Objective.TargetY))
	dx := targetX - pos.X
	dy := targetY - pos.Y
	distSq := dx*dx + dy*dy

	// If already at the target, no movement needed
	if distSq < fixed.FromFloat(4.0)*fixed.FromFloat(4.0)>>12 {
		state.State = StateDefend
		return nil
	}

	state.State = StateDefend
	return []AICommand{{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: targetX,
		TargetY: targetY,
	}}
}

// recruitDecisions returns recruit commands based on adaptive role-balanced strategy.
// Role ratios shift based on observed enemy composition.
func (as *AISystem) recruitDecisions() []AICommand {
	if as.PlayerGold == nil {
		return nil
	}
	gold := as.PlayerGold[as.AIPlayerID]
	if gold < 15 { // cheapest unit is LI at 15
		return nil
	}

	// Compute adaptive role ratios based on enemy intel
	adaptiveRatio := as.adaptiveRoleRatio()

	// Count living units by role
	roleCount := [3]int{}

	var cmds []AICommand
	for i := 0; i < 3; i++ { // max 3 recruits per tick
		if gold < 15 {
			break
		}

		// Pick the most underrepresented role using adaptive ratios
		role := pickRoleWithRatio(roleCount, adaptiveRatio)
		if role < 0 {
			role = RoleFrontline // fallback
		}

		// Pick an affordable unit from that role
		ut := as.pickAffordableUnit(role, gold)
		if ut == nil {
			// Can't afford any unit in this role, try cheapest overall
			ut = as.cheapestAffordableUnit(gold)
			if ut == nil {
				break
			}
		}

		cost := component.CombatUnitTypeTable[*ut].RecruitCost
		gold -= cost
		roleCount[unitRole[*ut]]++
		cmds = append(cmds, AICommand{
			Type:     CmdRecruit,
			UnitType: *ut,
		})
	}

	return cmds
}

// adaptiveRoleRatio adjusts target ratios based on observed enemy composition.
// If no enemies seen, returns the default balanced ratio.
func (as *AISystem) adaptiveRoleRatio() [3]float64 {
	total := 0
	roleSeen := [3]int{}
	for ut, count := range as.EnemyUnits {
		total += count
		roleSeen[unitRole[ut]] += count
	}

	if total == 0 {
		return roleTargetRatio // no intel — use default
	}

	// Adjust ratios based on what the enemy is building
	enemyFrontPct := float64(roleSeen[RoleFrontline]) / float64(total)
	enemyRangedPct := float64(roleSeen[RoleRanged]) / float64(total)
	enemyHeavyPct := float64(roleSeen[RoleHeavy]) / float64(total)

	ratio := roleTargetRatio

	// Counter-logic:
	// - Enemy many Ranged (Snipers) → boost Frontline (close distance fast)
	if enemyRangedPct > 0.4 {
		ratio[RoleFrontline] += 0.15
		ratio[RoleRanged] -= 0.10
		ratio[RoleHeavy] -= 0.05
	}

	// - Enemy many Heavy → boost Ranged (Anti-Armor is ranged)
	if enemyHeavyPct > 0.4 {
		ratio[RoleRanged] += 0.15
		ratio[RoleFrontline] -= 0.10
		ratio[RoleHeavy] -= 0.05
	}

	// - Enemy many Frontline → boost Ranged (Snipers pick off infantry)
	if enemyFrontPct > 0.5 {
		ratio[RoleRanged] += 0.10
		ratio[RoleHeavy] -= 0.05
		ratio[RoleFrontline] -= 0.05
	}

	// Clamp ratios to valid range
	for i := range ratio {
		if ratio[i] < 0.10 {
			ratio[i] = 0.10
		}
		if ratio[i] > 0.60 {
			ratio[i] = 0.60
		}
	}

	// Normalize to sum = 1.0
	sum := ratio[0] + ratio[1] + ratio[2]
	for i := range ratio {
		ratio[i] /= sum
	}

	return ratio
}

// pickRoleWithRatio returns the role index most underrepresented relative to given ratios.
func pickRoleWithRatio(roleCount [3]int, ratio [3]float64) int {
	total := roleCount[0] + roleCount[1] + roleCount[2]
	if total == 0 {
		return RoleFrontline
	}

	worstRole := -1
	worstDeficit := 0.0
	for r := 0; r < 3; r++ {
		actual := float64(roleCount[r]) / float64(total)
		deficit := ratio[r] - actual
		if deficit > worstDeficit {
			worstDeficit = deficit
			worstRole = r
		}
	}
	if worstRole < 0 {
		worstRole = rand.Intn(3)
	}
	return worstRole
}

// pickAffordableUnit returns a random affordable unit from the given role.
func (as *AISystem) pickAffordableUnit(role int, gold int32) *component.CombatUnitType {
	candidates := roleUnits[role]
	idx := rand.Intn(len(candidates))
	for i := 0; i < len(candidates); i++ {
		ut := candidates[(idx+i)%len(candidates)]
		if component.CombatUnitTypeTable[ut].RecruitCost <= gold {
			return &ut
		}
	}
	return nil
}

// cheapestAffordableUnit returns the cheapest unit the AI can afford across all roles.
func (as *AISystem) cheapestAffordableUnit(gold int32) *component.CombatUnitType {
	var best *component.CombatUnitType
	var bestCost int32 = math.MaxInt32
	for _, units := range roleUnits {
		for _, ut := range units {
			cost := component.CombatUnitTypeTable[ut].RecruitCost
			if cost <= gold && cost < bestCost {
				u := ut
				best = &u
				bestCost = cost
			}
		}
	}
	return best
}

func (as *AISystem) pickPatrolTarget(state *AIState) {
	margin := 5.0
	state.PatrolX = fixed.FromFloat(margin + rand.Float64()*(float64(as.MapW)-margin*2))
	state.PatrolY = fixed.FromFloat(margin + rand.Float64()*(float64(as.MapH)-margin*2))
}
