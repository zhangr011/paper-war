package combat

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/spatial"
)

// setupStrongholdWorld builds a world with all pools the stronghold mechanics
// touch, plus a CombatSystem and StrongholdSystem wired and initialized.
func setupStrongholdWorld() (*ecs.EntityManager, *ecs.World, *spatial.Hash,
	*ecs.ComponentPool[component.PositionComponent],
	*ecs.ComponentPool[component.HealthComponent],
	*ecs.ComponentPool[component.OwnerComponent],
	*ecs.ComponentPool[component.StrongholdComponent],
	*ecs.ComponentPool[component.BoidComponent],
	*ecs.ComponentPool[component.AttackComponent],
	*ecs.ComponentPool[component.UnitTypeComponent],
	*CombatSystem, *StrongholdSystem,
) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)
	shHash := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	healthPool := ecs.NewComponentPool[component.HealthComponent]()
	ownerPool := ecs.NewComponentPool[component.OwnerComponent]()
	strPool := ecs.NewComponentPool[component.StrongholdComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	attackPool := ecs.NewComponentPool[component.AttackComponent]()
	utPool := ecs.NewComponentPool[component.UnitTypeComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.HealthComponent{}, healthPool)
	w.RegisterPool(component.OwnerComponent{}, ownerPool)
	w.RegisterPool(component.StrongholdComponent{}, strPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.AttackComponent{}, attackPool)
	w.RegisterPool(component.UnitTypeComponent{}, utPool)

	cs := &CombatSystem{Sh: shHash}
	ss := &StrongholdSystem{}
	w.AddSystem(cs)
	w.AddSystem(ss)
	w.Init()
	return em, w, shHash, posPool, healthPool, ownerPool, strPool, boidPool, attackPool, utPool, cs, ss
}

// makeStronghold creates a stronghold entity at (tx,ty) with the given level
// and owning faction.
func makeStronghold(em *ecs.EntityManager, posPool *ecs.ComponentPool[component.PositionComponent],
	hpPool *ecs.ComponentPool[component.HealthComponent], ownerPool *ecs.ComponentPool[component.OwnerComponent],
	strPool *ecs.ComponentPool[component.StrongholdComponent], tx, ty int32, level uint8, faction uint8) ecs.Entity {
	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(float64(tx)), Y: fixed.FromFloat(float64(ty))})
	hp := component.StrongholdHP(level)
	hpPool.Add(e, component.HealthComponent{HP: hp, MaxHP: hp})
	ownerPool.Add(e, component.OwnerComponent{PlayerID: 0, Faction: faction})
	strPool.Add(e, component.StrongholdComponent{Level: level, Capacity: component.StrongholdCapacity(level)})
	return e
}

// TestStrongholdCaptureByFlip: a neutral stronghold reduced to 0 HP flips to
// the attacker's faction and restores to full HP. (#54 1B)
func TestStrongholdCaptureByFlip(t *testing.T) {
	em, w, _, posPool, hpPool, ownerPool, strPool, _, _, _, _, _ := setupStrongholdWorld()
	shE := makeStronghold(em, posPool, hpPool, ownerPool, strPool, 5, 5, 1, component.FactionNeutral)

	// Attacker: a player-faction unit.
	attacker := em.Create()
	ownerPool.Add(attacker, component.OwnerComponent{Faction: component.FactionPlayer})

	// Simulate combat reducing the stronghold to 0 with the attacker credited.
	hp, _ := hpPool.GetPtr(shE)
	hp.HP = 0
	hp.LastAttacker = uint32(attacker)

	w.Tick(1) // runs StrongholdSystem (among others)

	hp2, _ := hpPool.Get(shE)
	owner2, _ := ownerPool.Get(shE)
	if owner2.Faction != component.FactionPlayer {
		t.Errorf("after flip, stronghold faction = %d, want player(0)", owner2.Faction)
	}
	if hp2.HP != component.StrongholdHP(1) {
		t.Errorf("after flip, HP = %d, want full %d", hp2.HP, component.StrongholdHP(1))
	}
}

// TestStrongholdAutoGarrison: a unit on the stronghold tile of its own faction
// joins the garrison up to Capacity. A unit of a different faction does not.
func TestStrongholdAutoGarrison(t *testing.T) {
	em, w, _, posPool, hpPool, ownerPool, strPool, boidPool, _, _, _, _ := setupStrongholdWorld()
	shE := makeStronghold(em, posPool, hpPool, ownerPool, strPool, 7, 7, 1, component.FactionPlayer)

	// Friendly unit on the tile → garrisons.
	friend := em.Create()
	posPool.Add(friend, component.PositionComponent{X: fixed.FromFloat(7.4), Y: fixed.FromFloat(7.4)})
	boidPool.Add(friend, component.BoidComponent{Role: component.RoleMelee})
	ownerPool.Add(friend, component.OwnerComponent{Faction: component.FactionPlayer})

	// Enemy unit on the tile → does NOT garrison.
	foe := em.Create()
	posPool.Add(foe, component.PositionComponent{X: fixed.FromFloat(7.4), Y: fixed.FromFloat(7.4)})
	boidPool.Add(foe, component.BoidComponent{Role: component.RoleMelee})
	ownerPool.Add(foe, component.OwnerComponent{Faction: component.FactionEnemy})

	w.Tick(1)

	sh, _ := strPool.Get(shE)
	if len(sh.Garrison) != 1 {
		t.Fatalf("garrison size = %d, want 1 (friendly only)", len(sh.Garrison))
	}
	if sh.Garrison[0] != friend {
		t.Errorf("garrisoned entity = %v, want friend %v", sh.Garrison[0], friend)
	}
	friendBc, _ := boidPool.Get(friend)
	if friendBc.GarrisonedIn != uint32(shE) {
		t.Errorf("friend GarrisonedIn = %d, want %d", friendBc.GarrisonedIn, shE)
	}
	foeBc, _ := boidPool.Get(foe)
	if foeBc.GarrisonedIn != 0 {
		t.Errorf("foe was garrisoned (faction mismatch not enforced)")
	}
}

// TestStrongholdGarrisonCapacity: Capacity caps how many units garrison.
func TestStrongholdGarrisonCapacity(t *testing.T) {
	em, w, _, posPool, hpPool, ownerPool, strPool, boidPool, _, _, _, _ := setupStrongholdWorld()
	// L1 → Capacity 3.
	shE := makeStronghold(em, posPool, hpPool, ownerPool, strPool, 3, 3, 1, component.FactionPlayer)

	for i := 0; i < 5; i++ {
		u := em.Create()
		posPool.Add(u, component.PositionComponent{X: fixed.FromFloat(3.4), Y: fixed.FromFloat(3.4)})
		boidPool.Add(u, component.BoidComponent{Role: component.RoleMelee})
		ownerPool.Add(u, component.OwnerComponent{Faction: component.FactionPlayer})
	}

	w.Tick(1)
	sh, _ := strPool.Get(shE)
	if len(sh.Garrison) != int(component.StrongholdCapacity(1)) {
		t.Errorf("garrison size = %d, want capacity %d", len(sh.Garrison), component.StrongholdCapacity(1))
	}
}

// TestStrongholdEvictOnFlip: when a stronghold flips, its (enemy) garrison is
// evicted — GarrisonedIn cleared and the units survive with remaining HP.
func TestStrongholdEvictOnFlip(t *testing.T) {
	em, w, _, posPool, hpPool, ownerPool, strPool, boidPool, _, _, _, _ := setupStrongholdWorld()
	// Enemy-owned stronghold with a garrison.
	shE := makeStronghold(em, posPool, hpPool, ownerPool, strPool, 9, 9, 1, component.FactionEnemy)
	gUnit := em.Create()
	posPool.Add(gUnit, component.PositionComponent{X: fixed.FromFloat(9.5), Y: fixed.FromFloat(9.5)})
	hpPool.Add(gUnit, component.HealthComponent{HP: 100, MaxHP: 100})
	boidPool.Add(gUnit, component.BoidComponent{Role: component.RoleMelee})
	ownerPool.Add(gUnit, component.OwnerComponent{Faction: component.FactionEnemy})

	w.Tick(1) // auto-garrison the enemy unit
	sh, _ := strPool.Get(shE)
	if len(sh.Garrison) != 1 {
		t.Fatalf("setup: garrison = %d, want 1", len(sh.Garrison))
	}

	// Player attacker captures the stronghold.
	attacker := em.Create()
	ownerPool.Add(attacker, component.OwnerComponent{Faction: component.FactionPlayer})
	shp, _ := hpPool.GetPtr(shE)
	shp.HP = 0
	shp.LastAttacker = uint32(attacker)

	w.Tick(1)

	// Stronghold flipped to player.
	owner2, _ := ownerPool.Get(shE)
	if owner2.Faction != component.FactionPlayer {
		t.Errorf("stronghold faction = %d, want player", owner2.Faction)
	}
	// Garrison evicted: cleared from stronghold, unit released + alive.
	sh2, _ := strPool.Get(shE)
	if len(sh2.Garrison) != 0 {
		t.Errorf("garrison = %d after flip, want 0 (evicted)", len(sh2.Garrison))
	}
	gBc, _ := boidPool.Get(gUnit)
	if gBc.GarrisonedIn != 0 {
		t.Errorf("evicted unit still marked garrisoned")
	}
	gHp, _ := hpPool.Get(gUnit)
	if gHp.HP <= 0 {
		t.Errorf("evicted unit dead, should survive with remaining HP")
	}
}

// TestStrongholdDamageSplit: a siege hit on a garrisoned stronghold splits
// damage — the garrison absorbs its level-scaled share (÷N), the stronghold
// takes the remainder. (#54 1B, ADR-0023.)
func TestStrongholdDamageSplit(t *testing.T) {
	em, w, shHash, posPool, hpPool, ownerPool, strPool, boidPool, attackPool, utPool, _, _ := setupStrongholdWorld()

	shE := makeStronghold(em, posPool, hpPool, ownerPool, strPool, 4, 4, 1, component.FactionEnemy)
	// Two garrisoned enemy units.
	var gUnits []ecs.Entity
	for i := 0; i < 2; i++ {
		u := em.Create()
		posPool.Add(u, component.PositionComponent{X: fixed.FromFloat(4.4), Y: fixed.FromFloat(4.4)})
		hpPool.Add(u, component.HealthComponent{HP: 1000, MaxHP: 1000})
		boidPool.Add(u, component.BoidComponent{Role: component.RoleMelee})
		ownerPool.Add(u, component.OwnerComponent{Faction: component.FactionEnemy})
		gUnits = append(gUnits, u)
	}
	w.Tick(1) // StrongholdSystem auto-garrisons
	shp, _ := hpPool.Get(shE)
	hpBefore := shp.HP

	// Player Cannon attacker in range. Cannon vs Building = 25%.
	attacker := em.Create()
	posPool.Add(attacker, component.PositionComponent{X: fixed.FromFloat(4.4), Y: fixed.FromFloat(4.4)})
	attackPool.Add(attacker, component.AttackComponent{Range: fixed.FromFloat(5.0), Damage: 40, Cooldown: 1})
	utPool.Add(attacker, component.UnitTypeComponent{Weapon: component.WeaponCannon, Armor: component.ArmorLight})
	ownerPool.Add(attacker, component.OwnerComponent{Faction: component.FactionPlayer})

	rebuildSpatialHash(shHash, posPool)
	w.Tick(1) // combat resolves

	// dmg = 40 * 25 / 100 = 10 (no terrain cover on this Plain tile).
	// L1 garrison share = 50% → garrisonDmg = 5, perUnit = 5/2 = 2 each.
	// Stronghold absorbs 10 - 5 = 5.
	shp2, _ := hpPool.Get(shE)
	strongLoss := hpBefore - shp2.HP
	if strongLoss != 5 {
		t.Errorf("stronghold lost %d HP, want 5 (remainder after 50%% split)", strongLoss)
	}
	for _, u := range gUnits {
		uhp, _ := hpPool.Get(u)
		if uhp.HP != 1000-2 {
			t.Errorf("garrison unit HP = %d, want 998 (5 split ÷2)", uhp.HP)
		}
	}
}

// TestStrongholdGarrisonShareTable locks the level→share curve (L1=50 … L5=30).
func TestStrongholdGarrisonShareTable(t *testing.T) {
	want := map[uint8]int32{1: 50, 2: 45, 3: 40, 4: 35, 5: 30}
	for lvl, w := range want {
		if got := component.StrongholdGarrisonShare(lvl); got != w {
			t.Errorf("StrongholdGarrisonShare(%d) = %d, want %d", lvl, got, w)
		}
	}
}
