package ai

import (
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
)

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
	AIRecruitGold int32             // v1: gold available for AI recruitment
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

// recruitDecisions returns recruit commands when AI has enough gold.
func (as *AISystem) recruitDecisions() []AICommand {
	if as.AIRecruitGold < 15 {
		return nil
	}
	var cmds []AICommand
	// Simple strategy: recruit cheapest available units
	// For v1, just recruit Light Infantry
	for as.AIRecruitGold >= 15 {
		as.AIRecruitGold -= 15
		cmds = append(cmds, AICommand{
			Type:     CmdRecruit,
			UnitType: component.UnitLightInfantry,
		})
		if len(cmds) >= 3 { // max 3 recruits per tick
			break
		}
	}
	return cmds
}

func (as *AISystem) pickPatrolTarget(state *AIState) {
	margin := 5.0
	state.PatrolX = fixed.FromFloat(margin + rand.Float64()*(float64(as.MapW)-margin*2))
	state.PatrolY = fixed.FromFloat(margin + rand.Float64()*(float64(as.MapH)-margin*2))
}
