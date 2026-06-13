package game

import (
	"fmt"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// runClashSim spawns two teams with the given unit types, simulates up to maxTicks,
// and returns (winnerFaction, winnerHP%, totalTicks).
// Teams spawn 15 tiles apart. Team A at (10,10), Team B at (25,10).
func runClashSim(t *testing.T, teamAType, teamBType component.CombatUnitType, teamLevel uint8, maxTicks uint32) (winnerFaction uint8, winnerHPPct float64, ticks uint32) {
	t.Helper()
	gs := NewGameSession()
	gs.ResetWithMap(tilemap.LoadClashMap("plains"))

	// Spawn team A (faction 0)
	squadA := uint32(1)
	gs.SpawnTeamWithType(1, squadA,
		fixed.FromFloat(10), fixed.FromFloat(10),
		teamLevel, teamAType)

	// Spawn team B (faction 1)
	squadB := uint32(2)
	gs.SpawnTeamWithType(2, squadB,
		fixed.FromFloat(18), fixed.FromFloat(10),
		teamLevel, teamBType)

	// Set AI targets: each team targets the other's commander
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

	boidPool.Each(func(e ecs.Entity, b *component.BoidComponent) {
		if b.SquadID == squadA {
			// Team A targets Team B commander position
			if pc, ok := pathPool.Get(e); ok {
				pc.TargetX = fixed.FromFloat(18)
				pc.TargetY = fixed.FromFloat(10)
			}
		} else if b.SquadID == squadB {
			// Team B targets Team A commander position
			if pc, ok := pathPool.Get(e); ok {
				pc.TargetX = fixed.FromFloat(10)
				pc.TargetY = fixed.FromFloat(10)
			}
		}
	})

	healthPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

	// Simulate
	for tick := uint32(1); tick <= maxTicks; tick++ {
		gs.Tick()

		// Count survivors per side
		var aAlive, bAlive int
		var aHP, bHP int32
		var aMaxHP, bMaxHP int32

		healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
			if h.HP <= 0 {
				return
			}
			owner, ok := ownerPool.Get(e)
			if !ok {
				return
			}
			if owner.Faction == 0 {
				aAlive++
				aHP += h.HP
				aMaxHP += h.MaxHP
			} else {
				bAlive++
				bHP += h.HP
				bMaxHP += h.MaxHP
			}
		})

		if aAlive == 0 || bAlive == 0 {
			// Battle over
			if aAlive > 0 {
				return 0, float64(aHP) / float64(aMaxHP) * 100, tick
			}
			return 1, float64(bHP) / float64(bMaxHP) * 100, tick
		}
	}

	// Timeout — count who has more HP
	var aHP, bHP int32
	var aMaxHP, bMaxHP int32
	healthPool.Each(func(e ecs.Entity, h *component.HealthComponent) {
		if h.HP <= 0 {
			return
		}
		owner, ok := ownerPool.Get(e)
		if !ok {
			return
		}
		if owner.Faction == 0 {
			aHP += h.HP
			aMaxHP += h.MaxHP
		} else {
			bHP += h.HP
			bMaxHP += h.MaxHP
		}
	})
	if aHP > bHP {
		return 0, float64(aHP) / float64(aMaxHP) * 100, maxTicks
	}
	return 1, float64(bHP) / float64(bMaxHP) * 100, maxTicks
}

func TestClashSniperVsLightInfantry(t *testing.T) {
	winner, hpPct, ticks := runClashSim(t, component.UnitSniper, component.UnitLightInfantry, 3, 3000)
	t.Logf("Sniper vs LI: winner=faction%d, winnerHP=%.1f%%, ticks=%d", winner, hpPct, ticks)

	// Issue #5: Snipers should win with ~50% HP remaining (±30%)
	if winner != 0 {
		t.Fatalf("Snipers (faction 0) should win, but faction %d won", winner)
	}
	if hpPct < 40 {
		t.Errorf("Sniper remaining HP = %.1f%%, want >= 40%% (Snipers too weak)", hpPct)
	}
	if hpPct > 80 {
		t.Errorf("Sniper remaining HP = %.1f%%, want <= 80%% (Snipers too strong, issue #5)", hpPct)
	}
}

func TestClashLightInfantryVsSniper(t *testing.T) {
	// Reverse: LI attacks, Snipers defend
	winner, hpPct, ticks := runClashSim(t, component.UnitLightInfantry, component.UnitSniper, 3, 3000)
	t.Logf("LI vs Sniper: winner=faction%d, winnerHP=%.1f%%, ticks=%d", winner, hpPct, ticks)

	// Snipers should still win regardless of position
	if winner != 1 {
		t.Fatalf("Snipers (faction 1) should win, but faction %d won", winner)
	}
}

func TestClashMirrorMatchup(t *testing.T) {
	// Mirror match should be roughly even — run 5 times
	// Position matters (defender advantage), so we just verify it terminates
	for i := 0; i < 5; i++ {
		winner, _, ticks := runClashSim(t, component.UnitLightInfantry, component.UnitLightInfantry, 3, 3000)
		t.Logf("LI mirror run %d: winner=faction%d, ticks=%d", i+1, winner, ticks)
		if ticks >= 3000 {
			t.Errorf("mirror run %d timed out", i+1)
		}
	}
}

func TestClashHeavyInfantryVsMotorGun(t *testing.T) {
	// HeavyInfantry (Cannon) vs MotorGun (Heavy armor)
	// Cannon does 100% vs Heavy → should be effective
	winner, hpPct, ticks := runClashSim(t, component.UnitHeavyInfantry, component.UnitMotorGun, 3, 3000)
	t.Logf("HeavyInf vs MotorGun: winner=faction%d, winnerHP=%.1f%%, ticks=%d", winner, hpPct, ticks)
	// Just verify it completes without timeout
	if ticks >= 3000 {
		t.Error("battle timed out — units may not be engaging")
	}
}

func TestClashAllUnitTypesTerminate(t *testing.T) {
	// Verify every unit type can participate in a clash that terminates
	types := []component.CombatUnitType{
		component.UnitLightInfantry,
		component.UnitHeavyInfantry,
		component.UnitSniper,
		component.UnitAntiArmorInfantry,
		component.UnitMotorGun,
		component.UnitMotorArtillery,
		component.UnitMotorMissile,
	}
	names := map[component.CombatUnitType]string{
		component.UnitLightInfantry:     "LI",
		component.UnitHeavyInfantry:     "HI",
		component.UnitSniper:            "Sniper",
		component.UnitAntiArmorInfantry: "AAI",
		component.UnitMotorGun:          "MGun",
		component.UnitMotorArtillery:    "MArt",
		component.UnitMotorMissile:      "MMis",
	}
	for _, ut := range types {
		t.Run(fmt.Sprintf("%s_vs_LI", names[ut]), func(t *testing.T) {
			_, _, ticks := runClashSim(t, ut, component.UnitLightInfantry, 2, 5000)
			if ticks >= 5000 {
				t.Errorf("%s vs LI timed out", names[ut])
			}
		})
	}
}
