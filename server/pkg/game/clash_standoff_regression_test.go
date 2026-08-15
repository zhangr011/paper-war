package game

// TestClashStandoffDoesNotDeadlock — regression: in clash mode both AIs are
// MoveDisabled (armies march via explicit path orders), yet out-of-range
// squads used to freeze forever. Chain: the AI still enters StateApproach/
// StateGuard for a detected enemy; CombatSystem's pursue-skip honored that
// state (combat.go "AI is moving me"); but a MoveDisabled AI never issues
// the move — so nobody closed, nobody was in range, nobody fired. Standoff
// deadlock: units (typically the hill team) stand in weapons range +0.5 and
// never attack.
//
// Fix: session's CombatSystem.StateLookup reports 0 ("not AI-driven") for a
// MoveDisabled AI, so combat pursue closes the gap and firing resumes.
//
// Setup: defender squad on an elevation-1 hill, enemy squad 4.5 tiles east —
// just past the defender's effective fire range (base 3 + 1 high-ground).
// Both armies' path targets pinned to their spawn (no march), replicating a
// mid-match standoff. Healthy behavior: defenders pursue into range and land
// shots within seconds. Buggy behavior (pre-fix): both squads StateApproach,
// pursue skipped, zero attacks ever — match stalls until elimination timeout.

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestClashStandoffDoesNotDeadlock(t *testing.T) {
	// 16x16, elev-1 hill strip at x=2..6, y=7.
	m := tilemap.NewGameMap(16, 16)
	for x := int32(2); x <= 6; x++ {
		tl := m.TileAt(x, 7)
		tl.TerrainType = component.TerrainHill
		tl.Elevation = 1
	}

	gs := NewGameSession()
	gs.ResetWithMap(m)
	gs.EnableClashMode()
	gs.Lifecycle.Phase = PhasePlaying
	// Pin spawn jitter so the standoff geometry (nearest enemy at 4.5 tiles,
	// half a tile beyond effective fire range 4) is deterministic.
	gs.SetSessionRNG(rand.New(rand.NewSource(4)))

	// Defender squad (player 2): commander + 4 grunts on the hill at (4,7).
	gs.SpawnSquadWithType(2, 2, fixed.FromFloat(4.0), fixed.FromFloat(7.0), 4, component.UnitLightInfantry)
	// Enemy squad (player 1): commander + 1 grunt at (8.5,7) — 4.5 tiles out.
	gs.SpawnSquadWithType(1, 1, fixed.FromFloat(8.5), fixed.FromFloat(7.0), 1, component.UnitLightInfantry)

	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	hpPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	atkPool := gs.World.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])

	// Pin both armies' path targets to their spawn — a frozen standoff, no
	// march orders (exactly the live endgame state after both marches stall).
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	pathPool.Each(func(e ecs.Entity, p *component.PathfindingComponent) {
		pp, _ := posPool.Get(e)
		p.TargetX, p.TargetY = pp.X, pp.Y
	})

	gruntHP := func() int32 {
		var hp int32
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
			if bc.Role == component.RoleCommander {
				return
			}
			o, ok := ownerPool.Get(e)
			if !ok || o.Faction != component.FactionPlayer {
				return
			}
			if h, ok2 := hpPool.Get(e); ok2 {
				hp = h.HP
			}
		})
		return hp
	}

	if startHP := gruntHP(); startHP != 100 {
		t.Fatalf("setup: player grunt not found at 100 HP (hp=%d)", startHP)
	}

	for i := 0; i < 100; i++ { // 10s — LI cooldown 0.3s ⇒ dozens of shots possible
		gs.Tick()
	}

	hp := gruntHP()
	atkPool.Each(func(e ecs.Entity, ac *component.AttackComponent) {
		o, _ := ownerPool.Get(e)
		if o.Faction != component.FactionEnemy {
			return
		}
		pp, _ := posPool.Get(e)
		t.Logf("DEF e=%d pos=(%.1f,%.1f) target=%d lastAtk=%d",
			e, fixed.ToFloat(pp.X), fixed.ToFloat(pp.Y), ac.TargetID, ac.LastAttack)
	})

	if hp >= 100 {
		t.Errorf("standoff deadlock: defenders never closed and fired — enemy grunt still HP=%d after 10s (want <100)", hp)
	}
}
