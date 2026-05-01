package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/spatial"
)

type CombatSystem struct {
	Sh *spatial.Hash

	posPool    *ecs.ComponentPool[component.PositionComponent]
	healthPool *ecs.ComponentPool[component.HealthComponent]
	attackPool *ecs.ComponentPool[component.AttackComponent]
	boidPool   *ecs.ComponentPool[component.BoidComponent]
}

func (s *CombatSystem) Name() string  { return "CombatSystem" }
func (s *CombatSystem) Priority() int { return 80 }

func (s *CombatSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
}

func (s *CombatSystem) Tick(w *ecs.World, tick uint32) {
	s.attackPool.Each(func(e ecs.Entity, ac *component.AttackComponent) {
		if ac.Cooldown > 0 && tick-ac.LastAttack < uint32(ac.Cooldown) {
			return
		}

		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}

		// Auto-acquire target if none
		if ac.TargetID == 0 {
			ids := s.Sh.Query(pos.X, pos.Y, ac.Range)
			selfID := uint64(e)
			selfBoid, hasSelfBoid := s.boidPool.Get(e)
			for _, id := range ids {
				if id == selfID {
					continue
				}
				if hasSelfBoid {
					if otherBoid, ok := s.boidPool.Get(ecs.Entity(id)); ok {
						if selfBoid.SquadID == otherBoid.SquadID {
							continue // skip allies
						}
					}
				}
				ac.TargetID = uint32(id)
				break
			}
		}

		if ac.TargetID == 0 {
			return
		}

		// Check target alive and in range
		targetPos, ok := s.posPool.Get(ecs.Entity(ac.TargetID))
		if !ok {
			ac.TargetID = 0
			return
		}

		dx := targetPos.X - pos.X
		dy := targetPos.Y - pos.Y
		distSq := (dx*dx + dy*dy) >> 12
		rangeSq := (ac.Range * ac.Range) >> 12

		if distSq > rangeSq {
			ac.TargetID = 0
			return
		}

		// Instant damage for melee/ranged
		if ac.AttackType != component.AttackArtillery {
			targetHealth, ok := s.healthPool.GetPtr(ecs.Entity(ac.TargetID))
			if !ok {
				ac.TargetID = 0
				return
			}
			dmg := ac.Damage - targetHealth.Armor
			if dmg < 1 {
				dmg = 1
			}
			targetHealth.HP -= dmg
			ac.LastAttack = tick
		}
	})
}
