package combat

import (
	"fmt"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// RecruitRequest is a request to recruit a new CombatUnit.
type RecruitRequest struct {
	CommanderEntity ecs.Entity
	UnitType        component.CombatUnitType
}

// RecruitmentSystem handles Recruit requests each tick.
// It validates Formation Template slots, checks Gold and Leading Skill budgets,
// and spawns units at the Commander's position.
type RecruitmentSystem struct {
	em           *ecs.EntityManager
	healthPool   *ecs.ComponentPool[component.HealthComponent]
	attackPool   *ecs.ComponentPool[component.AttackComponent]
	posPool      *ecs.ComponentPool[component.PositionComponent]
	velPool      *ecs.ComponentPool[component.VelocityComponent]
	boidPool     *ecs.ComponentPool[component.BoidComponent]
	movePool     *ecs.ComponentPool[component.MovementComponent]
	pathPool     *ecs.ComponentPool[component.PathfindingComponent]
	ownerPool    *ecs.ComponentPool[component.OwnerComponent]
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent]
	cmdPool      *ecs.ComponentPool[component.CommanderComponent]

	// Pending recruit requests for this tick
	pending []RecruitRequest

	// PlayerGold is set by Session before each tick. Maps playerID → current gold.
	PlayerGold map[uint32]int32

	// GoldDeductions collects {playerID: amount} for each successful recruit.
	// Cleared at start of each Tick. Session reads this after Tick().
	GoldDeductions map[uint32]int32
}

func (s *RecruitmentSystem) Name() string  { return "RecruitmentSystem" }
func (s *RecruitmentSystem) Priority() int { return 70 } // before Combat(80)

func (s *RecruitmentSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
	if p := w.Pool(component.CommanderComponent{}); p != nil {
		s.cmdPool = p.(*ecs.ComponentPool[component.CommanderComponent])
	}
}

// Recruit enqueues a recruit request for the next tick.
func (s *RecruitmentSystem) Recruit(req RecruitRequest) error {
	s.pending = append(s.pending, req)
	return nil
}

func (s *RecruitmentSystem) Tick(w *ecs.World, tick uint32) {
	s.GoldDeductions = make(map[uint32]int32)
	for _, req := range s.pending {
		s.processRecruit(req)
	}
	s.pending = s.pending[:0]
}

func (s *RecruitmentSystem) processRecruit(req RecruitRequest) {
	// Validate commander exists and is alive
	cmdHP, ok := s.healthPool.Get(req.CommanderEntity)
	if !ok || cmdHP.HP <= 0 {
		return
	}

	cmdPos, ok := s.posPool.Get(req.CommanderEntity)
	if !ok {
		return
	}

	// Get unit type stats
	typeStats, ok := component.CombatUnitTypeTable[req.UnitType]
	if !ok {
		return
	}

	// Get player ID and check Gold
	var playerID uint32
	if s.ownerPool != nil {
		if owner, ok := s.ownerPool.Get(req.CommanderEntity); ok {
			playerID = owner.PlayerID
		}
	}
	recruitCost := typeStats.RecruitCost
	if s.PlayerGold != nil {
		gold, hasGold := s.PlayerGold[playerID]
		// Account for already-deducted gold this tick
		alreadyDeducted := s.GoldDeductions[playerID]
		if !hasGold || gold-alreadyDeducted < recruitCost {
			return // not enough gold
		}
	}

	// Get commander's squad and Leading Skill budget
	squadID := uint32(0)
	leadingSkill := int32(10) // default budget
	if s.unitTypePool != nil {
		if cmdUT, ok := s.unitTypePool.Get(req.CommanderEntity); ok {
			leadingSkill = 5 + int32(cmdUT.Level)*2 // v1 formula: 5 + (level * 2)
		}
	}
	if bc, ok := s.boidPool.Get(req.CommanderEntity); ok {
		squadID = bc.SquadID
	}

	// Count current squad cost (sum of CombatUnitTypeTable[].Cost for each unit)
	currentCost := s.currentSquadCost(squadID)
	newUnitCost := typeStats.Cost
	if currentCost+newUnitCost > leadingSkill {
		return // over budget
	}

	// Deduct gold
	if s.PlayerGold != nil && recruitCost > 0 {
		s.GoldDeductions[playerID] += recruitCost
	}

	// Spawn the unit at commander position with small offset
	newEntity := s.em.Create()
	s.healthPool.Add(newEntity, component.HealthComponent{HP: typeStats.HP, MaxHP: typeStats.HP})
	s.attackPool.Add(newEntity, component.AttackComponent{
		Damage:  typeStats.Damage,
		Range:   fixed.FromFloat(float64(typeStats.Range)),
		Cooldown: typeStats.Cooldown,
	})
	s.posPool.Add(newEntity, component.PositionComponent{
		X: cmdPos.X + fixed.FromFloat(0.5),
		Y: cmdPos.Y + fixed.FromFloat(0.5),
	})
	s.velPool.Add(newEntity, component.VelocityComponent{})
	s.boidPool.Add(newEntity, component.BoidComponent{
		SquadID:       squadID,
		Role:          component.RoleMelee,
		SeparationW:   fixed.FromFloat(1.5),
		NeighborRange: fixed.FromFloat(5.0),
	})
	s.movePool.Add(newEntity, component.MovementComponent{})
	s.pathPool.Add(newEntity, component.PathfindingComponent{})
	s.unitTypePool.Add(newEntity, component.UnitTypeComponent{
		Type:   req.UnitType,
		Weapon: typeStats.Weapon,
		Armor:  typeStats.Armor,
		Level:  1,
	})

	// Copy owner from commander
	if s.ownerPool != nil {
		if owner, ok := s.ownerPool.Get(req.CommanderEntity); ok {
			s.ownerPool.Add(newEntity, owner)
		}
	}
}

func (s *RecruitmentSystem) currentSquadCost(squadID uint32) int32 {
	total := int32(0)
	if s.boidPool == nil || s.unitTypePool == nil {
		return total
	}
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == squadID && bc.Role != component.RoleCommander {
			if ut, ok := s.unitTypePool.Get(e); ok {
				total += component.CombatUnitTypeTable[ut.Type].Cost
			} else {
				total++ // default cost 1
			}
		}
	})
	return total
}

// weaponCategory returns "Light" or "Heavy" for a weapon type.
func weaponCategory(w component.WeaponType) string {
	switch w {
	case component.WeaponGun, component.WeaponSniper:
		return "Light"
	case component.WeaponCannon, component.WeaponMissile:
		return "Heavy"
	default:
		return ""
	}
}

// KillBounty returns the gold bounty for killing a unit of the given type.
func KillBounty(unitType component.CombatUnitType) int32 {
	stats, ok := component.CombatUnitTypeTable[unitType]
	if !ok {
		return 0
	}
	return stats.KillBounty
}

// ValidateRecruit checks if a recruit request is valid without executing it.
func (s *RecruitmentSystem) ValidateRecruit(req RecruitRequest) error {
	_, ok := component.CombatUnitTypeTable[req.UnitType]
	if !ok {
		return fmt.Errorf("unknown unit type: %d", req.UnitType)
	}

	cmdHP, ok := s.healthPool.Get(req.CommanderEntity)
	if !ok || cmdHP.HP <= 0 {
		return fmt.Errorf("commander not alive")
	}

	return nil
}
