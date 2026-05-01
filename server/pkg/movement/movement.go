package movement

import (
	"github.com/user/paper-war/server/pkg/boid"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type MovementSystem struct {
	Gm       *tilemap.GameMap
	Cache    *pathfinding.Cache
	Sh       *spatial.Hash
	Profiles map[uint8]*component.MovementProfile

	posPool  *ecs.ComponentPool[component.PositionComponent]
	velPool  *ecs.ComponentPool[component.VelocityComponent]
	boidPool *ecs.ComponentPool[component.BoidComponent]
	movePool *ecs.ComponentPool[component.MovementComponent]
	pathPool *ecs.ComponentPool[component.PathfindingComponent]
}

func (s *MovementSystem) Name() string  { return "MovementSystem" }
func (s *MovementSystem) Priority() int { return 60 }

func (s *MovementSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
}

func (s *MovementSystem) Tick(w *ecs.World, tick uint32) {
	s.Sh.Clear()
	s.posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		s.Sh.Insert(uint64(e), pos.X, pos.Y)
	})

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		pos, ok := s.posPool.GetPtr(e)
		if !ok {
			return
		}
		vel, hasVel := s.velPool.GetPtr(e)

		var flowFX, flowFY int64
		if path, ok := s.pathPool.Get(e); ok {
			profile := s.Profiles[0]
			if mc, ok := s.movePool.Get(e); ok {
				if p, exists := s.Profiles[mc.ProfileID]; exists {
					profile = p
				}
			}
			tileX := int32(pos.X >> 12)
			tileY := int32(pos.Y >> 12)
			ff := s.Cache.Get(int32(path.TargetX>>12), int32(path.TargetY>>12), profile)
			dir := ff.GetDirection(tileX, tileY)
			flowW := fixed.FromFloat(2.5)
			flowFX = fixed.Mul(dir.DX, flowW)
			flowFY = fixed.Mul(dir.DY, flowW)
		}

		neighborPos := s.queryNeighborPositions(pos.X, pos.Y, bc.NeighborRange, uint64(e))
		sepFX, sepFY := boid.SeparationForce([2]int64{pos.X, pos.Y}, neighborPos, bc.NeighborRange)
		cohFX, cohFY := boid.CohesionForce([2]int64{pos.X, pos.Y}, neighborPos)

		var aliFX, aliFY int64
		if hasVel {
			neighborVels := s.queryNeighborVelocities(pos.X, pos.Y, bc.NeighborRange, uint64(e))
			aliFX, aliFY = boid.AlignmentForce([2]int64{vel.Vx, vel.Vy}, neighborVels)
		}

		totalFX := flowFX +
			fixed.Mul(sepFX, bc.SeparationW) +
			fixed.Mul(cohFX, bc.CohesionW) +
			fixed.Mul(aliFX, bc.AlignmentW)
		totalFY := flowFY +
			fixed.Mul(sepFY, bc.SeparationW) +
			fixed.Mul(cohFY, bc.CohesionW) +
			fixed.Mul(aliFY, bc.AlignmentW)

		maxForce := fixed.FromFloat(5.0)
		totalFX = fixed.Clamp(totalFX, -maxForce, maxForce)
		totalFY = fixed.Clamp(totalFY, -maxForce, maxForce)

		if hasVel {
			speed := vel.Speed
			vel.Vx = fixed.Clamp(totalFX, -speed, speed)
			vel.Vy = fixed.Clamp(totalFY, -speed, speed)
			pos.X += vel.Vx
			pos.Y += vel.Vy
		} else {
			pos.X += totalFX
			pos.Y += totalFY
		}
	})
}

func (s *MovementSystem) queryNeighborPositions(x, y, range_ int64, exclude uint64) [][2]int64 {
	ids := s.Sh.Query(x, y, range_)
	var result [][2]int64
	for _, id := range ids {
		if id == exclude {
			continue
		}
		if pos, ok := s.posPool.Get(ecs.Entity(id)); ok {
			result = append(result, [2]int64{pos.X, pos.Y})
		}
	}
	return result
}

func (s *MovementSystem) queryNeighborVelocities(x, y, range_ int64, exclude uint64) [][2]int64 {
	ids := s.Sh.Query(x, y, range_)
	var result [][2]int64
	for _, id := range ids {
		if id == exclude {
			continue
		}
		if vel, ok := s.velPool.Get(ecs.Entity(id)); ok {
			result = append(result, [2]int64{vel.Vx, vel.Vy})
		}
	}
	return result
}
