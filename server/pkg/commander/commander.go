package commander

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/spatial"
)

type CommanderSystem struct {
	Sh *spatial.Hash

	posPool      *ecs.ComponentPool[component.PositionComponent]
	healthPool   *ecs.ComponentPool[component.HealthComponent]
	cmdPool      *ecs.ComponentPool[component.CommanderComponent]
	boidPool     *ecs.ComponentPool[component.BoidComponent]
}

func (s *CommanderSystem) Name() string  { return "CommanderSystem" }
func (s *CommanderSystem) Priority() int { return 50 } // before Movement (60) and Combat (80)

func (s *CommanderSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.cmdPool = w.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
}

func (s *CommanderSystem) Tick(w *ecs.World, tick uint32) {
	s.cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		// Check if commander just died
		if cmd.IsAlive {
			hp, hasHP := s.healthPool.Get(e)
			if hasHP && hp.HP <= 0 {
				cmd.IsAlive = false
				s.handleCommanderDeath(cmd.SquadID)
				return
			}
		}

		// If alive, apply morale aura to nearby squad members
		if !cmd.IsAlive {
			return
		}

		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}

		ids := s.Sh.Query(pos.X, pos.Y, cmd.AuraRadius)
		for _, id := range ids {
			entity := ecs.Entity(id)
			boid, ok := s.boidPool.Get(entity)
			if !ok || boid.SquadID != cmd.SquadID {
				continue
			}
			if entity == e {
				continue // skip self
			}
			hp, ok := s.healthPool.GetPtr(entity)
			if !ok {
				continue
			}
			hp.Morale = 100 + cmd.AuraMoraleBonus
		}
	})
}

func (s *CommanderSystem) handleCommanderDeath(squadID uint32) {
	// Increase boid weights, decrease formation weight → squad becomes autonomous
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		bc.SeparationW = bc.SeparationW * 3 / 2
		bc.CohesionW = bc.CohesionW * 2
		bc.FormationW = bc.FormationW / 2
	})
}
