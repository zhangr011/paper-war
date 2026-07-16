package game

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// playtestResult captures aggregate stats across N matches of a single matchup.
type playtestResult struct {
	Label       string
	Runs        int
	BlueWins    int
	RedWins     int
	Draws       int
	Stalemates  int
	AvgTicks    float64
	AvgSurvHPpct float64 // winner survivor HP as % of starting HP
}

func (r playtestResult) String() string {
	total := r.BlueWins + r.RedWins + r.Draws + r.Stalemates
	if total == 0 {
		total = r.Runs
	}
	return fmt.Sprintf(
		"%-46s B=%2d R=%2d D=%2d S=%2d | ticks=%.0f survHP=%.0f%%",
		r.Label, r.BlueWins, r.RedWins, r.Draws, r.Stalemates,
		r.AvgTicks, r.AvgSurvHPpct,
	)
}

// runMatchup plays `runs` mirror matches of one unit type and reports aggregate stats.
// Sides are mirrored (same type) so any bias is structural, not counter-based.
func runMirrorMatchup(t *testing.T, typeName string, unitType component.CombatUnitType, runs int) playtestResult {
	t.Helper()
	res := playtestResult{Label: "mirror " + typeName, Runs: runs}

	var tickSum, survSum int
	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0

		mw, mh := gs.MapSize()
		cx1 := fixed.FromFloat(float64(mw)/2 - 2)
		cx2 := fixed.FromFloat(float64(mw)/2 + 2)
		cy := fixed.FromFloat(float64(mh) / 2)
		gs.SpawnSquadWithType(1, 1, cx1, cy, 10, unitType)
		gs.SpawnSquadWithType(2, 2, cx2, cy, 10, unitType)

		hp := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		owner := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

		var startHP [2]int
		hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
			if o, ok := owner.Get(e); ok && c.HP > 0 {
				startHP[o.Faction] += int(c.HP)
			}
		})

		winner := uint8(2) // draw
		decisiveTick := uint32(500)
		for tick := uint32(1); tick <= 500; tick++ {
			gs.Tick()
			var alive [2]int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok {
					alive[o.Faction]++
				}
			})
			if alive[0] == 0 && alive[1] == 0 {
				decisiveTick = tick
				break
			}
			if alive[0] == 0 {
				winner = 1
				decisiveTick = tick
				break
			}
			if alive[1] == 0 {
				winner = 0
				decisiveTick = tick
				break
			}
		}
		if decisiveTick >= 500 && winner == 2 {
			res.Stalemates++
		}

		tickSum += int(decisiveTick)
		switch winner {
		case 0:
			res.BlueWins++
		case 1:
			res.RedWins++
		case 2:
			res.Draws++
		}
		if winner != 2 {
			var survHP int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok && o.Faction == winner {
					survHP += int(c.HP)
				}
			})
			if startHP[winner] > 0 {
				survSum += survHP * 100 / startHP[winner]
			}
		}
	}
	decided := res.BlueWins + res.RedWins
	if decided > 0 {
		res.AvgTicks = float64(tickSum) / float64(decided+res.Draws+res.Stalemates)
		res.AvgSurvHPpct = float64(survSum) / float64(decided)
	}
	return res
}

// runCounterMatchup plays `runs` matches between two different unit types.
// Favored side index: 0=Blue, 1=Red. Expect favored side to win majority.
func runCounterMatchup(t *testing.T, label string, blueType, redType component.CombatUnitType, runs int) playtestResult {
	t.Helper()
	res := playtestResult{Label: label, Runs: runs}

	var tickSum, survSum int
	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0

		mw, mh := gs.MapSize()
		cx1 := fixed.FromFloat(float64(mw)/2 - 2)
		cx2 := fixed.FromFloat(float64(mw)/2 + 2)
		cy := fixed.FromFloat(float64(mh) / 2)
		gs.SpawnSquadWithType(1, 1, cx1, cy, 10, blueType)
		gs.SpawnSquadWithType(2, 2, cx2, cy, 10, redType)

		hp := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		owner := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

		var startHP [2]int
		hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
			if o, ok := owner.Get(e); ok && c.HP > 0 {
				startHP[o.Faction] += int(c.HP)
			}
		})

		winner := uint8(2)
		decisiveTick := uint32(500)
		for tick := uint32(1); tick <= 500; tick++ {
			gs.Tick()
			var alive [2]int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok {
					alive[o.Faction]++
				}
			})
			if alive[0] == 0 && alive[1] == 0 {
				decisiveTick = tick
				break
			}
			if alive[0] == 0 {
				winner = 1
				decisiveTick = tick
				break
			}
			if alive[1] == 0 {
				winner = 0
				decisiveTick = tick
				break
			}
		}
		if decisiveTick >= 500 && winner == 2 {
			res.Stalemates++
		}

		tickSum += int(decisiveTick)
		switch winner {
		case 0:
			res.BlueWins++
		case 1:
			res.RedWins++
		case 2:
			res.Draws++
		}
		if winner != 2 {
			var survHP int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok && o.Faction == winner {
					survHP += int(c.HP)
				}
			})
			if startHP[winner] > 0 {
				survSum += survHP * 100 / startHP[winner]
			}
		}
	}
	decided := res.BlueWins + res.RedWins
	if decided > 0 {
		res.AvgTicks = float64(tickSum) / float64(decided+res.Draws+res.Stalemates)
		res.AvgSurvHPpct = float64(survSum) / float64(decided)
	}
	return res
}

// setSquadPathTarget points every entity in a squad at (tx,ty). Mirrors
// GameSession.handleMoveSquad but is test-local so the realistic harness can
// drive movement deterministically without going through the WS command path.
func setSquadPathTarget(gs *GameSession, squadID uint32, tx, ty int64) {
	boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID {
			return
		}
		if path, ok := pathPool.GetPtr(e); ok {
			path.TargetX = tx
			path.TargetY = ty
		}
	})
}

// runRealisticMatchup plays `runs` matches with movement ENABLED and a large
// spawn gap, mirroring real PvP (spawns at top-center / bottom-center of the
// portrait map). Both squads are ordered to march at the enemy spawn, so they
// close from range and clash near the middle — range, DPS, and HP all matter,
// unlike the MoveDisabled + 4-tile-gap fixture used by runCounterMatchup.
//
// AI movement stays disabled (RecruitDisabled+MoveDisabled via EnableClashMode)
// so the outcome measures combat balance, not AI decision quality; the
// CombatSystem's auto-targeting handles firing once units are in range. The
// tick cap is 1500 (300s at 5Hz) — comfortably above the PvP first-contact
// invariant (≤120s, issue #45) so a healthy matchup resolves well before the
// cap and a stalemate is a real signal, not a timeout artifact.
func runRealisticMatchup(t *testing.T, label string, blueType, redType component.CombatUnitType, runs int) playtestResult {
	t.Helper()
	res := playtestResult{Label: label, Runs: runs}

	const tickCap = uint32(1500)
	var tickSum, survSum int
	for i := 0; i < runs; i++ {
		gs := NewGameSession()
		gs.ResetWithMap(tilemap.LoadClashMap("plains"))
		gs.EnableClashMode()
		gs.Map.Objective.Type = 0

		mw, mh := gs.MapSize()
		cx := fixed.FromFloat(float64(mw) / 2)
		topY := fixed.FromFloat(3)
		botY := fixed.FromFloat(float64(mh) - 4)

		// Randomize which faction holds the top spawn each run. The map is
		// vertically asymmetric for marching combat (cannon-splash geometry
		// differs between up- and down-marches), so a fixed top/blue
		// assignment produces a per-weapon faction sweep in mirrors.
		// Swapping positions per run cancels the directional bias; the
		// BlueWins/RedWins counts still reflect faction outcome.
		blueSpawnY, redSpawnY := topY, botY
		if rand.Intn(2) == 0 {
			blueSpawnY, redSpawnY = botY, topY
		}
		gs.SpawnSquadWithType(1, 1, cx, blueSpawnY, 10, blueType)
		gs.SpawnSquadWithType(2, 2, cx, redSpawnY, 10, redType)

		// Order both squads to march at the enemy spawn so they meet mid-map.
		setSquadPathTarget(gs, 1, cx, redSpawnY)
		setSquadPathTarget(gs, 2, cx, blueSpawnY)

		hp := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		owner := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])

		var startHP [2]int
		hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
			if o, ok := owner.Get(e); ok && c.HP > 0 {
				startHP[o.Faction] += int(c.HP)
			}
		})

		winner := uint8(2) // draw
		decisiveTick := tickCap
		for tick := uint32(1); tick <= tickCap; tick++ {
			gs.Tick()
			var alive [2]int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok {
					alive[o.Faction]++
				}
			})
			if alive[0] == 0 && alive[1] == 0 {
				decisiveTick = tick
				break
			}
			if alive[0] == 0 {
				winner = 1
				decisiveTick = tick
				break
			}
			if alive[1] == 0 {
				winner = 0
				decisiveTick = tick
				break
			}
		}
		if decisiveTick >= tickCap && winner == 2 {
			res.Stalemates++
		}

		tickSum += int(decisiveTick)
		switch winner {
		case 0:
			res.BlueWins++
		case 1:
			res.RedWins++
		case 2:
			res.Draws++
		}
		if winner != 2 {
			var survHP int
			hp.Each(func(e ecs.Entity, c *component.HealthComponent) {
				if c.HP <= 0 {
					return
				}
				if o, ok := owner.Get(e); ok && o.Faction == winner {
					survHP += int(c.HP)
				}
			})
			if startHP[winner] > 0 {
				survSum += survHP * 100 / startHP[winner]
			}
		}
	}
	decided := res.BlueWins + res.RedWins
	if decided > 0 {
		res.AvgTicks = float64(tickSum) / float64(decided+res.Draws+res.Stalemates)
		res.AvgSurvHPpct = float64(survSum) / float64(decided)
	}
	return res
}

// TestPlaytestMatrix runs a balance + behavior playtest across the unit roster.
//
// Two checks:
//  1. MIRROR matchups — within-run faction balance. Relaxed threshold (90%) to
//     keep false-positive rate low at 20 runs (binomial P(>=18|20,0.5) ~ 0.02%).
//  2. SYMMETRY — for each asymmetric matchup, run both faction assignments.
//     The side fielding the stronger type should win the same count regardless
//     of faction. A delta > 6 between normal and swapped means structural bias.
//
// Counter-win expectations are intentionally NOT asserted here. At 2-tile
// spawn distance with MoveDisabled, the unit with higher DPS / HP wins
// regardless of design intent — range advantage is nullified. Tune via stat
// changes, not engine changes.
//
// Winner survHP% is logged for visibility but NOT asserted. Empirically (issue
// #24): pure mirror combat with identical stats + no maneuver undergoes linear
// attrition — both sides lose HP at identical rates, so by the time one side
// is wiped, the other has lost ~90%+ of its HP. This is a mathematical property
// of the symmetric engagement, not a balance bug. HP buffs (1.5x, 3x) and
// cooldown jitter were both tested and produced no measurable change in
// survHP%. Real battles break the symmetry via terrain, positioning, and
// maneuver — none of which exist in this fixture. Do not add a survHP floor;
// it is unreachable here without engine-level asymmetry (e.g. per-unit retreat
// AI, which was considered and explicitly rejected).
func TestPlaytestMatrix(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	type unitInfo struct {
		name string
		t    component.CombatUnitType
	}
	units := []unitInfo{
		{"LightInfantry", component.UnitLightInfantry},
		{"HeavyInfantry", component.UnitHeavyInfantry},
		{"Sniper", component.UnitSniper},
		{"AntiArmorInf", component.UnitAntiArmorInfantry},
		{"MotorGun", component.UnitMotorGun},
		{"MotorArtillery", component.UnitMotorArtillery},
		{"MotorMissile", component.UnitMotorMissile},
	}

	const runs = 20

	t.Log("=== MIRROR MATCHUPS (faction balance sanity check) ===")
	for _, u := range units {
		r := runMirrorMatchup(t, u.name, u.t, runs)
		t.Log(r.String())
		// Relaxed: 18/20 = 90%. P(false positive) ≈ 0.02% per test.
		threshold := 18
		if r.BlueWins > threshold || r.RedWins > threshold {
			t.Errorf("FACTION BIAS in %s mirror: %s", u.name, r.String())
		}
	}

	t.Log("=== OBSERVED COUNTER OUTCOMES (informational, no assertion) ===")
	infoCases := []struct {
		label     string
		blue, red component.CombatUnitType
	}{
		{"Sniper vs LightInfantry", component.UnitSniper, component.UnitLightInfantry},
		{"AntiArmorInf vs MotorGun", component.UnitAntiArmorInfantry, component.UnitMotorGun},
		{"AntiArmorInf vs MotorArtillery", component.UnitAntiArmorInfantry, component.UnitMotorArtillery},
		{"Sniper vs MotorGun", component.UnitSniper, component.UnitMotorGun},
		{"HeavyInf vs LightInfantry", component.UnitHeavyInfantry, component.UnitLightInfantry},
	}
	for _, c := range infoCases {
		r := runCounterMatchup(t, c.label, c.blue, c.red, runs)
		t.Log(r.String())
	}

	// === SYMMETRY CHECK ===
	// For each asymmetric matchup, run it both ways (swap which side fields
	// which unit). Symmetric engine → swap mirrors normal. Bias → Red wins both.
	t.Log("=== SYMMETRY CHECK (swap sides, expect mirrored result) ===")
	symmetryCases := []struct {
		name string
		a, b component.CombatUnitType
	}{
		{"LightInfantry vs MotorGun", component.UnitLightInfantry, component.UnitMotorGun},
		{"Sniper vs LightInfantry", component.UnitSniper, component.UnitLightInfantry},
		{"AntiArmorInf vs MotorGun", component.UnitAntiArmorInfantry, component.UnitMotorGun},
		{"HeavyInfantry vs MotorGun", component.UnitHeavyInfantry, component.UnitMotorGun},
	}
	for _, sc := range symmetryCases {
		normal := runCounterMatchup(t, sc.name+" (A=Blue,B=Red)", sc.a, sc.b, runs)
		swapped := runCounterMatchup(t, sc.name+" (A=Red,B=Blue)", sc.b, sc.a, runs)
		t.Logf("  normal:  %s", normal.String())
		t.Logf("  swapped: %s", swapped.String())
		// Side fielding type A should win same count regardless of faction.
		normalAwins := normal.BlueWins
		swappedAwins := swapped.RedWins
		delta := normalAwins - swappedAwins
		if delta < 0 {
			delta = -delta
		}
		if delta > 6 {
			t.Errorf("ASYMMETRY in %s: type-A wins %d normal vs %d swapped (delta %d > 6) — faction bias",
				sc.name, normalAwins, swappedAwins, delta)
		}
	}
}

// TestPlaytestMatrixRealistic runs the roster through the movement-enabled,
// large-gap harness. This is the trustworthy balance fixture: unlike
// TestPlaytestMatrix (MoveDisabled, 4-tile gap — where short-range high-DPS
// units always win), here squads close from range so design intent (range,
// counters, survivability) actually shapes the outcome.
//
// Phase 0 of the MotorGun balance pass (docs/plans/2026-06-17-playtest-matrix-
// report.md): establish a realistic baseline before tuning. Mirror matchups
// still carry the relaxed faction-bias assertion (an engine invariant);
// counter outcomes are logged informationally — they are the balance data the
// tuning pass will iterate against.
func TestPlaytestMatrixRealistic(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	type unitInfo struct {
		name string
		t    component.CombatUnitType
	}
	units := []unitInfo{
		{"LightInfantry", component.UnitLightInfantry},
		{"HeavyInfantry", component.UnitHeavyInfantry},
		{"Sniper", component.UnitSniper},
		{"AntiArmorInf", component.UnitAntiArmorInfantry},
		{"MotorGun", component.UnitMotorGun},
		{"MotorArtillery", component.UnitMotorArtillery},
		{"MotorMissile", component.UnitMotorMissile},
	}

	const runs = 20

	t.Log("=== REALISTIC MIRROR MATCHUPS (movement on, large spawn gap) ===")
	for _, u := range units {
		r := runRealisticMatchup(t, "realistic mirror "+u.name, u.t, u.t, runs)
		t.Log(r.String())
		// Same relaxed faction-bias invariant as the close-range matrix.
		threshold := 18
		if r.BlueWins > threshold || r.RedWins > threshold {
			t.Errorf("FACTION BIAS in %s realistic mirror: %s", u.name, r.String())
		}
		if r.Stalemates > 0 {
			t.Logf("  WARN: %d/%d stalemates in %s mirror — squads never closed?",
				r.Stalemates, runs, u.name)
		}
	}

	t.Log("=== REALISTIC COUNTER OUTCOMES (informational, no assertion) ===")
	infoCases := []struct {
		label     string
		blue, red component.CombatUnitType
	}{
		{"Sniper vs LightInfantry", component.UnitSniper, component.UnitLightInfantry},
		{"AntiArmorInf vs MotorGun", component.UnitAntiArmorInfantry, component.UnitMotorGun},
		{"AntiArmorInf vs MotorArtillery", component.UnitAntiArmorInfantry, component.UnitMotorArtillery},
		{"Sniper vs MotorGun", component.UnitSniper, component.UnitMotorGun},
		{"HeavyInf vs LightInfantry", component.UnitHeavyInfantry, component.UnitLightInfantry},
		{"LightInfantry vs MotorGun", component.UnitLightInfantry, component.UnitMotorGun},
	}
	for _, c := range infoCases {
		r := runRealisticMatchup(t, "realistic "+c.label, c.blue, c.red, runs)
		t.Log(r.String())
		if r.Stalemates > 0 {
			t.Logf("  WARN: %d/%d stalemates in %s — squads never closed?",
				r.Stalemates, runs, c.label)
		}
	}
}
