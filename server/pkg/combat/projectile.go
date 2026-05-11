package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

type ProjectileSystem struct {
	em         *ecs.EntityManager
	posPool    *ecs.ComponentPool[component.PositionComponent]
	healthPool *ecs.ComponentPool[component.HealthComponent]
	projPool   *ecs.ComponentPool[component.ProjectileComponent]
}

func (s *ProjectileSystem) Name() string  { return "ProjectileSystem" }
func (s *ProjectileSystem) Priority() int { return 85 }

func (s *ProjectileSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	if p := w.Pool(component.ProjectileComponent{}); p != nil {
		s.projPool = p.(*ecs.ComponentPool[component.ProjectileComponent])
	}
}

func (s *ProjectileSystem) Tick(w *ecs.World, tick uint32) {
	if s.projPool == nil {
		return
	}

	var expired []ecs.Entity
	s.projPool.Each(func(e ecs.Entity, proj *component.ProjectileComponent) {
		pos, ok := s.posPool.GetPtr(e)
		if !ok {
			return
		}

		// Move toward target
		pos.X += proj.DX
		pos.Y += proj.DY

		// Check if reached target area or impact tick
		dx := proj.TargetX - pos.X
		dy := proj.TargetY - pos.Y
		distSq := fixed.DistSq(dx, dy)
		arrived := distSq <= fixed.FromFloat(0.5) || tick >= proj.ImpactTick

		if arrived {
			// Apply splash damage
			if proj.SplashRadius > 0 {
				// Use spatial hash if available (passed via struct, but we don't have it here)
				// Fall back to checking all health entities
				s.healthPool.Each(func(target ecs.Entity, hp *component.HealthComponent) {
					tpos, ok := s.posPool.Get(target)
					if !ok {
						return
					}
					tdx := tpos.X - pos.X
					tdy := tpos.Y - pos.Y
					td := fixed.DistSq(tdx, tdy)
					if td <= fixed.FromFloat(0.5)+proj.SplashRadius*proj.SplashRadius>>12 {
						dmg := proj.Damage
						if hp.Armor > 0 {
							dmg -= hp.Armor
						}
						if dmg < 1 {
							dmg = 1
						}
						hp.HP -= dmg
					}
				})
			} else {
				// Single target: find nearest entity at impact point
				var bestDist int64 = ^int64(0)
				var bestEntity ecs.Entity
				s.healthPool.Each(func(target ecs.Entity, hp *component.HealthComponent) {
					tpos, ok := s.posPool.Get(target)
					if !ok {
						return
					}
					tdx := tpos.X - pos.X
					tdy := tpos.Y - pos.Y
					td := fixed.DistSq(tdx, tdy)
					if td < bestDist {
						bestDist = td
						bestEntity = target
					}
				})
				if bestEntity != 0 {
					hp, ok := s.healthPool.GetPtr(bestEntity)
					if ok {
						dmg := proj.Damage - hp.Armor
						if dmg < 1 {
							dmg = 1
						}
						hp.HP -= dmg
					}
				}
			}
			expired = append(expired, e)
		}
	})

	// Remove expired projectiles
	for _, e := range expired {
		s.projPool.Remove(e)
		s.posPool.Remove(e)
		s.em.Destroy(e)
	}
}
