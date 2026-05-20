package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

type DeathSystem struct {
	em *ecs.EntityManager

	healthPool        *ecs.ComponentPool[component.HealthComponent]
	posPool           *ecs.ComponentPool[component.PositionComponent]
	velPool           *ecs.ComponentPool[component.VelocityComponent]
	boidPool          *ecs.ComponentPool[component.BoidComponent]
	attackPool        *ecs.ComponentPool[component.AttackComponent]
	cmdPool           *ecs.ComponentPool[component.CommanderComponent]
	movePool          *ecs.ComponentPool[component.MovementComponent]
	pathPool          *ecs.ComponentPool[component.PathfindingComponent]
	formationPool     *ecs.ComponentPool[component.FormationComponent]
	formationRolePool *ecs.ComponentPool[component.FormationRoleComponent]
	ownerPool         *ecs.ComponentPool[component.OwnerComponent]
	killPointsPool    *ecs.ComponentPool[component.KillPointsComponent]
	unitTypePool      *ecs.ComponentPool[component.UnitTypeComponent]

	// GoldBounties collects {playerID: bounty} for each tick.
	// Cleared at start of each Tick. Session reads this after Tick().
	GoldBounties map[uint32]int32
}

func (s *DeathSystem) Name() string  { return "DeathSystem" }
func (s *DeathSystem) Priority() int { return 90 }

func (s *DeathSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	if p := w.Pool(component.CommanderComponent{}); p != nil {
		s.cmdPool = p.(*ecs.ComponentPool[component.CommanderComponent])
	}
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	if p := w.Pool(component.FormationComponent{}); p != nil {
		s.formationPool = p.(*ecs.ComponentPool[component.FormationComponent])
	}
	if p := w.Pool(component.FormationRoleComponent{}); p != nil {
		s.formationRolePool = p.(*ecs.ComponentPool[component.FormationRoleComponent])
	}
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.KillPointsComponent{}); p != nil {
		s.killPointsPool = p.(*ecs.ComponentPool[component.KillPointsComponent])
	}
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
}

func (s *DeathSystem) Tick(w *ecs.World, tick uint32) {
	s.GoldBounties = make(map[uint32]int32)

	var dead []ecs.Entity
	s.healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			dead = append(dead, e)
		}
	})

	for _, e := range dead {
		hp, _ := s.healthPool.Get(e)

		// Award kill points to the killer
		if hp.LastAttacker != 0 && s.killPointsPool != nil {
			killerEntity := ecs.Entity(hp.LastAttacker)
			if killerHP, ok := s.healthPool.Get(killerEntity); ok && killerHP.HP > 0 {
				if kp, ok := s.killPointsPool.GetPtr(killerEntity); ok {
					kp.Points += s.killPointValue(e)
				}

				// Award Gold bounty to killer's player
				if s.ownerPool != nil && s.unitTypePool != nil {
					if killerOwner, ok := s.ownerPool.Get(killerEntity); ok {
						if deadUT, ok := s.unitTypePool.Get(e); ok {
							bounty := component.CombatUnitTypeTable[deadUT.Type].KillBounty
							if bounty > 0 {
								s.GoldBounties[killerOwner.PlayerID] += bounty
							}
						}
					}
				}
			}
		}

		// Handle commander death: promote highest-level unit in squad
		if s.cmdPool != nil {
			if cmd, ok := s.cmdPool.Get(e); ok && cmd.IsAlive {
				cmd.IsAlive = false
				s.handleCommanderDeath(cmd.SquadID)
			}
		}

		// Clear attack targets referencing this entity
		targetID := uint32(e)
		s.attackPool.Each(func(other ecs.Entity, ac *component.AttackComponent) {
			if ac.TargetID == targetID {
				ac.TargetID = 0
			}
		})

		// Remove all components
		s.healthPool.Remove(e)
		s.posPool.Remove(e)
		s.velPool.Remove(e)
		s.boidPool.Remove(e)
		s.attackPool.Remove(e)
		s.movePool.Remove(e)
		s.pathPool.Remove(e)
		if s.cmdPool != nil {
			s.cmdPool.Remove(e)
		}
		if s.formationPool != nil {
			s.formationPool.Remove(e)
		}
		if s.formationRolePool != nil {
			s.formationRolePool.Remove(e)
		}
		if s.ownerPool != nil {
			s.ownerPool.Remove(e)
		}
		if s.killPointsPool != nil {
			s.killPointsPool.Remove(e)
		}
		if s.unitTypePool != nil {
			s.unitTypePool.Remove(e)
		}

		s.em.Destroy(e)
	}
}

// killPointValue returns the kill point value of a dying entity.
// Commanders are worth more.
func (s *DeathSystem) killPointValue(dead ecs.Entity) int32 {
	if s.cmdPool != nil {
		if _, ok := s.cmdPool.Get(dead); ok {
			return 5 // Commander kill bonus
		}
	}
	return 1
}

// handleCommanderDeath promotes the highest-level CombatUnit in the squad
// to Commander. The promoted unit retains its CombatUnitType (types never convert).
func (s *DeathSystem) handleCommanderDeath(squadID uint32) {
	var bestEntity ecs.Entity
	var bestLevel uint8

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}

		level := uint8(0)
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(e); ok {
				level = ut.Level
			}
		}

		if level > bestLevel || (level == bestLevel && bestEntity == 0) {
			bestEntity = e
			bestLevel = level
		}
	})

	if bestEntity == 0 {
		return // no one to promote
	}

	// Promote: change BoidRole to Commander
	bc, _ := s.boidPool.GetPtr(bestEntity)
	bc.Role = component.RoleCommander

	// Add CommanderComponent if not present
	if s.cmdPool != nil {
		s.cmdPool.Add(bestEntity, component.CommanderComponent{
			SquadID:   squadID,
			IsAlive:   true,
			AuraRadius: bc.NeighborRange,
		})
	}
}
