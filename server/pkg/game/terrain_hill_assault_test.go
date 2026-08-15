package game

import (
	"math/rand"
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// TestLiveHillAssault — the capstone tactical scenario: one squad HOLDS the
// high ground, the other tries to move onto it.
//
// Defender (player 2) sits on a peak platform (Elevation 2); attacker (player
// 1) starts on low ground ~6 tiles out and is ordered onto the hilltop. The
// defender's LightInfantry base range 3 becomes effective 4 from high ground
// (flat +1, ADR-0029), so it fires on the approaching attacker; the attacker's
// range 3 (shooting uphill, no bonus) can't reach the defender at distance 6
// until it closes. So during the approach the hill holder punishes the assault
// while taking no return fire — the high-ground advantage, exercised through a
// real gs.Tick() loop with movement.
//
// Both factions are present so Elimination doesn't end the match at tick 1
// (the trap that mimics a movement stall in single-faction tests).
func TestLiveHillAssault(t *testing.T) {
	m := tilemap.NewGameMap(24, 24)
	// 5x5 peak platform the defender holds.
	for y := int32(10); y <= 14; y++ {
		for x := int32(17); x <= 21; x++ {
			tl := m.TileAt(x, y)
			tl.TerrainType = component.TerrainHill
			tl.Elevation = 2
		}
	}
	// Ramp up the west face of the platform at row 12 (the assault route).
	m.SetTerrain(16, 12, component.TerrainRamp)
	m.TileAt(16, 12).Elevation = 2

	gs := NewGameSession()
	gs.ResetWithMap(m)
	gs.EnableClashMode()
	gs.Lifecycle.Phase = PhasePlaying
	if gs.AISys != nil { // pin the AI defender so it holds the hill
		gs.AISys.MoveDisabled = true
		gs.AISys.RecruitDisabled = true
	}
	// Pin spawn jitter: the session RNG is time-seeded in tests, and the
	// 1-tile high-ground window (flat +1 on a 3-range unit) is narrow enough
	// that ±0.3-tile jitter can flip either assertion. Deterministic setup →
	// deterministic scenario (the same pin TestClashModeBalance uses).
	gs.SetSessionRNG(rand.New(rand.NewSource(7)))

	// Defender on the peak; attacker 6 tiles west on low ground. Beyond the
	// defender's effective 4 plus formation spread, so the assault closes
	// under fire it cannot return until near the top.
	gs.SpawnSquadWithType(2, 2, fixed.FromFloat(19.0), fixed.FromFloat(12.0), 5, component.UnitLightInfantry)
	gs.SpawnSquadWithType(1, 1, fixed.FromFloat(13.0), fixed.FromFloat(12.0), 5, component.UnitLightInfantry)

	// Order the attacker onto the hilltop (combat pursue will reinforce this).
	pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	pathPool.Each(func(e ecs.Entity, p *component.PathfindingComponent) {
		if o, ok := ownerPool.Get(e); ok && o.Faction == component.FactionPlayer {
			p.TargetX = fixed.FromFloat(19.0)
			p.TargetY = fixed.FromFloat(12.0)
		}
	})

	const squadHP = int32(600 + 5*100) // commander (6×100) + 5 LI (100 each)
	startRawX := int64(fixed.FromFloat(13.0))

	sumHP := func(faction uint8) int32 {
		var sum int32
		hp := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
		own := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
		hp.Each(func(e ecs.Entity, h *component.HealthComponent) {
			if o, ok := own.Get(e); ok && o.Faction == faction && h.HP > 0 {
				sum += h.HP
			}
		})
		return sum
	}
	cmdRawX := func() (int64, bool) {
		var x int64
		found := false
		boid := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
		pos := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
		own := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
		boid.Each(func(e ecs.Entity, b *component.BoidComponent) {
			if b.Role != component.RoleCommander {
				return
			}
			if o, ok := own.Get(e); ok && o.Faction == component.FactionPlayer {
				if p, ok := pos.Get(e); ok {
					x = p.X
					found = true
				}
			}
		})
		return x, found
	}

	// 1. The attacker advanced toward the hill — sampled early (tick 10),
	//    before focus fire kills the commander, using raw X since movement is
	//    sub-tile over short windows (~0.01 tile/tick).
	for i := 0; i < 10; i++ {
		gs.Tick()
	}
	if x, ok := cmdRawX(); !ok {
		t.Errorf("attacker commander already dead by tick 10")
	} else if x <= startRawX {
		t.Errorf("attacker commander rawX=%d, want >%d (did not advance toward the hill)", x, startRawX)
	}

	for i := 0; i < 110; i++ { // run the engagement out to 120 total
		gs.Tick()
	}

	attHP := sumHP(component.FactionPlayer)
	defHP := sumHP(component.FactionEnemy)

	// 2. The defender held the hill and took no return fire (attacker, range 3
	//    shooting uphill, could not reach it at the approach distance).
	if defHP < squadHP {
		t.Errorf("defender HP=%d, want %d (should be undamaged on high ground while the assault is out of range)", defHP, squadHP)
	}
	// 3. The high-ground defender punished the assault.
	if attHP >= squadHP {
		t.Errorf("attacker HP=%d, want <%d (high-ground defender should damage the approaching assault)", attHP, squadHP)
	}
}
