// Package creep implements the spreading faction-terrain overlay
// (the StarCraft "creep" equivalent) — Phase 4 of
// terrain-starcraft-plan.md.
//
// Creep is an OVERLAY stored in Tile.CreepOwner (0 = none, 1/2 = faction),
// not a terrain type. Each faction's alive commanders act as creep sources.
// Every SpreadInterval ticks a fresh multi-source BFS re-derives the creep
// field out to MaxDistance tiles from each source (orthogonal steps only,
// across standard-profile walkable tiles). A unit whose creep-faction
// matches a tile's CreepOwner gets a movement discount there (applied in
// tilemap.CostAtFor / EdgeWalkableFor, not here).
package creep

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// System parameters. SpreadInterval=5 (~0.5s at 10Hz) keeps recompute cost
// trivial while making spread visible. MaxDistance=6 tiles gives a localized
// "home field" around each commander rather than carpeting the whole map.
const (
	SpreadInterval uint8 = 5
	MaxDistance    int32 = 6
)

// CreepSystem re-derives the per-tile CreepOwner overlay every SpreadInterval
// ticks from alive commander positions. It implements ecs.System.
type CreepSystem struct {
	Gm      *tilemap.GameMap
	World   *ecs.World
	Profile *component.MovementProfile // standard profile for walkability gates

	// CreepFactionOf maps an OwnerComponent.Faction to its creep-owner index
	// (1 or 2). The default mapper handles FactionPlayer→1 / FactionEnemy→2.
	// Override for tests. nil → DefaultCreepFactionOf.
	CreepFactionOf func(faction uint8) uint8
}

// DefaultCreepFactionOf maps an owner faction constant to its 1-based creep
// index. FactionPlayer(0) → 1, FactionEnemy(1) → 2. Any other value (e.g.
// FactionNeutral) maps to 0 (no creep).
func DefaultCreepFactionOf(faction uint8) uint8 {
	switch faction {
	case component.FactionPlayer:
		return 1
	case component.FactionEnemy:
		return 2
	default:
		return 0
	}
}

func (s *CreepSystem) Name() string     { return "creep" }
func (s *CreepSystem) Priority() int    { return 30 } // before MovementSystem (60)
func (s *CreepSystem) Init(w *ecs.World) {}

// Tick advances the creep overlay. Every SpreadInterval ticks it (1) gathers
// alive commander sources, (2) resets the grid, (3) seeds each source's tile,
// (4) runs a bounded BFS per faction spreading into unclaimed walkable tiles.
func (s *CreepSystem) Tick(w *ecs.World, tick uint32) {
	if s.Gm == nil || s.World == nil {
		return
	}
	if uint8(tick)%SpreadInterval != 0 {
		return
	}
	factionOf := s.CreepFactionOf
	if factionOf == nil {
		factionOf = DefaultCreepFactionOf
	}

	// Reset: creep is recomputed fresh each spread tick, so a dead commander's
	// creep recedes (matches SC2 semantics where creep dies without a source).
	tiles := s.Gm.Tiles
	for i := range tiles {
		tiles[i].CreepOwner = 0
	}

	// Gather sources: each alive commander's tile + creep-faction.
	type src struct {
		x, y   int32
		faction uint8 // 1 or 2
	}
	var sources []src
	cmdPool := s.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	posPool := s.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := s.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	healthPool := s.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive {
			return
		}
		if hp, ok := healthPool.Get(e); ok && hp.HP <= 0 {
			return
		}
		pos, ok := posPool.Get(e)
		if !ok {
			return
		}
		owner, ok := ownerPool.Get(e)
		if !ok {
			return
		}
		cf := factionOf(owner.Faction)
		if cf == 0 {
			return
		}
		sources = append(sources, src{x: int32(pos.X >> 12), y: int32(pos.Y >> 12), faction: cf})
	})
	if len(sources) == 0 {
		return
	}

	w32, h32 := s.Gm.Width, s.Gm.Height
	inBounds := func(x, y int32) bool { return x >= 0 && x < w32 && y >= 0 && y < h32 }

	// Seed source tiles. Clamp into bounds (a commander off-map shouldn't
	// panic the BFS); an out-of-bounds source contributes nothing.
	for _, sr := range sources {
		if !inBounds(sr.x, sr.y) {
			continue
		}
		s.Gm.Tiles[sr.y*w32+sr.x].CreepOwner = sr.faction
	}

	// Per-faction BFS into unclaimed (CreepOwner==0) walkable tiles. Faction 1
	// spreads first, so on exact distance ties it wins the contested tile —
	// deterministic and immaterial since sources are normally far apart.
	dirs4 := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for faction := uint8(1); faction <= 2; faction++ {
		// Frontier = tiles already claimed by this faction at the current
		// distance. Start from this faction's source tiles (distance 0).
		var frontier []src
		for _, sr := range sources {
			if sr.faction != faction {
				continue
			}
			if inBounds(sr.x, sr.y) {
				frontier = append(frontier, src{x: sr.x, y: sr.y, faction: faction})
			}
		}
		for dist := int32(0); dist < MaxDistance && len(frontier) > 0; dist++ {
			next := make([]src, 0, len(frontier)*4)
			for _, cur := range frontier {
				for _, d := range dirs4 {
					nx, ny := cur.x+d[0], cur.y+d[1]
					if !inBounds(nx, ny) {
						continue
					}
					ni := ny*w32 + nx
					if tiles[ni].CreepOwner != 0 {
						continue // claimed by anyone this tick
					}
					// Spread only across walkable ground (cost 0 = impassable).
					if s.Gm.CostAt(nx, ny, s.Profile) == 0 {
						continue
					}
					tiles[ni].CreepOwner = faction
					next = append(next, src{x: nx, y: ny, faction: faction})
				}
			}
			frontier = next
		}
	}
}
