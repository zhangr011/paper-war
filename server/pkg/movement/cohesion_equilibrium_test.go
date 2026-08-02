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

	// Pin spawn jitter so runs are comparable across the SeparationW sweep.
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

// setCombatSeparationW overrides SeparationW on every non-commander unit in
// the squad (commander keeps its spawned 1.5). Used by the sweep.
func setCombatSeparationW(t *testing.T, gs *game.GameSession, squadID uint32, w float64) {
	t.Helper()
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	val := fixed.FromFloat(w)
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}
		bc.SeparationW = val
	})
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
// (2) a baseline table across squad sizes N∈{4,8,12,16} at committed weights,
// (3) a SeparationW sweep for N=8 and N=16 to find the stacking floor.
func TestCohesionEquilibriumMeasurement(t *testing.T) {
	// ---------- 0. Verify spawned weights the sim actually sees ----------
	verifySpawnedWeights(t)

	// ---------- 1. BASELINE: current committed weights (SeparationW=1.5) ----------
	t.Log("=== BASELINE: committed weights (SeparationW=1.5, FormationW=2.0,")
	t.Log("    NeighborRange=1.0, CohesionW=0.8, AlignmentW=1.0). Zero flow (see setup).")
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

	// ---------- 2. SEPARATION SWEEP — find the stacking floor ----------
	sweepVals := []float64{1.5, 1.0, 0.75, 0.5, 0.3, 0.15, 0.0}
	const plateauRelThreshold = 0.05 // <5% relative shrinkage vs previous step ⇒ plateau
	for _, N := range []int{8, 16} {
		t.Log("")
		t.Logf("=== SEPARATION SWEEP (N=%d) — all other weights held at committed values ===", N)
		t.Log("SepW  | mean   | median | P90    | max    | meanMinSpace")
		var prevMean float64 = -1
		floorW := -1.0
		floorMean := 0.0
		for i, w := range sweepVals {
			gs, squadID := setupSession(t, N, component.UnitLightInfantry, 1)
			if i == 0 {
				// Baseline (1.5) already has the right value; for the rest, override.
			} else {
				setCombatSeparationW(t, gs, squadID, w)
			}
			runToEquilibrium(t, gs, squadID, nil, 500, "sweep")
			m := measure(t, gs, squadID)
			t.Logf("%.2f | %.3f  | %.3f  | %.3f  | %.3f  | %.3f",
				w, m.mean, m.median, m.p90, m.max, m.meanMinSpace)
			if floorW < 0 && prevMean > 0 {
				relDrop := (prevMean - m.mean) / prevMean
				if relDrop < plateauRelThreshold {
					floorW = w
					floorMean = m.mean
				}
			}
			prevMean = m.mean
		}
		if floorW >= 0 {
			t.Logf("Floor (N=%d): radius plateaued at SeparationW=%.2f (mean≈%.3f tiles,",
				N, floorW, floorMean)
			t.Logf("              using <%.0f%% relative shrinkage vs prior step as the plateau rule).",
				plateauRelThreshold*100)
		} else {
			t.Logf("Floor (N=%d): radius kept shrinking down to SeparationW=0.0 — no plateau", N)
			t.Logf("              within the tested range (radius still dropping at each step).")
		}
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
		"SeparationW": 1.5, "FormationW": 2.0, "NeighborRange": 2.0,
		"CohesionW": 0.8, "AlignmentW": 1.0,
	}
	expectedCmd := map[string]float64{
		"SeparationW": 1.5, "FormationW": 2.0, "NeighborRange": 2.0,
		"CohesionW": 0.8, "AlignmentW": 1.0,
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
			"SeparationW":   fixed.ToFloat(bc.SeparationW),
			"CohesionW":     fixed.ToFloat(bc.CohesionW),
			"AlignmentW":    fixed.ToFloat(bc.AlignmentW),
			"FormationW":    fixed.ToFloat(bc.FormationW),
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
		t.Logf("  [%s] SepW=%.2f FormW=%.2f CohW=%.2f AlignW=%.2f NbrRange=%.2f speed(tiles/tick)=%.4f%s",
			label, actual["SeparationW"], actual["FormationW"], actual["CohesionW"],
			actual["AlignmentW"], actual["NeighborRange"], speed, discrepancy)
	})
	t.Log("")
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
