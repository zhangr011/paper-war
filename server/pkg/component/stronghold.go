package component

import "github.com/user/paper-war/server/pkg/ecs"

// StrongholdComponent marks an entity as a Stronghold — a capturable,
// garrisonable Building (ADR-0023 / issue #54). A stronghold starts Neutral
// (OwnerComponent.Faction == FactionNeutral) and is claimed by being damaged
// to 0 HP, which flips it to the attacker's Faction and restores HP.
//
// Phase 1A (#54): entity model + spawn + retire terrain. Capture-by-flip and
// garrison gameplay (enter/exit, fire-out, the level-scaled damage split,
// evict-on-flip) are Phase 1B — until then a Stronghold entity is a non-targetable
// position marker carrying HP/level/owner for the systems that will use them.
type StrongholdComponent struct {
	// Level 1-5. Drives HP, Capacity, and (Phase 1B) the damage-split shelter
	// curve and garrison capacity.
	Level uint8
	// Capacity is the max CombatUnits the stronghold's Garrison can hold.
	// Scales with Level. Used in Phase 1B (garrison).
	Capacity uint8
	// Garrison holds the entity IDs of CombatUnits inside the stronghold.
	// Phase 1B populates this; empty until garrison commands exist.
	Garrison []ecs.Entity
}

// StrongholdHP returns the max HP for a stronghold of the given level (1-5).
// Phase 1A tuning; capture restores to this on flip.
func StrongholdHP(level uint8) int32 {
	if level < 1 {
		level = 1
	}
	if level > 5 {
		level = 5
	}
	return int32(200 + 150*int(level)) // L1=350 .. L5=950
}

// StrongholdCapacity returns the garrison capacity for a stronghold level.
func StrongholdCapacity(level uint8) uint8 {
	if level < 1 {
		level = 1
	}
	if level > 5 {
		level = 5
	}
	return uint8(2 + int(level)) // L1=3 .. L5=7
}

// StrongholdGarrisonShare returns the % of incoming damage the garrison
// absorbs (the rest is taken by the stronghold itself). L1 = 50% → L5 = 30%
// — higher-level strongholds shelter their garrison better. ADR-0023 (#54).
func StrongholdGarrisonShare(level uint8) int32 {
	if level < 1 {
		level = 1
	}
	if level > 5 {
		level = 5
	}
	// L1=50, L2=45, L3=40, L4=35, L5=30
	return int32(50 - int(level-1)*5)
}
