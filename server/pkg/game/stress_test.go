package game

import (
	"testing"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/network"
)

// TestStressLargeEntityCount spawns a large number of combat units
// (well above the normal 5-20 per side) and verifies the tick loop
// completes without panic or unacceptable slowdown.
//
// Catches:
//   - ECS pool overflows / capacity bugs
//   - Spatial-index performance cliffs (O(n²) degradation)
//   - Combat system death-loops when many units overlap
//   - Snapshot generation failure at extreme entity counts
func TestStressLargeEntityCount(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective.Type = 0

	// Spawn 100 units per side (200 total) — 10x normal capacity.
	// Split into 10 squads of 10 each, two squads close together so
	// combat starts immediately.
	const squadsPerSide = 10
	const unitsPerSquad = 10
	for s := 0; s < squadsPerSide; s++ {
		squadID1 := uint32(s*2 + 1)
		squadID2 := uint32(s*2 + 2)
		// Both sides near map center for immediate combat
		x1 := fixed.FromFloat(20 + float64(s))
		x2 := fixed.FromFloat(28 + float64(s))
		y := fixed.FromFloat(40 + float64(s%3))
		gs.SpawnSquadWithType(1, squadID1, x1, y, unitsPerSquad, component.UnitLightInfantry)
		gs.SpawnSquadWithType(2, squadID2, x2, y, unitsPerSquad, component.UnitLightInfantry)
	}

	// Count starting entities
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	startCount := 0
	boidPool.Each(func(_ ecs.Entity, _ *component.BoidComponent) { startCount++ })
	if startCount < 100 {
		t.Fatalf("expected ≥100 units after spawn, got %d", startCount)
	}
	t.Logf("Spawned %d units total", startCount)

	// Time 200 ticks — should complete in under 10s even at 200 entities
	start := time.Now()
	const tickCount = 200
	for i := 0; i < tickCount; i++ {
		// Should not panic
		gs.Tick()
	}
	elapsed := time.Since(start)

	// Count remaining entities
	remaining := 0
	boidPool.Each(func(_ ecs.Entity, _ *component.BoidComponent) { remaining++ })

	t.Logf("After %d ticks in %s: %d/%d units remain (%d killed)",
		tickCount, elapsed, remaining, startCount, startCount-remaining)

	// Soft perf budget: 200 ticks of 200 entities should be well under 10s.
	// This catches O(n²) cliffs where doubling entity count quadruples time.
	if elapsed > 10*time.Second {
		t.Errorf("perf regression: %d ticks took %s (budget 10s) — possible O(n²) cliff",
			tickCount, elapsed)
	}

	// Sanity: with both sides in melee range, SOMETHING should have died.
	// (If no combat occurred, the combat system is broken.)
	if remaining == startCount {
		t.Errorf("no units died in %d ticks with overlapping sides — combat system not engaging", tickCount)
	}
}

// TestStressParticlePool verifies the particle pool doesn't grow
// unbounded under sustained combat, and that the LRU wraparound
// works correctly when many particles spawn in a short burst.
//
// Note: this is a client-side concern, but the spawn pattern can
// be tested by examining the pool's behavior under load. We
// approximate by counting death events per tick.
func TestStressParticlePool(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective.Type = 0

	// Spawn 40 units in a tight cluster (squad 1 vs squad 2, 20 each)
	gs.SpawnSquadWithType(1, 1, fixed.FromFloat(24), fixed.FromFloat(48), 20, component.UnitLightInfantry)
	gs.SpawnSquadWithType(2, 2, fixed.FromFloat(25), fixed.FromFloat(48), 20, component.UnitLightInfantry)

	// Tick until one side is eliminated
	deathTicks := 0
	maxDeathsPerTick := 0
	for i := 0; i < 2000; i++ {
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		unitsBefore := 0
		boidPool.Each(func(_ ecs.Entity, _ *component.BoidComponent) { unitsBefore++ })

		gs.Tick()

		boidPool2 := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		unitsAfter := 0
		boidPool2.Each(func(_ ecs.Entity, _ *component.BoidComponent) { unitsAfter++ })

		deaths := unitsBefore - unitsAfter
		if deaths > 0 {
			deathTicks++
			if deaths > maxDeathsPerTick {
				maxDeathsPerTick = deaths
			}
		}

		if gs.Lifecycle != nil && gs.Lifecycle.Phase == PhaseEnded {
			t.Logf("Match ended at tick %d: %d ticks had deaths, max deaths in a single tick = %d",
				i, deathTicks, maxDeathsPerTick)
			// The client particle pool is sized 500.  Each death spawns
			// up to 12 smoke particles.  If maxDeathsPerTick × 12 > 500,
			// the pool will LRU-evict — that's expected and tested in
			// particles_test.mjs.  Here we just verify the spawn pattern
			// is reasonable (no infinite-death-spiral).
			if maxDeathsPerTick > 100 {
				t.Errorf("extreme death burst: %d in one tick (pool can hold %d particles, burst would evict)",
					maxDeathsPerTick, 500)
			}
			return
		}
	}
	t.Fatal("stress match did not end in 2000 ticks")
}

// TestStressRapidCommands sends a flood of commands from one client
// in a single tick — far more than a human could ever produce. Verifies
// the server handles the burst without deadlock or corruption.
func TestStressRapidCommands(t *testing.T) {
	gs := NewGameSession()
	gs.Map.Objective.Type = 0

	gs.SpawnTeamWithType(1, 1, fixed.FromFloat(24), fixed.FromFloat(3), 1, component.UnitLightInfantry)
	gs.SpawnTeamWithType(2, 2, fixed.FromFloat(24), fixed.FromFloat(93), 1, component.UnitLightInfantry)
	gs.PlayerGold[1] = 10000 // plenty for many recruits

	// Flood: send 100 move commands + 100 recruit commands in one batch
	// before ticking. The server should queue / dedupe / process them
	// without corruption.
	burstStart := time.Now()
	for i := 0; i < 100; i++ {
		gs.HandleCommand(1, &network.Command{
			Type:    network.CmdMoveSquad,
			SquadID: 1,
			TargetX: int32(fixed.FromFloat(24)),
			TargetY: int32(fixed.FromFloat(50)),
		})
	}
	for i := 0; i < 50; i++ {
		// Recruit different unit types to exercise all branches
		gs.HandleCommand(1, &network.Command{
			Type:        network.CmdRecruit,
			RecruitType: uint8(i % 4),
		})
	}
	burstElapsed := time.Since(burstStart)

	t.Logf("Processed 150 commands in %s", burstElapsed)

	// Tick a few times to let queued commands flush
	for i := 0; i < 5; i++ {
		gs.Tick()
	}

	// Verify no panic, no deadlock, gold pool still consistent
	if gs.PlayerGold[1] < 0 {
		t.Errorf("player 1 gold = %d after recruit flood — should never go negative", gs.PlayerGold[1])
	}
	if gs.PlayerGold[1] > 10000 {
		t.Errorf("player 1 gold = %d — somehow gained gold from recruiting", gs.PlayerGold[1])
	}

	// Verify at least some recruits succeeded (gold was deducted)
	if gs.PlayerGold[1] == 10000 {
		t.Errorf("gold unchanged after 50 recruit commands — recruit path not engaging")
	}

	t.Logf("After flood + 5 ticks: player 1 gold = %d (started 10000)", gs.PlayerGold[1])
}
