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

	BeaconPos *[2]int64 // set by GameSession before tick; nil = no beacon

	posPool           *ecs.ComponentPool[component.PositionComponent]
	velPool           *ecs.ComponentPool[component.VelocityComponent]
	boidPool          *ecs.ComponentPool[component.BoidComponent]
	movePool          *ecs.ComponentPool[component.MovementComponent]
	pathPool          *ecs.ComponentPool[component.PathfindingComponent]
	cmdPool           *ecs.ComponentPool[component.CommanderComponent]
	ownerPool         *ecs.ComponentPool[component.OwnerComponent]
}

const PositionDivisor = 10

// defaultEntitySpeed is the fallback movement speed used when an entity's
// VelocityComponent.Speed is zero (a guard rail against spawn paths that
// forget to set Speed, which would otherwise permanently freeze the entity).
// Matches the order of magnitude of defaultCombatUnitSpeed for a standard map.
const defaultEntitySpeed = 820 // ≈ fixed.FromFloat(0.2) in 12.4 fixed-point

func (s *MovementSystem) Name() string  { return "MovementSystem" }
func (s *MovementSystem) Priority() int { return 60 }

func (s *MovementSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	if p := w.Pool(component.CommanderComponent{}); p != nil {
		s.cmdPool = p.(*ecs.ComponentPool[component.CommanderComponent])
	}
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
}

func (s *MovementSystem) Tick(w *ecs.World, tick uint32) {
	s.Sh.Clear()

	// 1. Insert all entities into spatial hash
	s.posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		s.Sh.Insert(uint64(e), pos.X, pos.Y)
	})

	// Build maps of squadID -> commander position and suppressing flag.
	commanderPos := make(map[uint32][2]int64)
	suppressing := make(map[uint32]bool) // squad is suppressing its surge (ADR-0025)
	if s.cmdPool != nil {
		s.cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
			if !cmd.IsAlive {
				return
			}
			if pos, ok := s.posPool.Get(e); ok {
				commanderPos[cmd.SquadID] = [2]int64{pos.X, pos.Y}
			}
			if cmd.Suppressing {
				suppressing[cmd.SquadID] = true
			}
		})
	}

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		pos, ok := s.posPool.GetPtr(e)
		if !ok {
			return
		}
		// Attack freeze (#52, server-side): the unit is planted during its
		// attack swing — skip movement so the position doesn't advance and
		// the client never accumulates a teleport-inducing delta.
		if tick < bc.FreezeUntilTick {
			return
		}
		// Garrisoned units don't move — they're inside a stronghold (#54 1B).
		if bc.GarrisonedIn != 0 {
			return
		}
		vel, hasVel := s.velPool.GetPtr(e)

		// Check if this unit should follow the beacon
		useBeacon := false
		if s.BeaconPos != nil && s.ownerPool != nil {
			if owner, ok := s.ownerPool.Get(e); ok && owner.Faction == component.FactionPlayer {
				useBeacon = true
			}
		}

		// Flow-field force (skip when beacon is active for player units)
		var flowFX, flowFY int64
		if !useBeacon {
			if path, ok := s.pathPool.Get(e); ok {
				profile := s.Profiles[0]
				if mc, ok := s.movePool.Get(e); ok {
					if p, exists := s.Profiles[mc.ProfileID]; exists {
						profile = p
					}
				}
				tileX := int32(pos.X >> 12)
				tileY := int32(pos.Y >> 12)
				if tileX < 0 {
					tileX = 0
				}
				if tileY < 0 {
					tileY = 0
				}
				if tileX >= s.Gm.Width {
					tileX = s.Gm.Width - 1
				}
				if tileY >= s.Gm.Height {
					tileY = s.Gm.Height - 1
				}
				ff := s.Cache.Get(
				clamp32(int32(path.TargetX>>12), 0, s.Gm.Width-1),
				clamp32(int32(path.TargetY>>12), 0, s.Gm.Height-1),
				profile)
				dir := ff.GetDirection(tileX, tileY)
				flowW := fixed.FromFloat(2.5)
				if hasVel && vel.Speed > flowW {
					flowW = vel.Speed
				}
				flowFX = fixed.Mul(dir.DX, flowW)
				flowFY = fixed.Mul(dir.DY, flowW)

				// Drift centering: zero flow for non-commander units of suppressing squads
				if bc.Role != component.RoleCommander && suppressing[bc.SquadID] {
					flowFX, flowFY = 0, 0
				}
			}
		}

		// Attraction force. Two sources:
		//  - player beacon (active move order) for player units;
		//  - commander-attraction (cohesion): non-commander units steer toward
		//    their commander's position so the squad clusters on it.
		var attrFX, attrFY int64
		if useBeacon {
			attrFX, attrFY = boid.AttractionForce([2]int64{pos.X, pos.Y}, *s.BeaconPos)
		} else if bc.Role != component.RoleCommander {
			if cpos, ok := commanderPos[bc.SquadID]; ok {
				attrFX, attrFY = boid.AttractionForce([2]int64{pos.X, pos.Y}, cpos)
			}
		}

		totalFX := flowFX +
			fixed.Mul(attrFX, bc.AttractionW)
		totalFY := flowFY +
			fixed.Mul(attrFY, bc.AttractionW)

		maxForce := fixed.FromFloat(5.0)
		if hasVel && vel.Speed > maxForce {
			maxForce = vel.Speed
		}
		totalFX = fixed.Clamp(totalFX, -maxForce, maxForce)
		totalFY = fixed.Clamp(totalFY, -maxForce, maxForce)

		if hasVel {
			speed := vel.Speed
			if speed <= 0 {
				// Guard rail: a zero/negative Speed (e.g. a spawn path that
				// forgot to set it) would clamp velocity to 0 and permanently
				// freeze the entity. Fall back to a sane default instead.
				speed = defaultEntitySpeed
			}
			vel.Vx = fixed.Clamp(totalFX, -speed, speed)
			vel.Vy = fixed.Clamp(totalFY, -speed, speed)
			pos.X += vel.Vx / PositionDivisor
			pos.Y += vel.Vy / PositionDivisor
		} else {
			pos.X += totalFX
			pos.Y += totalFY
		}

		mapMaxX := int64(s.Gm.Width) << fixed.FractionBits
		mapMaxY := int64(s.Gm.Height) << fixed.FractionBits
		pos.X = fixed.Clamp(pos.X, 0, mapMaxX)
		pos.Y = fixed.Clamp(pos.Y, 0, mapMaxY)
	})
}

func clamp32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
