package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestDeathRecordCapturesPosition (issue #28) verifies that when a unit
// dies, DeathSystem records its last known position so the snapshot can
// emit an enriched EventDeath payload (entityID+X+Y+tick).  Without this,
// the client would have to anchor the die animation at the interpolated
// render position, which may have drifted past the actual death tile.
func TestDeathRecordCapturesPosition(t *testing.T) {
	gs := NewGameSession()
	gs.EnableClashMode()

	// Spawn two squads close enough to kill each other.
	gs.SpawnSquadWithType(1, 1, 48, 48, 1, component.UnitLightInfantry)
	gs.SpawnSquadWithType(2, 2, 48, 50, 1, component.UnitLightInfantry)

	// Drop every unit to 1 HP so the next damage tick kills it.
	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])

	type startPos struct {
		id uint32
		x  int64
		y  int64
	}
	var starts []startPos
	healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		hp.HP = 1
		hp.MaxHP = 1
		if pos, ok := posPool.Get(e); ok {
			starts = append(starts, startPos{id: uint32(e), x: pos.X, y: pos.Y})
		}
	})
	if len(starts) < 2 {
		t.Fatalf("expected at least 2 units, got %d", len(starts))
	}

	gs.Lifecycle.Phase = PhasePlaying

	// Tick until at least one unit dies.
	var records []combat.DeathRecord
	for tick := 0; tick < 200; tick++ {
		gs.Tick()
		if gs.deathSys != nil && len(gs.deathSys.DeathRecords) > 0 {
			records = append(records, gs.deathSys.DeathRecords...)
			break
		}
	}
	if len(records) == 0 {
		t.Fatal("no deaths recorded in 200 ticks (expected at least one)")
	}

	// Verify each record has the correct entityID and a plausible
	// position near where the unit started (within 4 tiles).  This is
	// the property the client relies on: the die animation should
	// anchor to the death location, not to some far-away interp tile.
	for _, rec := range records {
		var match *startPos
		for i := range starts {
			if starts[i].id == rec.EntityID {
				match = &starts[i]
				break
			}
		}
		if match == nil {
			t.Errorf("DeathRecord.EntityID=%d not found in start positions", rec.EntityID)
			continue
		}
		dx := fixed.ToFloat(rec.X - match.x)
		dy := fixed.ToFloat(rec.Y - match.y)
		if dx < -4.0 || dx > 4.0 || dy < -4.0 || dy > 4.0 {
			t.Errorf("DeathRecord for entity %d drifted too far: dx=%.2f dy=%.2f",
				rec.EntityID, dx, dy)
		}
		if rec.Tick == 0 {
			t.Errorf("DeathRecord.Tick=0, expected non-zero simulation tick")
		}
	}
}
