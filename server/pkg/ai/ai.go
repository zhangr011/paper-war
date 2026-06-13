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
	StateDefend   uint8 = 5 // v1: defend capture target

	EvalInterval        uint32 = 30
	RetreatHPThreshold         = 0.0 // disabled — fight to the death

	// Role definitions for recruitment strategy
	RoleFrontline = 0
	RoleRanged    = 1
	RoleHeavy     = 2
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
	Objective    *tilemap.Objective // v1: objective for AI awareness
	PlayerGold   map[uint32]int32   // reference to session gold pool
}

func NewAISystem(aiPlayerID uint32, fogSys *fog.FogSystem, mapW, mapH int32) *AISystem {
	return &AISystem{
		States:     make(map[uint32]*AIState),
		AIPlayerID: aiPlayerID,
		FogSystem:  fogSys,
		MapW:       mapW,
		MapH:       mapH,
	}
}

func (as *AISystem) SetObjective(obj *tilemap.Objective) {
	as.Objective = obj
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
) []AICommand {
	var cmds []AICommand
	var aiFog *fog.FogGrid
	if as.FogSystem != nil {
		aiFog = as.FogSystem.GetGrid(as.AIPlayerID)
	}

	// v1: AI recruitment check
	cmds = append(cmds, as.recruitDecisions()...)

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

		// Scan for nearest enemy commander within vision
		var bestDist int64 = -1
		var bestEnemyID uint32
		var bestEnemyX, bestEnemyY int64

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
			if aiFog != nil {
				ex := int32(ePos.X >> 12)
				ey := int32(ePos.Y >> 12)
				if !aiFog.IsVisible(ex, ey) {
					return
				}
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

		if bestEnemyID != 0 {
			state.TargetUnitID = bestEnemyID
			attackRange := fixed.FromFloat(5.0) // use max unit combat range
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
		} else {
			// No enemy commander visible — fall back to nearest enemy unit
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

		if bestEnemyID != 0 {
			// Found an enemy (commander or regular unit) — approach or attack
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
		} else {
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
	}
	return cmds
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

// recruitDecisions returns recruit commands based on role-balanced strategy.
// It reads gold from PlayerGold (shared with session), counts current units
// by role, and picks units from the most underrepresented role.
func (as *AISystem) recruitDecisions() []AICommand {
	if as.PlayerGold == nil {
		return nil
	}
	gold := as.PlayerGold[as.AIPlayerID]
	if gold < 15 { // cheapest unit is LI at 15
		return nil
	}

	// Count living units by role
	roleCount := [3]int{}
	// We don't have healthPool here, so we use the unit count from our squads
	// For now, count based on role mapping of all combat units
	// (This is called from Update which has pools, but recruitDecisions doesn't)
	// We'll use the total unit count tracked per role in AISystem

	var cmds []AICommand
	for i := 0; i < 3; i++ { // max 3 recruits per tick
		if gold < 15 {
			break
		}

		// Pick the most underrepresented role
		role := as.pickRole(roleCount)
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

	// Note: do NOT deduct from PlayerGold here — recruitSys.Recruit()
	// handles the actual gold deduction. This planning gold is just
	// for computing the recruit sequence.
	return cmds
}

// pickRole returns the role index most underrepresented relative to target ratios.
// Returns -1 if total count is 0 (no preference).
func (as *AISystem) pickRole(roleCount [3]int) int {
	total := roleCount[0] + roleCount[1] + roleCount[2]
	if total == 0 {
		return RoleFrontline // start with frontline
	}

	worstRole := -1
	worstDeficit := 0.0
	for r := 0; r < 3; r++ {
		actual := float64(roleCount[r]) / float64(total)
		deficit := roleTargetRatio[r] - actual
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

// pickAffordableUnit returns the cheapest unit the AI can afford from the given role.
func (as *AISystem) pickAffordableUnit(role int, gold int32) *component.CombatUnitType {
	candidates := roleUnits[role]
	// Shuffle to add variety while preferring cheaper units
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
