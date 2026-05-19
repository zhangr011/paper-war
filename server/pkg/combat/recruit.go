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

	// Validate weapon slot (Formation Template)
	if s.unitTypePool != nil {
		if cmdUT, ok := s.unitTypePool.Get(req.CommanderEntity); ok {
			// Check that the requested unit's weapon category matches the formation slot
			unitWeapon := typeStats.Weapon
			cmdWeaponSlot := cmdUT.Weapon

			// Light weapons: Gun, Sniper
			// Heavy weapons: Cannon, Missile
			unitCategory := weaponCategory(unitWeapon)
			slotCategory := weaponCategory(cmdWeaponSlot)
			if unitCategory != slotCategory {
				return // weapon doesn't match formation slot
			}
		}
	}

	// Count current squad size
	squadID := uint32(0)
	if bc, ok := s.boidPool.Get(req.CommanderEntity); ok {
		squadID = bc.SquadID
	}
	squadSize := 0
	s.currentSquadCost(squadID, &squadSize)

	// Check Leading Skill budget (total squad cost <= Leading Skill)
	// For v1: Leading Skill = commander cost budget from Formation Template
	// Simplified: max squad size based on commander level
	maxSquadSize := 10 // base
	if s.unitTypePool != nil {
		if ut, ok := s.unitTypePool.Get(req.CommanderEntity); ok {
			maxSquadSize = int(10 + ut.Level*2)
		}
	}
	if squadSize >= maxSquadSize {
		return // squad is full
	}

	// Spawn the unit at commander position with small offset
	newEntity := s.em.Create()
	s.healthPool.Add(newEntity, component.HealthComponent{HP: typeStats.HP, MaxHP: typeStats.HP})
	s.attackPool.Add(newEntity, component.AttackComponent{
		Damage:  typeStats.Damage,
		Range:   typeStats.Range,
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

func (s *RecruitmentSystem) currentSquadCost(squadID uint32, count *int) {
	if s.boidPool == nil {
		return
	}
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID == squadID && bc.Role != component.RoleCommander {
			*count++
		}
	})
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
