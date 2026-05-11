package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

type DeathSystem struct {
	em *ecs.EntityManager

	healthPool       *ecs.ComponentPool[component.HealthComponent]
	posPool          *ecs.ComponentPool[component.PositionComponent]
	velPool          *ecs.ComponentPool[component.VelocityComponent]
	boidPool         *ecs.ComponentPool[component.BoidComponent]
	attackPool       *ecs.ComponentPool[component.AttackComponent]
	cmdPool          *ecs.ComponentPool[component.CommanderComponent]
	movePool         *ecs.ComponentPool[component.MovementComponent]
	pathPool         *ecs.ComponentPool[component.PathfindingComponent]
	formationPool    *ecs.ComponentPool[component.FormationComponent]
	formationRolePool *ecs.ComponentPool[component.FormationRoleComponent]
	ownerPool        *ecs.ComponentPool[component.OwnerComponent]
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
}

func (s *DeathSystem) Tick(w *ecs.World, tick uint32) {
	var dead []ecs.Entity
	s.healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			dead = append(dead, e)
		}
	})

	for _, e := range dead {
		// Handle commander death before removing
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

		s.em.Destroy(e)
	}
}

func (s *DeathSystem) handleCommanderDeath(squadID uint32) {
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		bc.SeparationW = bc.SeparationW * 3 / 2
		bc.CohesionW = bc.CohesionW * 2
		bc.FormationW = bc.FormationW / 2
	})
}
