package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// MaxStructuresPerPlayer caps how many structures a player can build.
const MaxStructuresPerPlayer = 10

// BuildRange is the max distance (in tiles) from player spawn where structures can be placed.
const BuildRange = 10.0

// BuildSystem handles placement of defensive structures.
type BuildSystem struct {
	em            *ecs.EntityManager
	posPool       *ecs.ComponentPool[component.PositionComponent]
	healthPool    *ecs.ComponentPool[component.HealthComponent]
	ownerPool     *ecs.ComponentPool[component.OwnerComponent]
	structPool    *ecs.ComponentPool[component.StructureComponent]
	attackPool    *ecs.ComponentPool[component.AttackComponent]
	unitTypePool  *ecs.ComponentPool[component.UnitTypeComponent]
	PlayerGold    map[uint32]int32
	GoldDeductions map[uint32]int32 // per-tick deductions (shared with recruit)
	// PlayerSpawns[playerID] = (x, y) in fixed-point
	PlayerSpawns map[uint32][2]int64
}

func (s *BuildSystem) Name() string  { return "BuildSystem" }
func (s *BuildSystem) Priority() int { return 65 } // before Recruitment(70)

func (s *BuildSystem) Init(w *ecs.World) {
	// Issue #40 (found in QA): s.em was never assigned in Init, causing
	// a nil-pointer panic in Build() the first time the player tried to
	// place a structure. Other systems (RecruitmentSystem, etc.) set
	// this via `s.em = w.Entities()` — BuildSystem was missing it.
	s.em = w.Entities()
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	s.structPool = w.Pool(component.StructureComponent{}).(*ecs.ComponentPool[component.StructureComponent])
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
}

type BuildRequest struct {
	PlayerID  uint32
	Type      component.StructureType
	X, Y      int64 // fixed-point position
}

func (s *BuildSystem) Tick(w *ecs.World, tick uint32) {
	s.GoldDeductions = make(map[uint32]int32)
}

// Build attempts to place a structure. Returns nil on success.
func (s *BuildSystem) Build(req BuildRequest) bool {
	// Lazy-init maps
	if s.GoldDeductions == nil {
		s.GoldDeductions = map[uint32]int32{}
	}

	stats, ok := component.StructureTypeTable[req.Type]
	if !ok {
		return false
	}

	// Check gold
	if s.PlayerGold != nil {
		gold, hasGold := s.PlayerGold[req.PlayerID]
		deducted := s.GoldDeductions[req.PlayerID]
		if !hasGold || gold-deducted < stats.Cost {
			return false
		}
	}

	// Check structure count
	if s.countStructures(req.PlayerID) >= MaxStructuresPerPlayer {
		return false
	}

	// Check placement range from spawn
	if s.PlayerSpawns != nil {
		spawn, ok := s.PlayerSpawns[req.PlayerID]
		if ok {
			dx := fixed.ToFloat(req.X - spawn[0])
			dy := fixed.ToFloat(req.Y - spawn[1])
			dist := dx*dx + dy*dy
			if dist > BuildRange*BuildRange {
				return false
			}
		}
	}

	// Deduct gold
	if s.PlayerGold != nil && stats.Cost > 0 {
		s.GoldDeductions[req.PlayerID] += stats.Cost
		s.PlayerGold[req.PlayerID] -= stats.Cost
	}

	// Create the structure entity
	e := s.em.Create()
	s.posPool.Add(e, component.PositionComponent{X: req.X, Y: req.Y})
	s.healthPool.Add(e, component.HealthComponent{HP: stats.HP, MaxHP: stats.HP})
	s.structPool.Add(e, component.StructureComponent{
		Type:    req.Type,
		OwnerID: req.PlayerID,
	})

	// Turret gets an attack component
	if req.Type == component.StructureTurret {
		s.attackPool.Add(e, component.AttackComponent{
			Damage:   stats.Damage,
			Range:    fixed.FromFloat(float64(stats.Range)),
			Cooldown: uint8(stats.Cooldown),
		})
	}

	// Watchtower and Barricade are immobile — no movement/pathing components
	// Set owner
	if s.ownerPool != nil {
		faction := component.FactionPlayer
		if req.PlayerID == 2 {
			faction = component.FactionEnemy
		}
		s.ownerPool.Add(e, component.OwnerComponent{PlayerID: req.PlayerID, Faction: faction})
	}

	return true
}

func (s *BuildSystem) countStructures(playerID uint32) int {
	count := 0
	s.structPool.Each(func(e ecs.Entity, sc *component.StructureComponent) {
		if sc.OwnerID == playerID {
			count++
		}
	})
	return count
}

// StrongholdDefenseBonus returns the damage reduction multiplier (0-100) for a
// unit standing on a stronghold tile. Level 1 = 25% reduction, Level 5 = 50%.
func StrongholdDefenseBonus(strongholdLevel int) int32 {
	if strongholdLevel < 1 {
		return 0
	}
	if strongholdLevel > 5 {
		strongholdLevel = 5
	}
	// L1=25%, L2=31%, L3=37%, L4=43%, L5=50%
	return int32(25 + (strongholdLevel-1)*6)
}

// TerrainCoverBonus returns the damage-reduction % (0-100) a CombatUnit gains
// from the terrain it stands on. Forest = 25% (cover/concealment), Hill = 15%
// (high ground); other terrains grant none. Tunable — issue #55 phase 1.
// Walls are impassable so no unit stands on them; Building/stronghold tiles
// route through StrongholdDefenseBonus instead.
func TerrainCoverBonus(terrain component.TerrainType) int32 {
	switch terrain {
	case component.TerrainForest:
		return 25
	case component.TerrainHill:
		return 15
	default:
		return 0
	}
}
