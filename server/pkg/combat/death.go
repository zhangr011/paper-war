package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
)

type DeathSystem struct {
	em *ecs.EntityManager

	healthPool        *ecs.ComponentPool[component.HealthComponent]
	posPool           *ecs.ComponentPool[component.PositionComponent]
	velPool           *ecs.ComponentPool[component.VelocityComponent]
	boidPool          *ecs.ComponentPool[component.BoidComponent]
	attackPool        *ecs.ComponentPool[component.AttackComponent]
	cmdPool           *ecs.ComponentPool[component.CommanderComponent]
	movePool          *ecs.ComponentPool[component.MovementComponent]
	pathPool          *ecs.ComponentPool[component.PathfindingComponent]
	formationPool     *ecs.ComponentPool[component.FormationComponent]
	formationRolePool *ecs.ComponentPool[component.FormationRoleComponent]
	ownerPool         *ecs.ComponentPool[component.OwnerComponent]
	killPointsPool    *ecs.ComponentPool[component.KillPointsComponent]
	unitTypePool      *ecs.ComponentPool[component.UnitTypeComponent]

	// GoldBounties collects {playerID: bounty} for each tick.
	// Cleared at start of each Tick. Session reads this after Tick().
	GoldBounties map[uint32]int32

	// Promotions collects {squadID: newCommanderEntity} for each tick.
	// Cleared at start of each Tick. Session reads this to update AI state.
	Promotions map[uint32]ecs.Entity

	// Deaths collects entityIDs of units that died this tick.
	// Used by GenerateSnapshot to send death events to clients.
	//
	// Issue #28 — superseded by DeathRecords (which carries position+tick
	// alongside the entityID).  Kept as a derived accessor below for any
	// caller that only needs IDs (e.g. snapshot generator cleanup).
	Deaths []uint32

	// DeathRecords collects full per-death context the snapshot encoder
	// needs to emit an enriched EventDeath payload (entityID + X + Y +
	// tick).  Position is captured BEFORE the entity's components are
	// torn down, so the client can play the die animation at the exact
	// location the unit was when it died — not at the extrapolated
	// interpolated render position, which may have drifted.
	DeathRecords []DeathRecord

	// KillEvents collects faction-attributed kill data each tick.
	// Cleared at start of each Tick. Session reads this to feed MatchStats.
	KillEvents []KillEvent
}

// KillEvent captures the faction attribution for a single unit death.
// Emitted by DeathSystem each tick.
type KillEvent struct {
	KillerFaction uint8  // faction of the killer, or 0xFF if no killer
	DeadFaction   uint8  // faction of the dead unit
	IsCommander   bool   // true if the dead unit was a squad commander
	Bounty        int32  // gold bounty awarded to the killer (0 if no killer)
}

// DeathRecord captures the simulation-side context of a single unit death
// so the snapshot encoder can emit an enriched EventDeath payload.
//
//   - EntityID: which unit died (so the client can look up its render state)
//   - X, Y:     fixed-point (FractionBits=12) position at moment of death.
//               Captured BEFORE the PositionComponent is removed, so the
//               client can play the die animation at the death location
//               rather than at the extrapolated interpolation position.
//   - Tick:     simulation tick on which the death occurred.  Lets the
//               client reconstruct exactly when the unit died even if the
//               death event is processed a few snapshots late.
type DeathRecord struct {
	EntityID uint32
	X, Y     int64
	Tick     uint32
}

func (s *DeathSystem) Name() string  { return "DeathSystem" }
func (s *DeathSystem) Priority() int { return 90 }

func (s *DeathSystem) Init(w *ecs.World) {
	s.em = w.Entities()
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	if p := w.Pool(component.CommanderComponent{}); p != nil {
		s.cmdPool = p.(*ecs.ComponentPool[component.CommanderComponent])
	}
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
	if p := w.Pool(component.FormationComponent{}); p != nil {
		s.formationPool = p.(*ecs.ComponentPool[component.FormationComponent])
	}
	if p := w.Pool(component.FormationRoleComponent{}); p != nil {
		s.formationRolePool = p.(*ecs.ComponentPool[component.FormationRoleComponent])
	}
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	if p := w.Pool(component.KillPointsComponent{}); p != nil {
		s.killPointsPool = p.(*ecs.ComponentPool[component.KillPointsComponent])
	}
	if p := w.Pool(component.UnitTypeComponent{}); p != nil {
		s.unitTypePool = p.(*ecs.ComponentPool[component.UnitTypeComponent])
	}
}

func (s *DeathSystem) Tick(w *ecs.World, tick uint32) {
	s.GoldBounties = make(map[uint32]int32)
	s.Promotions = make(map[uint32]ecs.Entity)
	s.Deaths = nil
	s.DeathRecords = nil
	s.KillEvents = nil

	var dead []ecs.Entity
	s.healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			dead = append(dead, e)
		}
	})

	// Pre-pass: snapshot attacker factions for all dead entities BEFORE any
	// components are removed. This ensures mutual kills (attacker and victim
	// both die in the same tick) are attributed correctly — without this,
	// processing one death removes the attacker's components, making the
	// other death's attacker lookup fail and creating unattributed kills.
	attackerFaction := make(map[uint32]uint8)
	attackerPlayerID := make(map[uint32]uint32)
	if s.ownerPool != nil {
		for _, e := range dead {
			hp, _ := s.healthPool.Get(e)
			if hp.LastAttacker != 0 {
				killerEntity := ecs.Entity(hp.LastAttacker)
				if killerOwner, ok := s.ownerPool.Get(killerEntity); ok {
					attackerFaction[uint32(e)] = killerOwner.Faction
					attackerPlayerID[uint32(e)] = killerOwner.PlayerID
				}
			}
		}
	}

	for _, e := range dead {
		hp, _ := s.healthPool.Get(e)

		// --- Build KillEvent for stats attribution ---
		ke := KillEvent{KillerFaction: 0xFF}
		if s.ownerPool != nil {
			if deadOwner, ok := s.ownerPool.Get(e); ok {
				ke.DeadFaction = deadOwner.Faction
			}
		}
		// Commander kill: only count original commanders, not promoted ones
		isCommanderDeath := false
		if s.cmdPool != nil {
			if cmd, ok := s.cmdPool.Get(e); ok && cmd.IsAlive && !cmd.Promoted {
				isCommanderDeath = true
			}
		}
		ke.IsCommander = isCommanderDeath

		// Attribute kill to attacker's faction from pre-pass snapshot.
		// This happens regardless of whether the attacker is still alive —
		// the killing blow was already dealt.
		if af, ok := attackerFaction[uint32(e)]; ok {
			ke.KillerFaction = af
		}

		// Bounty value from dead unit's type, but only if there's an
		// attributed killer (no bounty for unattributed deaths).
		if ke.KillerFaction != 0xFF && s.unitTypePool != nil {
			if deadUT, ok := s.unitTypePool.Get(e); ok {
				ke.Bounty = component.CombatUnitTypeTable[deadUT.Type].KillBounty
			}
		}

		// Award kill points and spendable gold to the PLAYER only if the
		// attacker is still alive (dead players can't collect rewards).
		if hp.LastAttacker != 0 && s.killPointsPool != nil {
			killerEntity := ecs.Entity(hp.LastAttacker)
			if killerHP, ok := s.healthPool.Get(killerEntity); ok && killerHP.HP > 0 {
				if kp, ok := s.killPointsPool.GetPtr(killerEntity); ok {
					kp.Points += s.killPointValue(e)
				}
				if ke.Bounty > 0 {
					if pid, ok := attackerPlayerID[uint32(e)]; ok {
						s.GoldBounties[pid] += ke.Bounty
					}
				}
			}
		}
		s.KillEvents = append(s.KillEvents, ke)

		// Handle commander death: promote highest-level unit in squad
		if s.cmdPool != nil {
			if cmd, ok := s.cmdPool.Get(e); ok && cmd.IsAlive {
				cmd.IsAlive = false
				s.handleCommanderDeath(cmd.SquadID)
			}
		}

		// Clear attack targets referencing this entity
		targetID := uint32(e)
		s.Deaths = append(s.Deaths, targetID)

		// Issue #28 — capture position BEFORE posPool.Remove(e) tears it
		// down.  The client uses this to anchor the die animation at the
		// exact tile the unit occupied when it died, rather than at the
		// extrapolated interpolation position (which may have drifted
		// past the actual death location between snapshots).
		var deathX, deathY int64
		if pos, ok := s.posPool.Get(e); ok {
			deathX = pos.X
			deathY = pos.Y
		}
		s.DeathRecords = append(s.DeathRecords, DeathRecord{
			EntityID: targetID,
			X:        deathX,
			Y:        deathY,
			Tick:     tick,
		})

		s.attackPool.Each(func(other ecs.Entity, ac *component.AttackComponent) {
			if ac.TargetID == targetID {
				ac.TargetID = 0
			}
		})

		// Remove all components
		s.healthPool.Remove(e)
		s.posPool.Remove(e)
		s.velPool.Remove(e)
		s.boidPool.Remove(e)
		s.attackPool.Remove(e)
		s.movePool.Remove(e)
		s.pathPool.Remove(e)
		if s.cmdPool != nil {
			s.cmdPool.Remove(e)
		}
		if s.formationPool != nil {
			s.formationPool.Remove(e)
		}
		if s.formationRolePool != nil {
			s.formationRolePool.Remove(e)
		}
		if s.ownerPool != nil {
			s.ownerPool.Remove(e)
		}
		if s.killPointsPool != nil {
			s.killPointsPool.Remove(e)
		}
		if s.unitTypePool != nil {
			s.unitTypePool.Remove(e)
		}

		s.em.Destroy(e)
	}
}

// killPointValue returns the kill point value of a dying entity.
// Commanders are worth more.
func (s *DeathSystem) killPointValue(dead ecs.Entity) int32 {
	if s.cmdPool != nil {
		if _, ok := s.cmdPool.Get(dead); ok {
			return 5 // Commander kill bonus
		}
	}
	return 1
}

// handleCommanderDeath promotes the highest-level CombatUnit in the squad
// to Commander. The promoted unit retains its CombatUnitType (types never convert).
func (s *DeathSystem) handleCommanderDeath(squadID uint32) {
	var bestEntity ecs.Entity
	var bestLevel uint8

	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}

		level := uint8(0)
		if s.unitTypePool != nil {
			if ut, ok := s.unitTypePool.Get(e); ok {
				level = ut.Level
			}
		}

		if level > bestLevel || (level == bestLevel && bestEntity == 0) {
			bestEntity = e
			bestLevel = level
		}
	})

	if bestEntity == 0 {
		return // no one to promote
	}

	// Promote: change BoidRole to Commander
	bc, _ := s.boidPool.GetPtr(bestEntity)
	bc.Role = component.RoleCommander

	// Add CommanderComponent if not present
	if s.cmdPool != nil {
		s.cmdPool.Add(bestEntity, component.CommanderComponent{
			SquadID:    squadID,
			IsAlive:    true,
			AuraRadius: bc.NeighborRange,
			Promoted:   true,
		})
	}

	// Record promotion for AI state update
	s.Promotions[squadID] = bestEntity
}
