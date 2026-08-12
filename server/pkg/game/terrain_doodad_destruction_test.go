package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/combat"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestLiveDoodadDestruction verifies the Phase 3 doodad-conversion path fires
// through a real gs.Tick() loop along the path that actually destroys doodads
// in live play: a cannon shot at a unit standing on a destructible Forest tile
// → CombatSystem.collectSplash at the target's tile → TileDamageFn →
// TerrainSystem.ProcessDestruction → QueueEvent → TerrainSystem.Tick converts
// the tile to Plain.
//
// (Walls/Rocks are impassable, so no unit stands on them — they are breached
// only via the player AttackGround command. Forest is the walkable destructible
// and the realistic live case.)
//
// Depends on the ResetWithMap fix in session.go: without it, the TerrainSystem
// the scheduler ticks is a stale instance and queued conversions never drain.
func TestLiveDoodadDestruction(t *testing.T) {
	// 16x16 map; Forest at (10,2) with low HP so a single cannon splash converts
	// it (HI cmd dmg 50 → Cannon-vs-Light 25 → splashDmg 12 ≥ 10 HP).
	m := tilemap.NewGameMap(16, 16)
	m.SetTerrain(10, 2, component.TerrainForest)
	m.TileAt(10, 2).Health = 10
	m.TileAt(10, 2).MaxHealth = 10

	gs := NewGameSession()
	gs.ResetWithMap(m)
	gs.EnableClashMode()
	gs.Lifecycle.Phase = PhasePlaying
	// Pin player 2's AI: ResetWithMap recreates AISys after EnableClashMode, so
	// re-assert MoveDisabled here. The target must stay on the Forest tile so
	// the cannon splash epicentre lands on it (otherwise splash hits a Plain the
	// target walked onto, whose MaxHealth is 0 and is ignored).
	if gs.AISys != nil {
		gs.AISys.MoveDisabled = true
		gs.AISys.RecruitDisabled = true
	}

	// Wiring guard: combat must forward tile damage to the TerrainSystem.
	cs := gs.World.SystemByName("CombatSystem").(*combat.CombatSystem)
	if cs.TileDamageFn == nil {
		t.Fatal("CombatSystem.TileDamageFn not wired after ResetWithMap")
	}

	// Target: LightInfantry commander (player 2) ON the Forest at (10,2).
	// Attacker: HeavyInfantry commander (player 1, Cannon, range 7) at (5,2) —
	// distance 5. Both in range (LI range 5, HI range 7), so neither pursues
	// and the target stays planted on the Forest. The cannon's collectSplash
	// damages the Forest at the target's tile each shot.
	gs.SpawnSquadWithType(2, 2, fixed.FromFloat(10.0), fixed.FromFloat(2.0), 0, component.UnitLightInfantry)
	gs.SpawnSquadWithType(1, 1, fixed.FromFloat(5.0), fixed.FromFloat(2.0), 0, component.UnitHeavyInfantry)

	// HI cooldown 5 → first shot ~tick 5; one 12-dmg splash converts the 10-HP Forest.
	for i := 0; i < 20; i++ {
		gs.Tick()
	}

	tl := gs.Map.TileAt(10, 2)
	if tl.TerrainType != component.TerrainPlain {
		t.Errorf("forest tile after cannon fire: terrain=%d, want Plain (splash→TileDamageFn→ProcessDestruction did not convert it live)", tl.TerrainType)
	}
	if tl.Health != 0 || tl.MaxHealth != 0 {
		t.Errorf("forest tile HP=%d/%d, want 0/0 (health not zeroed on conversion)", tl.Health, tl.MaxHealth)
	}
}
