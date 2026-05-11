package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

type CombatSystem struct {
	Sh *spatial.Hash
	em *ecs.EntityManager

	posPool    *ecs.ComponentPool[component.PositionComponent]
	healthPool *ecs.ComponentPool[component.HealthComponent]
	attackPool *ecs.ComponentPool[component.AttackComponent]
	boidPool   *ecs.ComponentPool[component.BoidComponent]
	ownerPool  *ecs.ComponentPool[component.OwnerComponent]
	projPool   *ecs.ComponentPool[component.ProjectileComponent]
}

func (s *CombatSystem) Name() string  { return "CombatSystem" }
func (s *CombatSystem) Priority() int { return 80 }

func (s *CombatSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.ProjectileComponent{}); p != nil {
		s.projPool = p.(*ecs.ComponentPool[component.ProjectileComponent])
	}
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
				if s.ownerPool != nil {
					if selfOwner, ok := s.ownerPool.Get(e); ok {
						if otherOwner, ok := s.ownerPool.Get(ecs.Entity(id)); ok {
							if selfOwner.Faction == otherOwner.Faction {
								continue // skip same faction
							}
						}
					}
				} else if hasSelfBoid {
					if otherBoid, ok := s.boidPool.Get(ecs.Entity(id)); ok {
						if selfBoid.SquadID == otherBoid.SquadID {
							continue // fallback: skip same squad
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
		} else {
			// Artillery: spawn projectile entity
			if s.projPool != nil && s.em != nil {
				proj := s.em.Create()
				s.posPool.Add(proj, component.PositionComponent{X: pos.X, Y: pos.Y})
				tpos, _ := s.posPool.Get(ecs.Entity(ac.TargetID))
				dx := tpos.X - pos.X
				dy := tpos.Y - pos.Y
				speed := fixed.FromFloat(2.0)
				dist := fixed.ISqrt(fixed.DistSq(dx, dy))
				var vdx, vdy int64
				if dist > 0 {
					vdx = fixed.Mul(fixed.Div(dx, dist), speed)
					vdy = fixed.Mul(fixed.Div(dy, dist), speed)
				}
				s.projPool.Add(proj, component.ProjectileComponent{
					DX:           vdx,
					DY:           vdy,
					TargetX:      tpos.X,
					TargetY:      tpos.Y,
					Damage:       ac.Damage,
					ImpactTick:   tick + 5,
					SplashRadius: fixed.FromFloat(1.5),
				})
				ac.LastAttack = tick
			}
		}
	})
}
