package terrain

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type TerrainEvent struct {
	X, Y       int32
	NewTerrain  component.TerrainType
}

type TerrainSystem struct {
	Gm    *tilemap.GameMap
	Cache *pathfinding.Cache

	events    []TerrainEvent
	profiles  []*component.MovementProfile

	// applied holds the terrain changes applied this tick. The snapshot
	// builder drains it into EventTerrainChange wire events so clients update
	// live terrain. Reset each Tick after the snapshot encoder has consumed it.
	// Mirrors the DeathRecords/AttackRecords pattern in combat. Phase 3.
	applied []TerrainEvent
}

func NewTerrainSystem(gm *tilemap.GameMap, cache *pathfinding.Cache, profiles []*component.MovementProfile) *TerrainSystem {
	return &TerrainSystem{
		Gm:       gm,
		Cache:    cache,
		profiles: profiles,
	}
}

// DrainApplied returns the terrain changes applied this tick and clears the
// buffer. The session's snapshot builder calls this between ticks to emit
// EventTerrainChange wire events. Phase 3.
func (s *TerrainSystem) DrainApplied() []TerrainEvent {
	if len(s.applied) == 0 {
		return nil
	}
	out := s.applied
	s.applied = nil
	return out
}

func (s *TerrainSystem) Name() string  { return "TerrainSystem" }
func (s *TerrainSystem) Priority() int { return 30 }
func (s *TerrainSystem) Init(w *ecs.World) {}

func (s *TerrainSystem) Tick(w *ecs.World, tick uint32) {
	if len(s.events) == 0 {
		return
	}

	// Collect affected cache keys (target positions that pass through changed tiles)
	// For simplicity, invalidate all cached flow fields when terrain changes
	for range s.events {
		s.Cache.InvalidateAll()
		break // InvalidateAll once is enough
	}

	// Apply terrain changes
	for _, evt := range s.events {
		s.Gm.SetTerrain(evt.X, evt.Y, evt.NewTerrain)
		// Update tile health if applicable
		tile := s.Gm.TileAt(evt.X, evt.Y)
		if tile != nil {
			tile.Health = 0
			tile.MaxHealth = 0
		}
		// Record so the snapshot builder can emit EventTerrainChange (Phase 3).
		s.applied = append(s.applied, evt)
	}

	s.events = s.events[:0]
}

// QueueEvent queues a terrain change for next tick processing.
func (s *TerrainSystem) QueueEvent(evt TerrainEvent) {
	s.events = append(s.events, evt)
}

// ProcessDestruction handles a destructible object taking damage.
func (s *TerrainSystem) ProcessDestruction(x, y int32, damage int32) {
	tile := s.Gm.TileAt(x, y)
	if tile == nil || tile.MaxHealth == 0 {
		return
	}
	tile.Health -= damage
	if tile.Health <= 0 {
		newTerrain := s.getDestroyedTerrain(tile.TerrainType)
		s.QueueEvent(TerrainEvent{X: x, Y: y, NewTerrain: newTerrain})
	}
}

func (s *TerrainSystem) getDestroyedTerrain(tt component.TerrainType) component.TerrainType {
	switch tt {
	case component.TerrainBridge:
		return component.TerrainDeep
	case component.TerrainWall:
		return component.TerrainPlain
	case component.TerrainRock:
		return component.TerrainPlain
	case component.TerrainForest:
		return component.TerrainPlain
	default:
		return component.TerrainPlain
	}
}
