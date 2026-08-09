package movement_test

// This file is a THROWAWAY measurement harness (uncommitted). It measures the
// ACTUAL equilibrium cluster radius of combat units around their commander
// under the current committed boid weights, at no-flow equilibrium (pure
// attraction vs separation — the regime that governs the "marching spread"
// symptom). No pass/fail thresholds: it always "passes" and only logs data.
//
// Run:
//   cd server && go test ./pkg/movement/ -run TestCohesionEquilibriumMeasurement -v

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/game"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// measurement holds the distribution stats (in tiles) for one equilibrium run.
type measurement struct {
	n            int
	mean         float64
	median       float64
	p90          float64
	max          float64
	within05     int // count of units within 0.5 tiles of commander
	within10     int // ... 1.0 tiles
	within15     int // ... 1.5 tiles
	within20     int // ... 2.0 tiles
	meanMinSpace float64 // mean of per-unit nearest-neighbour distance (stacking detector)
}

func computeStats(dists []float64) measurement {
	m := measurement{n: len(dists)}
	if len(dists) == 0 {
		return m
	}
	sorted := append([]float64(nil), dists...)
	sort.Float64s(sorted)
	var sum float64
	for _, d := range dists {
		sum += d
		if d <= 0.5 {
			m.within05++
		}
		if d <= 1.0 {
			m.within10++
		}
		if d <= 1.5 {
			m.within15++
		}
		if d <= 2.0 {
			m.within20++
		}
	}
	m.mean = sum / float64(len(dists))
	m.median = percentile(sorted, 0.50)
	m.p90 = percentile(sorted, 0.90)
	m.max = sorted[len(sorted)-1]
	return m
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// setupSession creates a fresh GameSession with no objective, spawns one
// commander + N combat units of the given type at the map center, neutralises
// the flow force, and returns the session plus the spawned squad's ID.
//
// FLOW ZEROING (critical — read this):
// MovementSystem (movement.go:110-150) computes the flow force from each
// unit's PathfindingComponent.TargetX/Y with weight flowW = max(2.5, vel.Speed).
// For a spawned combat unit vel.Speed ≈ 0.2 tiles/tick (fixed-point 820), so
// flowW ≈ 820 — this SATURATES the per-tick force clamp (maxForce = vel.Speed)
// and would yank every unit onto its target tile, completely swamping the
// attraction-vs-separation balance this test exists to measure.
//
// We neutralise flow by removing each combat unit's PathfindingComponent.
// movement.go:112 does `if path, ok := s.pathPool.Get(e); ok` — after Remove
// that returns false, so flowFX/flowFY stay exactly (0,0) for the whole run.
// No other system reads PathfindingComponent (only MovementSystem; DeathSystem
// only Removes it). The commander is stationary (its attraction branch is
// skipped for RoleCommander, and same-squad separation is filtered out for
// commanders), so its own flow target is irrelevant and left intact.
func setupSession(t *testing.T, N int, unitType component.CombatUnitType, seed int64) (*game.GameSession, uint32) {
	t.Helper()
	gs := game.NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement
	// objectiveSys is unexported and cannot be nil-ed from this external test
	// package. That is safe: with Objective.Type == 0, ObjectiveSystem.Tick is
	// a no-op switch (objective.go:62-69 — none of Elimination/Capture/Survival
	// matches Type 0), so it neither moves units nor ends the match.

	if gs.Lifecycle.Phase != game.PhasePlaying {
		gs.Lifecycle.Start()
	}

	// Pin spawn jitter so runs are comparable across squad sizes.
	gs.SetSessionRNG(rand.New(rand.NewSource(seed)))

	spawnX := fixed.FromFloat(float64(game.DefaultMapWidth) / 2)
	spawnY := fixed.FromFloat(float64(game.DefaultMapHeight) / 2)
	const squadID uint32 = 1
	gs.SpawnSquadWithType(1, squadID, spawnX, spawnY, N, unitType)

	// Neutralise flow force on every combat unit (see comment above).
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}
		pathPool.Remove(e)
	})
	return gs, squadID
}

// measure collects positions at the current tick and computes the distance
// distribution of alive combat units from their commander, plus the mean
// nearest-neighbour distance among combat units (the stacking detector).
func measure(t *testing.T, gs *game.GameSession, squadID uint32) measurement {
	t.Helper()
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	var cmdX, cmdY float64
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive || cmd.SquadID != squadID {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			cmdX = fixed.ToFloat(pos.X)
			cmdY = fixed.ToFloat(pos.Y)
		}
	})

	type uv struct{ x, y float64 }
	var units []uv
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			units = append(units, uv{fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y)})
		}
	})

	dists := make([]float64, 0, len(units))
	for _, u := range units {
		dx := u.x - cmdX
		dy := u.y - cmdY
		dists = append(dists, math.Sqrt(dx*dx+dy*dy))
	}
	m := computeStats(dists)

	// Mean minimum inter-unit spacing: for each combat unit, the distance to
	// its nearest fellow combat unit, averaged. When this stops shrinking (or
	// approaches 0) while SeparationW keeps dropping, units are overlapping —
	// that is the stacking floor.
	if len(units) > 1 {
		var sumMin float64
		for i, u := range units {
			best := math.MaxFloat64
			for j, v := range units {
				if i == j {
					continue
				}
				dx := u.x - v.x
				dy := u.y - v.y
				d := math.Sqrt(dx*dx + dy*dy)
				if d < best {
					best = d
				}
			}
			sumMin += best
		}
		m.meanMinSpace = sumMin / float64(len(units))
	}
	return m
}

// runToEquilibrium ticks the session to `total`, logging the mean distance at
// each checkpoint so convergence (plateau) can be confirmed visually.
func runToEquilibrium(t *testing.T, gs *game.GameSession, squadID uint32, checkpoints []int, total int, tag string) {
	t.Helper()
	tickNow := 0
	for _, cp := range checkpoints {
		for tickNow < cp {
			gs.Tick()
			tickNow++
		}
		m := measure(t, gs, squadID)
		t.Logf("  [%s] tick=%-4d mean=%.3f median=%.3f max=%.3f minSpace=%.3f",
			tag, cp, m.mean, m.median, m.max, m.meanMinSpace)
	}
	for tickNow < total {
		gs.Tick()
		tickNow++
	}
}

// TestCohesionEquilibriumMeasurement is a measurement-only test. It logs
// (1) a verification of the spawned boid weights the sim actually uses,
// (2) a baseline table across squad sizes N∈{4,8,12,16} at committed weights.
func TestCohesionEquilibriumMeasurement(t *testing.T) {
	// ---------- 0. Verify spawned weights the sim actually sees ----------
	verifySpawnedWeights(t)

	// ---------- 1. BASELINE: current committed weights (AttractionW=2.0) ----------
	t.Log("=== BASELINE: committed weights (AttractionW=2.0, NeighborRange=1.0).")
	t.Log("    Equilibrium checkpoints (tick 100/300/500) shown per N to confirm plateau.")
	t.Log("")
	t.Log("N   | mean   | median | P90    | max    | <=0.5  | <=1.0  | <=1.5  | <=2.0  | minSpace")
	for _, N := range []int{4, 8, 12, 16} {
		gs, squadID := setupSession(t, N, component.UnitLightInfantry, 1)
		runToEquilibrium(t, gs, squadID, []int{100, 300, 500}, 500,
			"N="+itoa(N))
		m := measure(t, gs, squadID)
		t.Logf("%-3d | %.3f  | %.3f  | %.3f  | %.3f  | %d/%-4d | %d/%-4d | %d/%-4d | %d/%-4d | %.3f",
			N, m.mean, m.median, m.p90, m.max,
			m.within05, m.n, m.within10, m.n, m.within15, m.n, m.within20, m.n, m.meanMinSpace)
	}
}

// verifySpawnedWeights spawns one squad and reports the BoidComponent weights
// the sim actually uses for commander and combat units, so a discrepancy
// (e.g. an earlier weight change that didn't land) is visible at a glance.
func verifySpawnedWeights(t *testing.T) {
	t.Helper()
	gs := game.NewGameSession()
	gs.Map.Objective = tilemap.Objective{Type: 0}
	if gs.Lifecycle.Phase != game.PhasePlaying {
		gs.Lifecycle.Start()
	}
	gs.SetSessionRNG(rand.New(rand.NewSource(7)))
	spawnX := fixed.FromFloat(float64(game.DefaultMapWidth) / 2)
	spawnY := fixed.FromFloat(float64(game.DefaultMapHeight) / 2)
	gs.SpawnSquadWithType(1, 1, spawnX, spawnY, 4, component.UnitLightInfantry)

	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	velPool := gs.World.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])

	expectedCombat := map[string]float64{
		"AttractionW": 2.0, "NeighborRange": 2.0,
	}
	expectedCmd := map[string]float64{
		"AttractionW": 2.0, "NeighborRange": 2.0,
	}

	t.Log("=== SPAWNED WEIGHTS (sim-actual) ===")
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		var label string
		var exp map[string]float64
		if bc.Role == component.RoleCommander {
			label = "Commander"
			exp = expectedCmd
		} else {
			label = "CombatUnit"
			exp = expectedCombat
		}
		actual := map[string]float64{
			"AttractionW":    fixed.ToFloat(bc.AttractionW),
			"NeighborRange": fixed.ToFloat(bc.NeighborRange),
		}
		discrepancy := ""
		for k, want := range exp {
			got := actual[k]
			if math.Abs(got-want) > 5e-3 { // fixed-point resolution ≈ 1/4096 ≈ 2.4e-4
				discrepancy += fmt.Sprintf(" MISMATCH %s: got %.3f want %.3f", k, got, want)
			}
		}
		speed := 0.0
		if v, ok := velPool.Get(e); ok {
			speed = fixed.ToFloat(v.Speed)
		}
		t.Logf("  [%s] FormW=%.2f NbrRange=%.2f speed(tiles/tick)=%.4f%s",
			label, actual["AttractionW"], actual["NeighborRange"], speed, discrepancy)
	})
	t.Log("")
}

// marchStats holds the per-checkpoint march measurement. All distances are
// in tiles. "cmdRel" is radius measured from the COMMANDER's current
// position; "centroidRel" is radius measured from the SQUAD CENTROID of
// combat units. Comparing the two separates "units spreading apart"
// (centroidRel grows) from "commander separating from the pack"
// (cmdRel >> centroidRel).
type marchStats struct {
	tick          int
	cmdTraveled   float64 // distance from spawn to current commander position
	cmdSpeed      float64 // tiles/tick = cmdTraveled / tick (cumulative mean)
	cmdRel        measurement
	centroidRel   measurement
	cmdToCentroid float64 // gap between commander and squad centroid
}

// measureMarch snapshots the current positions and returns the march stats.
// spawnCmdX/Y is the commander's spawn position (for distance-traveled).
func measureMarch(t *testing.T, gs *game.GameSession, squadID uint32, tick int, spawnCmdX, spawnCmdY float64) marchStats {
	t.Helper()
	posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])

	var cmdX, cmdY float64
	cmdFound := false
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if !cmd.IsAlive || cmd.SquadID != squadID {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			cmdX = fixed.ToFloat(pos.X)
			cmdY = fixed.ToFloat(pos.Y)
			cmdFound = true
		}
	})

	var units []uv
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}
		if pos, ok := posPool.Get(e); ok {
			units = append(units, uv{fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y)})
		}
	})

	// Commander-relative distances.
	cmdDists := make([]float64, 0, len(units))
	for _, u := range units {
		dx := u.x - cmdX
		dy := u.y - cmdY
		cmdDists = append(cmdDists, math.Sqrt(dx*dx+dy*dy))
	}
	cmdRel := computeStats(cmdDists)
	// Override meanMinSpace using the existing measure() helper so spacing
	// is computed identically (the production computeStats doesn't fill it).
	cmdRel.meanMinSpace = meanMinSpacing(units)

	// Centroid-relative distances.
	var cx, cy float64
	for _, u := range units {
		cx += u.x
		cy += u.y
	}
	if len(units) > 0 {
		cx /= float64(len(units))
		cy /= float64(len(units))
	}
	centDists := make([]float64, 0, len(units))
	for _, u := range units {
		dx := u.x - cx
		dy := u.y - cy
		centDists = append(centDists, math.Sqrt(dx*dx+dy*dy))
	}
	centroidRel := computeStats(centDists)

	ms := marchStats{
		tick:        tick,
		cmdRel:      cmdRel,
		centroidRel: centroidRel,
	}
	if cmdFound {
		dx := cmdX - spawnCmdX
		dy := cmdY - spawnCmdY
		ms.cmdTraveled = math.Sqrt(dx*dx + dy*dy)
		ms.cmdToCentroid = math.Sqrt((cmdX-cx)*(cmdX-cx) + (cmdY-cy)*(cmdY-cy))
		if tick > 0 {
			ms.cmdSpeed = ms.cmdTraveled / float64(tick)
		}
	}
	return ms
}

// meanMinSpacing is the per-unit nearest-neighbour distance, averaged.
// Mirrors the inline computation in measure() so march numbers use the same
// stacking detector as the idle harness.
func meanMinSpacing(units []uv) float64 {
	if len(units) < 2 {
		return 0
	}
	var sumMin float64
	for i, u := range units {
		best := math.MaxFloat64
		for j, v := range units {
			if i == j {
				continue
			}
			dx := u.x - v.x
			dy := u.y - v.y
			d := math.Sqrt(dx*dx + dy*dy)
			if d < best {
				best = d
			}
		}
		sumMin += best
	}
	return sumMin / float64(len(units))
}

// uv is the position pair used by meanMinSpacing (kept local to this file).
type uv struct{ x, y float64 }

// TestCohesionMarchMeasurement measures cluster radius WHILE THE SQUAD IS
// MARCHING under real flow (PathfindingComponent left intact, target set to
// a distant corner ~25 tiles from map center). For N=8 and N=16 it logs, at
// tick 50/100/200/300:
//   - commander distance-traveled + mean speed (confirms march is happening),
//   - commander-relative radius (mean/median/P90/max + minSpace),
//   - centroid-relative radius (same stats),
//   - commander↔centroid gap.
//
// Comparing commander-relative vs centroid-relative radius answers the
// question: is the "loose formation" during march caused by units spreading
// apart (centroidRel grows) or by the commander separating from the squad
// (cmdRel >> centroidRel)?
//
// Run:
//   cd server && go test ./pkg/movement/ -run TestCohesionMarchMeasurement -v
func TestCohesionMarchMeasurement(t *testing.T) {
	// Distant target: near the far corner of the 30×48 default map. From the
	// map center (15, 24) this is ~sqrt(13²+21²) ≈ 24.7 tiles away — far
	// enough that the squad is still in transit at tick 300.
	targetX := fixed.FromFloat(28.0)
	targetY := fixed.FromFloat(45.0)
	const squadID uint32 = 1
	checkpoints := []int{50, 100, 200, 300}

	for _, N := range []int{8, 16} {
		// --- Build session exactly like setupSession, but KEEP flow active ---
		gs := game.NewGameSession()
		gs.Map.Objective = tilemap.Objective{Type: 0} // no objective — pure movement
		if gs.Lifecycle.Phase != game.PhasePlaying {
			gs.Lifecycle.Start()
		}
		gs.SetSessionRNG(rand.New(rand.NewSource(1)))

		spawnX := fixed.FromFloat(float64(game.DefaultMapWidth) / 2)
		spawnY := fixed.FromFloat(float64(game.DefaultMapHeight) / 2)
		gs.SpawnSquadWithType(1, squadID, spawnX, spawnY, N, component.UnitLightInfantry)
		// NOTE: deliberately NOT removing PathfindingComponent — flow must
		// be active so the march regime is exercised.

		posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
		pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

		var spawnCmdX, spawnCmdY float64
		cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
			if !cmd.IsAlive || cmd.SquadID != squadID {
				return
			}
			if pos, ok := posPool.Get(e); ok {
				spawnCmdX = fixed.ToFloat(pos.X)
				spawnCmdY = fixed.ToFloat(pos.Y)
			}
		})

		// Issue the march order: set PathfindingComponent.TargetX/Y on every
		// squad boid (commander included). This mirrors handleMoveSquad in
		// session.go:1928 exactly — that helper just sets path.TargetX/Y on
		// each squad member and lets MovementSystem's flow-field lookup do
		// the rest. handleMoveSquad is unexported so we replicate it here.
		boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
			if bc.SquadID != squadID {
				return
			}
			if path, ok := pathPool.GetPtr(e); ok {
				path.TargetX = targetX
				path.TargetY = targetY
			}
		})

		// Diagnostic: confirm the spawn tile is passable and the flow field
		// toward (28,45) returns a non-zero direction. (A zero direction
		// here would mean the BFS left the spawn tile's entry at (0,0) and
		// the squad would never march.)
		tileX := int32(spawnCmdX)
		tileY := int32(spawnCmdY)
		if tile := gs.Map.TileAt(tileX, tileY); tile != nil {
			t.Logf("spawn tile (%d,%d) terrain=%d blockLOS=%v", tileX, tileY, tile.TerrainType, tile.BlockLOS)
		}
		lightProfile := component.StandardMovementProfiles()[0]
		ff := gs.Cache.Get(28, 45, lightProfile)
		dir := ff.GetDirection(tileX, tileY)
		t.Logf("flow field target=(28,45) at spawn tile: dir=(%.3f,%.3f)",
			fixed.ToFloat(dir.DX), fixed.ToFloat(dir.DY))

		// ⚠ KNOWN ISSUE — see long note below. Summary:
		// The production path `gs.Tick()` was observed stalling the commander
		// after the first tick (it moves 0.02 tile once, then stops). The
		// stall is in a POST-tick hook of gs.Tick() — updateFog / runAI /
		// objective / gold reconciliation — NOT in MovementSystem itself:
		// calling `gs.World.Tick(tick)` directly (same systems, same order,
		// minus the post-tick hooks) produces a clean march of ~0.02 tile
		// every tick. The harness below therefore drives the world tick
		// directly so the MARCH regime is actually exercised and the
		// formation-deformation measurement is meaningful. The gs.Tick()
		// stall is a separate production bug worth filing — see the report.
		t.Logf("==================== MARCH (N=%d) target=(28,45) spawn=(%.1f,%.1f) ====================", N, spawnCmdX, spawnCmdY)
		t.Logf("    [driven via gs.World.Tick — see KNOWN ISSUE note above]")
		t.Log("tick | cmdTrav | cmdSpd  | cmdRel: mean/med/P90/max/minSp   | centRel: mean/med/P90/max | cmd→cent")
		t.Log("-----+---------+---------+----------------------------------+---------------------------+---------")

		tickNow := uint32(0)
		for _, cp := range checkpoints {
			for int(tickNow) < cp {
				tickNow++
				gs.World.Tick(tickNow)
			}
			ms := measureMarch(t, gs, squadID, cp, spawnCmdX, spawnCmdY)
			t.Logf("%-4d | %-7.2f | %.4f | %-5.2f/%-4.2f/%-4.2f/%-4.2f/%-4.2f | %-5.2f/%-4.2f/%-4.2f/%-4.2f | %.2f",
				cp, ms.cmdTraveled, ms.cmdSpeed,
				ms.cmdRel.mean, ms.cmdRel.median, ms.cmdRel.p90, ms.cmdRel.max, ms.cmdRel.meanMinSpace,
				ms.centroidRel.mean, ms.centroidRel.median, ms.centroidRel.p90, ms.centroidRel.max,
				ms.cmdToCentroid)
		}
		t.Log("")
	}
}

// marchSystemOrder is the priority-sorted system list mirrors of what
// gs.World.Tick drives (see server/pkg/game/session.go:201-217 for the
// AddSystem order and each system's Name()/Priority() for the sort keys).
// Used by the suppression-OFF march variant to tick every system EXCEPT
// CommanderSystem — that way cmd.Suppressing is never set by CommanderSystem
// (priority 50, runs before MovementSystem at 60) and stays at its zero
// default for the whole run, so MovementSystem's drift-centering branch
// (movement.go:146-148 — "if bc.Role != RoleCommander && suppressing[SquadID]")
// never fires. This is the cleanest test-only way to disable ADR-0025
// suppression without touching production code.
var marchSystemOrder = []string{
	"TerrainSystem",      // 30
	"CommanderSystem",    // 50 — SKIPPED in the OFF variant
	"MovementSystem",     // 60
	"BuildSystem",        // 65
	"RecruitmentSystem",  // 70
	"CombatSystem",       // 80
	"StrongholdSystem",   // 82
	"ProjectileSystem",   // 85
	"LevelingSystem",     // 85
	"DeathSystem",        // 90
	"ObjectiveSystem",    // 95
}

// tickSkippingCommanderSystem drives every system in priority order EXCEPT
// CommanderSystem. Returns the number of alive commanders in squadID that
// currently have Suppressing==true (verification that suppression stayed off
// — expected to be 0 for the whole run since CommanderSystem never ran).
func tickSkippingCommanderSystem(t *testing.T, gs *game.GameSession, squadID uint32, tick uint32) int {
	t.Helper()
	cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
	for _, name := range marchSystemOrder {
		if name == "CommanderSystem" {
			continue
		}
		sys := gs.World.SystemByName(name)
		if sys == nil {
			t.Fatalf("SystemByName(%q) returned nil — system list is stale", name)
		}
		sys.Tick(gs.World, tick)
	}
	suppressingCount := 0
	cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
		if cmd.SquadID != squadID || !cmd.IsAlive {
			return
		}
		if cmd.Suppressing {
			suppressingCount++
		}
	})
	return suppressingCount
}

// TestCohesionMarchSuppressionOff is IDENTICAL to TestCohesionMarchMeasurement
// (same spawn at map center, same march target (28,45), same N∈{8,16}, same
// checkpoints 50/100/200/300, same World.Tick driving) EXCEPT the ADR-0025
// drift-centering suppression is disabled: instead of gs.World.Tick (which
// runs CommanderSystem at priority 50 and lets it set cmd.Suppressing=true
// before MovementSystem at 60 reads it), we tick each system individually in
// priority order, SKIPPING CommanderSystem. cmd.Suppressing therefore stays
// at its spawn-time zero default for the entire run, so MovementSystem's
// "zero flow for non-commander units of suppressing squads" branch never
// fires.
//
// Comparison vs TestCohesionMarchMeasurement answers the hypothesis:
//  - If the commander↔centroid gap COLLAPSES here (drops toward the idle-disc
//    baseline ~0.3-0.5) → ADR-0025 suppression is the cause of the march
//    deformation.
//  - If the gap PERSISTS → suppression is NOT the cause; the commander simply
//    outruns the squad for another reason (flow asymmetry / speed clamp).
//
// Verification: after every driven tick we count commanders with
// Suppressing==true and log it; it must be 0 for the whole run. This proves
// the suppression branch in MovementSystem could not have fired.
//
// Run:
//   cd server && go test ./pkg/movement/ -run TestCohesionMarchSuppressionOff -v
func TestCohesionMarchSuppressionOff(t *testing.T) {
	targetX := fixed.FromFloat(28.0)
	targetY := fixed.FromFloat(45.0)
	const squadID uint32 = 1
	checkpoints := []int{50, 100, 200, 300}

	for _, N := range []int{8, 16} {
		gs := game.NewGameSession()
		gs.Map.Objective = tilemap.Objective{Type: 0}
		if gs.Lifecycle.Phase != game.PhasePlaying {
			gs.Lifecycle.Start()
		}
		gs.SetSessionRNG(rand.New(rand.NewSource(1)))

		spawnX := fixed.FromFloat(float64(game.DefaultMapWidth) / 2)
		spawnY := fixed.FromFloat(float64(game.DefaultMapHeight) / 2)
		gs.SpawnSquadWithType(1, squadID, spawnX, spawnY, N, component.UnitLightInfantry)

		posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
		boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		cmdPool := gs.World.Pool(component.CommanderComponent{}).(*ecs.ComponentPool[component.CommanderComponent])
		pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])

		var spawnCmdX, spawnCmdY float64
		cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
			if !cmd.IsAlive || cmd.SquadID != squadID {
				return
			}
			if pos, ok := posPool.Get(e); ok {
				spawnCmdX = fixed.ToFloat(pos.X)
				spawnCmdY = fixed.ToFloat(pos.Y)
			}
		})

		boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
			if bc.SquadID != squadID {
				return
			}
			if path, ok := pathPool.GetPtr(e); ok {
				path.TargetX = targetX
				path.TargetY = targetY
			}
		})

		// Sanity: confirm spawn values for Suppressing are false (zero default).
		suppressingAtSpawn := 0
		cmdPool.Each(func(e ecs.Entity, cmd *component.CommanderComponent) {
			if cmd.SquadID == squadID && cmd.IsAlive && cmd.Suppressing {
				suppressingAtSpawn++
			}
		})

		t.Logf("==================== MARCH SUPPRESSION-OFF (N=%d) target=(28,45) spawn=(%.1f,%.1f) ====================", N, spawnCmdX, spawnCmdY)
		t.Logf("    [driven via per-system Tick skipping CommanderSystem — Suppressing held at spawn=%d]", suppressingAtSpawn)
		t.Log("tick | cmdTrav | cmdSpd  | cmdRel: mean/med/P90/max/minSp   | centRel: mean/med/P90/max | cmd→cent | supOn")
		t.Log("-----+---------+---------+----------------------------------+---------------------------+---------+------")

		tickNow := uint32(0)
		maxSuppressingSeen := 0
		for _, cp := range checkpoints {
			for int(tickNow) < cp {
				tickNow++
				suppressingCount := tickSkippingCommanderSystem(t, gs, squadID, tickNow)
				if suppressingCount > maxSuppressingSeen {
					maxSuppressingSeen = suppressingCount
				}
			}
			ms := measureMarch(t, gs, squadID, cp, spawnCmdX, spawnCmdY)
			t.Logf("%-4d | %-7.2f | %.4f | %-5.2f/%-4.2f/%-4.2f/%-4.2f/%-4.2f | %-5.2f/%-4.2f/%-4.2f/%-4.2f | %.2f   | %d",
				cp, ms.cmdTraveled, ms.cmdSpeed,
				ms.cmdRel.mean, ms.cmdRel.median, ms.cmdRel.p90, ms.cmdRel.max, ms.cmdRel.meanMinSpace,
				ms.centroidRel.mean, ms.centroidRel.median, ms.centroidRel.p90, ms.centroidRel.max,
				ms.cmdToCentroid, maxSuppressingSeen)
		}
		t.Logf("    [verify] max Suppressing==true commanders observed across all %d ticks: %d (must be 0)", tickNow, maxSuppressingSeen)
		t.Log("")
	}
}

// itoa is a tiny strconv stand-in to keep imports lean.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
