package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

// TestStrongholdsSpawnedAsEntities verifies the #54 Phase 1A migration: a
// generated map records stronghold specs, and Reset spawns one Stronghold
// entity per spec — neutral, with HP and capacity scaled by level. This is
// the structural replacement for the retired TerrainStronghold1-5 terrain.
func TestStrongholdsSpawnedAsEntities(t *testing.T) {
	gs := NewGameSession()
	gs.Reset() // generates the map + spawns stronghold entities

	if len(gs.Map.Strongholds) == 0 {
		t.Fatal("generated map recorded no stronghold specs")
	}

	shPool, ok := gs.World.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])
	if !ok {
		t.Fatal("StrongholdComponent pool not registered")
	}
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	hpPool := gs.World.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])

	count := 0
	shPool.Each(func(e ecs.Entity, sc *component.StrongholdComponent) {
		count++
		hp, _ := hpPool.Get(e)
		if hp.HP != component.StrongholdHP(sc.Level) {
			t.Errorf("stronghold level %d HP = %d, want %d", sc.Level, hp.HP, component.StrongholdHP(sc.Level))
		}
		if sc.Capacity != component.StrongholdCapacity(sc.Level) {
			t.Errorf("stronghold level %d capacity = %d, want %d", sc.Level, sc.Capacity, component.StrongholdCapacity(sc.Level))
		}
		owner, _ := ownerPool.Get(e)
		if owner.Faction != component.FactionNeutral {
			t.Errorf("stronghold faction = %d, want neutral (0xFF) at match start", owner.Faction)
		}
	})
	if count != len(gs.Map.Strongholds) {
		t.Errorf("spawned %d stronghold entities, map recorded %d specs", count, len(gs.Map.Strongholds))
	}
}

// TestStrongholdStateIfChanged covers the client-broadcast helper: it reports
// the live state, dedupes identical state, and re-reports after a change (#54 1B).
func TestStrongholdStateIfChanged(t *testing.T) {
	gs := NewGameSession()
	gs.Reset()

	states, changed := gs.StrongholdStateIfChanged()
	if !changed || len(states) == 0 {
		t.Fatal("first call should report changed with the spawned strongholds")
	}
	for _, s := range states {
		if s.Faction != component.FactionNeutral {
			t.Errorf("initial state faction = %d, want neutral", s.Faction)
		}
	}

	// Identical state → not changed (deduped).
	if _, again := gs.StrongholdStateIfChanged(); again {
		t.Error("unchanged state should be deduped")
	}

	// Flip one stronghold to player faction → should report changed.
	shPool := gs.World.Pool(component.StrongholdComponent{}).(*ecs.ComponentPool[component.StrongholdComponent])
	ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
	var flipped bool
	shPool.Each(func(e ecs.Entity, _ *component.StrongholdComponent) {
		if !flipped {
			if op, ok := ownerPool.GetPtr(e); ok {
				op.Faction = component.FactionPlayer
			}
			flipped = true
		}
	})
	states2, changed2 := gs.StrongholdStateIfChanged()
	if !changed2 {
		t.Fatal("faction change should report changed")
	}
	var sawPlayer bool
	for _, s := range states2 {
		if s.Faction == component.FactionPlayer {
			sawPlayer = true
		}
	}
	if !sawPlayer {
		t.Error("flipped stronghold not reported as player-owned")
	}
}
