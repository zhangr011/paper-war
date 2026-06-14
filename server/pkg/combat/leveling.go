package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// CombatUnitLevelThresholds defines kill points needed for each CombatUnit level-up.
// Level 1->2 needs 2 KP, 2->3 needs 4, 3->4 needs 8, 4->5 needs 16, 5->6 needs 32.
var CombatUnitLevelThresholds = [6]int32{0, 2, 4, 8, 16, 32}

// CommanderLevelMultipliers defines HP and damage multipliers for Commander levels 1-10.
// Index 0 = level 1 (base), index 9 = level 10 (3x).
var CommanderLevelMultipliers = [10]struct {
	HP  int32 // multiplier in percent (100 = 1.0x)
	Dmg int32
}{
	{100, 100}, // lv1: 1.0x / 1.0x
	{140, 120}, // lv2: 1.4x / 1.2x
	{180, 140}, // lv3: 1.8x / 1.4x
	{220, 160}, // lv4: 2.2x / 1.6x
	{260, 180}, // lv5: 2.6x / 1.8x
	{300, 200}, // lv6: 3.0x / 2.0x
	{340, 220}, // lv7: 3.4x / 2.2x
	{380, 240}, // lv8: 3.8x / 2.4x
	{420, 260}, // lv9: 4.2x / 2.6x
	{500, 300}, // lv10: 5.0x / 3.0x
}

// LevelingSystem checks kill point thresholds and applies level-ups each tick.
type LevelingSystem struct {
	healthPool    *ecs.ComponentPool[component.HealthComponent]
	attackPool    *ecs.ComponentPool[component.AttackComponent]
	killPointsPool *ecs.ComponentPool[component.KillPointsComponent]
	unitTypePool  *ecs.ComponentPool[component.UnitTypeComponent]
	boidPool      *ecs.ComponentPool[component.BoidComponent]
	cmdPool       *ecs.ComponentPool[component.CommanderComponent]
}

func (s *LevelingSystem) Name() string  { return "LevelingSystem" }
func (s *LevelingSystem) Priority() int { return 85 } // after Combat(80), before Death(90)

func (s *LevelingSystem) Init(w *ecs.World) {
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	if p := w.Pool(component.KillPointsComponent{}); p != nil {
		s.killPointsPool = p.(*ecs.ComponentPool[component.KillPointsComponent])
	}
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	if p := w.Pool(component.CommanderComponent{}); p != nil {
		s.cmdPool = p.(*ecs.ComponentPool[component.CommanderComponent])
	}
}

func (s *LevelingSystem) Tick(w *ecs.World, tick uint32) {
	if s.killPointsPool == nil || s.unitTypePool == nil {
		return
	}

	s.killPointsPool.Each(func(e ecs.Entity, kp *component.KillPointsComponent) {
		utPtr, ok := s.unitTypePool.GetPtr(e)
		if !ok {
			return
		}

		isCommander := false
		if s.boidPool != nil {
			if bc, ok := s.boidPool.Get(e); ok && bc.Role == component.RoleCommander {
				isCommander = true
			}
		}

		if isCommander {
			s.tryCommanderLevelUp(e, utPtr, kp)
		} else {
			s.tryCombatUnitLevelUp(e, utPtr, kp)
		}
	})
}

// tryCombatUnitLevelUp checks thresholds for CombatUnits (max level 6).
func (s *LevelingSystem) tryCombatUnitLevelUp(e ecs.Entity, ut *component.UnitTypeComponent, kp *component.KillPointsComponent) {
	for ut.Level < 6 && int(ut.Level) < len(CombatUnitLevelThresholds) {
		threshold := CombatUnitLevelThresholds[ut.Level]
		if kp.Points < threshold {
			break
		}
		kp.Points -= threshold
		ut.Level++

		// +10% MaxHP per level
		if hp, ok := s.healthPool.GetPtr(e); ok {
			baseMaxHP := hp.MaxHP * 10 / (10 + int32(ut.Level-1))
			hp.MaxHP = baseMaxHP * (10 + int32(ut.Level)) / 10
			// Also heal the HP increase
			hp.HP += baseMaxHP / 10
		}
	}
}

// tryCommanderLevelUp checks thresholds for Commanders (max level 10).
// Uses 64 kill points per level (same as the highest CombatUnit threshold).
func (s *LevelingSystem) tryCommanderLevelUp(e ecs.Entity, ut *component.UnitTypeComponent, kp *component.KillPointsComponent) {
	for ut.Level < 10 {
		threshold := int32(2 + ut.Level*2) // 4, 6, 8, 10, 12, 14, 16, 18, 20
		if kp.Points < threshold {
			break
		}
		kp.Points -= threshold
		ut.Level++

		// Apply Commander multiplier from table
		idx := int(ut.Level - 1)
		if idx >= len(CommanderLevelMultipliers) {
			idx = len(CommanderLevelMultipliers) - 1
		}
		mult := CommanderLevelMultipliers[idx]

		// Get base stats from CombatUnitType
		typeStats := component.CombatUnitTypeTable[ut.Type]

		if hp, ok := s.healthPool.GetPtr(e); ok {
			hp.MaxHP = typeStats.HP * mult.HP / 100
			if hp.HP > hp.MaxHP {
				hp.HP = hp.MaxHP
			}
		}
		if ac, ok := s.attackPool.GetPtr(e); ok {
			ac.Damage = typeStats.Damage * mult.Dmg / 100
		}
	}
}
