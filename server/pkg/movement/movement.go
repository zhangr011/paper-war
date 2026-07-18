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
	formationRolePool *ecs.ComponentPool[component.FormationRoleComponent]
	ownerPool         *ecs.ComponentPool[component.OwnerComponent]
}

const PositionDivisor = 10

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
	if p := w.Pool(component.FormationRoleComponent{}); p != nil {
		s.formationRolePool = p.(*ecs.ComponentPool[component.FormationRoleComponent])
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

	// Build map of squadID -> commander position
	commanderPos := make(map[uint32][2]int64)
	if s.cmdPool != nil {
		s.cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
			if !cmd.IsAlive {
				return
			}
			if pos, ok := s.posPool.Get(e); ok {
				commanderPos[cmd.SquadID] = [2]int64{pos.X, pos.Y}
			}
		})
	}

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		pos, ok := s.posPool.GetPtr(e)
		if !ok {
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
			}
		}

		neighborPos := s.queryNeighborPositions(pos.X, pos.Y, bc.NeighborRange, uint64(e))

		// Commanders should not be repelled by their own squad members —
		// they need to lead, not flee from their own units. Filter out
		// same-squad neighbors so only enemies/obstacles cause repulsion.
		if bc.Role == component.RoleCommander {
			neighborPos = s.queryNeighborPositionsExcludeSquad(pos.X, pos.Y, bc.NeighborRange, uint64(e), bc.SquadID)
		}

		sepFX, sepFY := boid.SeparationForce([2]int64{pos.X, pos.Y}, neighborPos, bc.NeighborRange)

		// Attraction force (toward commander/beacon/flow field target)
		var attrFX, attrFY int64

		// Commander-following force (or beacon steering for player units)
		if useBeacon {
			attrFX, attrFY = boid.AttractionForce([2]int64{pos.X, pos.Y}, *s.BeaconPos)
		} else if bc.Role != component.RoleCommander {
			if cpos, ok := commanderPos[bc.SquadID]; ok {
				target := [2]int64{cpos[0], cpos[1]}
				if s.formationRolePool != nil {
					if fr, ok := s.formationRolePool.Get(e); ok {
						target[0] += fr.OffsetX
						target[1] += fr.OffsetY
					}
				}
				attrFX, attrFY = boid.AttractionForce([2]int64{pos.X, pos.Y}, target)
			}
		}

		totalFX := flowFX +
			fixed.Mul(sepFX, bc.SeparationW) +
			fixed.Mul(attrFX, bc.FormationW)
		totalFY := flowFY +
			fixed.Mul(sepFY, bc.SeparationW) +
			fixed.Mul(attrFY, bc.FormationW)

		maxForce := fixed.FromFloat(5.0)
		if hasVel && vel.Speed > maxForce {
			maxForce = vel.Speed
		}
		totalFX = fixed.Clamp(totalFX, -maxForce, maxForce)
		totalFY = fixed.Clamp(totalFY, -maxForce, maxForce)

		if hasVel {
			speed := vel.Speed
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

// queryNeighborPositionsExcludeSquad returns neighbor positions, skipping any
// entity that shares the given squadID (used by commanders to avoid being
// repelled by their own squad members).
func (s *MovementSystem) queryNeighborPositionsExcludeSquad(x, y, range_ int64, exclude uint64, squadID uint32) [][2]int64 {
	ids := s.Sh.Query(x, y, range_)
	var result [][2]int64
	for _, id := range ids {
		if id == exclude {
			continue
		}
		ent := ecs.Entity(id)
		if bc, ok := s.boidPool.Get(ent); ok && bc.SquadID == squadID {
			continue
		}
		if pos, ok := s.posPool.Get(ent); ok {
			result = append(result, [2]int64{pos.X, pos.Y})
		}
	}
	return result
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
