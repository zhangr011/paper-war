package ai

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/fog"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// ============================================================================
// AI v2 — Strategic + Tactical Improvements (ADR-0017)
//
// Changes from v1:
//   - SquadAssessment: composition-aware engagement (range, strength, HP)
//   - Range-aware combat: uses squad's actual weapon range, not hardcoded 5.0
//   - Target prioritization: scores enemies by value, vulnerability, distance
//   - Force-ratio retreat: disengages from unwinnable fights, regroups at base
//   - Offensive push: actively advances toward enemy base (elimination objective)
//   - Wave recruitment: coordinated bursts instead of trickle-spending
//   - Persistent enemy intel with decay (robust adaptive recruitment)
// ============================================================================

const (
	StateIdle     uint8 = 0
	StatePatrol   uint8 = 1
	StateApproach uint8 = 2
	StateAttack   uint8 = 3
	StateRetreat  uint8 = 4
	StateDefend   uint8 = 5  // defend capture target or base
	StateScout    uint8 = 6  // exploring fogged areas
	StateCapture  uint8 = 7  // moving to capture stronghold
	StatePush     uint8 = 8  // offensive push toward enemy base
	StateRegroup  uint8 = 9  // falling back to rally with reinforcements
	StateGuard    uint8 = 10 // hold position and fire at detected enemies; never pursue

	EvalInterval uint32 = 30

	// --- v2 retreat thresholds ---
	RetreatHPThreshold = 0.25 // retreat when squad HP ratio below this AND outnumbered
	CriticallyLowHP    = 0.10 // always retreat below this regardless of odds
	ForceRatioRetreat  = 1.5  // retreat if enemyStrength/squadStrength exceeds this

	// Role definitions for recruitment strategy
	RoleFrontline = 0
	RoleRanged    = 1
	RoleHeavy     = 2

	// Strategic constants
	ExploreDuration      uint32 = 150 // ~30s at 5Hz tick
	BaseDefenseRadius           = 12.0
	BaseDefenseThreshold        = 2 // enemies near base to trigger defense (scaled response)

	// v2 constants
	IntelDecayInterval  uint32 = 100 // decay enemy intel every 100 ticks (~20s)
	IntelDecayFactor           = 0.7 // 30% reduction per decay cycle
	RecruitWaveInterval uint32 = 60  // ~12s between recruitment waves
	RecruitWaveMinGold  int32  = 30  // minimum gold to trigger a wave

	// HuntMemoryTicks is how long a last-known position (LKP) stays "worth
	// hunting". An enemy seen within the last ~30s (300 ticks at 10Hz) is
	// recent enough that the AI should seek that point on losing contact;
	// older sightings are presumed stale and fall through to push/patrol.
	// Tunable. See plan docs/plans/ai-hunt-seek.md.
	HuntMemoryTicks uint32 = 300
	DefaultEngageRange         = 5.0 // fallback when squad composition unknown

	// ADR-0027 — bounded engagement anti-kite guards.
	MaxPursuitDist     = 8.0  // tiles: max distance a squad will chase from its engagement anchor
	AvoidCooldownTicks uint32 = 60 // skip a broken-off target for this many ticks (~2 eval cycles)
)

// roleTargetRatio is the ideal army composition ratio for each role.
var roleTargetRatio = [3]float64{0.40, 0.30, 0.30}

// unitRole maps each combat unit type to its tactical role.
var unitRole = map[component.CombatUnitType]int{
	component.UnitLightInfantry:     RoleFrontline,
	component.UnitHeavyInfantry:     RoleFrontline,
	component.UnitSniper:            RoleRanged,
	component.UnitAntiArmorInfantry: RoleRanged,
	component.UnitMotorGun:          RoleHeavy,
	component.UnitMotorArtillery:    RoleHeavy,
	component.UnitMotorMissile:      RoleHeavy,
}

// roleUnits lists unit types available for each role, sorted by cost ascending.
var roleUnits = [3][]component.CombatUnitType{
	RoleFrontline: {component.UnitLightInfantry, component.UnitHeavyInfantry},
	RoleRanged:    {component.UnitSniper, component.UnitAntiArmorInfantry},
	RoleHeavy:     {component.UnitMotorGun, component.UnitMotorArtillery, component.UnitMotorMissile},
}

type AIState struct {
	SquadID      uint32
	CommanderID  uint32
	State        uint8
	TargetUnitID uint32
	PatrolX      int64
	PatrolY      int64
	NextEvalTick uint32

	// ADR-0027 — bounded engagement. Anchor is the squad position when it
	// first detected the current target; if the squad is pulled beyond
	// MaxPursuitDist from the anchor it breaks off, returns to the anchor,
	// and avoids that target until AvoidUntilTick.
	EngageEnemyID  uint32
	EngageAnchorX  int64
	EngageAnchorY  int64
	AvoidUnitID    uint32
	AvoidUntilTick uint32
}

type AICommand struct {
	Type     uint8 // CmdMove, CmdAttack, or CmdRecruit
	SquadID  uint32
	TargetX  int64
	TargetY  int64
	TargetID uint32
	UnitType component.CombatUnitType // for CmdRecruit
}

const (
	CmdMove    uint8 = 1
	CmdAttack  uint8 = 2
	CmdRecruit uint8 = 3
)

// SquadAssessment holds computed combat stats for an AI squad.
// Enables range-aware engagement, force-ratio evaluation, and composition tactics.
type SquadAssessment struct {
	UnitCount   int
	MaxRange    int64 // max weapon range among squad members (fixed-point tiles)
	TotalHP     int32
	TotalMaxHP  int32
	HPRatio     float64 // 0.0 - 1.0
	RangedCount int     // units with ranged/heavy roles
	MeleeCount  int     // units with frontline role
	Strength    int     // weighted combat strength (heavy armor = 2, others = 1)
}

// IsRangedDominant returns true if ranged+heavy units outnumber frontline units.
func (a SquadAssessment) IsRangedDominant() bool {
	return a.RangedCount > a.MeleeCount
}

// CommitRange returns the distance at which the squad should stop closing and hold fire.
// Ranged-dominant squads hold at their max range; melee-dominant squads close to 5.
func (a SquadAssessment) CommitRange() int64 {
	defaultRange := fixed.FromFloat(DefaultEngageRange)
	if a.IsRangedDominant() && a.MaxRange > defaultRange {
		return a.MaxRange
	}
	return defaultRange
}

type AISystem struct {
	States     map[uint32]*AIState
	AIPlayerID uint32
	FogSystem  *fog.FogSystem
	MapW, MapH int32
	Objective  *tilemap.Objective // objective for AI awareness
	PlayerGold map[uint32]int32   // reference to session gold pool

	// Base positions (fixed-point)
	BaseX      int64 // AI home base
	BaseY      int64
	EnemyBaseX int64 // enemy spawn for offensive pressure
	EnemyBaseY int64

	Strongholds [][2]int32 // stronghold tile positions on map
	// StrongholdFactions is parallel to Strongholds — the live owning faction
	// of each (0=player, 1=enemy, 0xFF=neutral). Refreshed each tick by the
	// session so the AI targets only capturable strongholds (#56 phase 2).
	StrongholdFactions []uint8
	// AIFaction is the faction this AI plays as; strongholdCommand skips
	// strongholds already owned by AIFaction.
	AIFaction uint8

	// Enemy composition intel — persistent across ticks (decays periodically)
	EnemyUnits map[component.CombatUnitType]int

	visitedSH       map[int]bool // strongholds already sent squads to
	RecruitDisabled bool         // true → AI never issues recruit commands (clash/spectator mode)
	MoveDisabled    bool         // true → AI never issues move commands (clash mirror mode)

	// rnd is the AI's own RNG source. Plumbing it through (instead of using
	// the process-global math/rand) makes AI decisions deterministic given
	// the map seed — see ADR-0028. Defaults to a time-seeded source when nil
	// so ad-hoc callers preserve prior behavior.
	rnd *rand.Rand

	// v2 internal state
	lastIntelDecay  uint32
	lastRecruitWave uint32

	// lastEnemySighting is the most recent position an enemy was seen, with
	// the tick it was seen. Single AI-wide LKP (intel is shared across the
	// AI's squads). Drives hunt/seek when no enemy is currently visible.
	// Updated in the combat branch whenever scanEnemiesScored returns a
	// target. See plan docs/plans/ai-hunt-seek.md.
	lastEnemySightingX, lastEnemySightingY int64
	lastEnemySightingTick                 uint32
}

func NewAISystem(aiPlayerID uint32, fogSys *fog.FogSystem, mapW, mapH int32, rnd *rand.Rand) *AISystem {
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &AISystem{
		States:     make(map[uint32]*AIState),
		AIPlayerID: aiPlayerID,
		FogSystem:  fogSys,
		MapW:       mapW,
		MapH:       mapH,
		EnemyUnits: make(map[component.CombatUnitType]int),
		visitedSH:  make(map[int]bool),
		rnd:        rnd,
	}
}

// SetRNG replaces the AI's RNG source. Tests use this to force a deterministic
// seed after construction; production wires it from the map seed so AI
// decisions track terrain determinism (ADR-0028).
func (as *AISystem) SetRNG(rnd *rand.Rand) {
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	as.rnd = rnd
}

func (as *AISystem) SetObjective(obj *tilemap.Objective) {
	as.Objective = obj
}

// SetBasePosition sets the AI's home base for defense logic.
func (as *AISystem) SetBasePosition(x, y int64) {
	as.BaseX = x
	as.BaseY = y
}

// SetEnemyBasePosition sets the enemy's spawn for offensive push logic.
func (as *AISystem) SetEnemyBasePosition(x, y int64) {
	as.EnemyBaseX = x
	as.EnemyBaseY = y
}

// SetStrongholds provides the list of stronghold positions for capture logic.
func (as *AISystem) SetStrongholds(positions [][2]int32) {
	as.Strongholds = positions
}

// SetStrongholdFactions sets the live owner faction of each stronghold
// (parallel to Strongholds). Fed each tick by the session (#56 phase 2).
func (as *AISystem) SetStrongholdFactions(factions []uint8) {
	as.StrongholdFactions = factions
}

// SetAIFaction sets the faction this AI plays as, so strongholdCommand can
// skip strongholds it already owns.
func (as *AISystem) SetAIFaction(f uint8) {
	as.AIFaction = f
}

func (as *AISystem) RegisterSquad(squadID, commanderID uint32) {
	state := &AIState{
		SquadID:     squadID,
		CommanderID: commanderID,
		State:       StateIdle,
	}
	as.pickPatrolTarget(state)
	as.States[squadID] = state
}

// hasEnemyBase returns true if the enemy spawn position is known.
func (as *AISystem) hasEnemyBase() bool {
	return as.EnemyBaseX != 0 || as.EnemyBaseY != 0
}

func (as *AISystem) Update(
	tick uint32,
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	boidPool *ecs.ComponentPool[component.BoidComponent],
) []AICommand {
	var cmds []AICommand
	var aiFog *fog.FogGrid
	if as.FogSystem != nil {
		aiFog = as.FogSystem.GetGrid(as.AIPlayerID)
	}

	// Decay enemy intel periodically (keeps stale sightings from dominating)
	if tick-as.lastIntelDecay >= IntelDecayInterval {
		as.decayIntel()
		as.lastIntelDecay = tick
	}

	// v2: Recruitment uses persistent intel (not reset each tick)
	if !as.RecruitDisabled {
		cmds = append(cmds, as.recruitDecisions(tick)...)
	}

	// Iterate squads in sorted-ID order so the run is reproducible: Go
	// randomizes map iteration order, and per-squad command issue can
	// interact (e.g. two squads contesting a target) (ADR-0028).
	squadIDs := make([]uint32, 0, len(as.States))
	for id := range as.States {
		squadIDs = append(squadIDs, id)
	}
	sort.Slice(squadIDs, func(i, j int) bool { return squadIDs[i] < squadIDs[j] })
	for _, squadID := range squadIDs {
		state := as.States[squadID]
		cmdEntity := ecs.Entity(state.CommanderID)
		pos, hasPos := posPool.Get(cmdEntity)
		health, hasHealth := healthPool.Get(cmdEntity)
		if !hasPos || !hasHealth {
			continue
		}

		// --- ASSESS SQUAD COMPOSITION ---
		assessment := as.assessSquad(squadID, boidPool, healthPool, unitTypePool,
			cmdEntity, &health)

		// --- EMERGENCY RETREAT (bypass cooldown) ---
		// Critically low HP units always retreat, regardless of force ratio.
		hpRatio := assessment.HPRatio
		if hpRatio > 0 && hpRatio < CriticallyLowHP && state.State != StateRetreat {
			state.State = StateRetreat
			if !as.MoveDisabled {
				cmds = append(cmds, as.retreatCommand(squadID, pos))
			}
			continue
		}

		// Cooldown for strategic decisions
		if tick < state.NextEvalTick {
			continue
		}
		state.NextEvalTick = tick + EvalInterval

		// Clear retreat if HP has recovered sufficiently
		if state.State == StateRetreat && hpRatio > RetreatHPThreshold+0.15 {
			state.State = StateIdle
		}

		// --- CAPTURE OBJECTIVE DEFENSE ---
		if as.Objective != nil && as.Objective.Type == tilemap.ObjectiveCapture {
			cmds = append(cmds, as.captureDefense(squadID, state, pos)...)
			continue
		}

		// --- SCAN ENEMIES (scored target selection + force ratio) ---
		// ADR-0027: recover dist/pos (v3 Guard discarded them) for the
		// bounded-engagement range + break-off checks.
		_, bestDist, bestEnemyID, bestEnemyX, bestEnemyY, enemyStrength :=
			as.scanEnemiesScored(cmdPool, posPool, ownerPool, healthPool, unitTypePool, aiFog, pos)

		// --- BASE DEFENSE (scaled response) ---
		// Only recall non-engaged squads when a significant force threatens the base.
		if as.BaseX != 0 || as.BaseY != 0 {
			nearBase := as.countEnemiesNearBase(
				cmdPool, posPool, ownerPool, healthPool, unitTypePool, aiFog)
			if nearBase >= BaseDefenseThreshold &&
				state.State != StateAttack && state.State != StateApproach {
				state.State = StateDefend
				if !as.MoveDisabled {
					cmds = append(cmds, AICommand{
						Type:    CmdMove,
						SquadID: squadID,
						TargetX: as.BaseX,
						TargetY: as.BaseY,
					})
				}
				continue
			}
		}

		// --- COMBAT (highest priority if enemies visible) ---
		// ADR-0027: bounded engagement. Reintroduces closing on out-of-
		// range targets (removed by v3 Guard, issue #52) with anti-kite
		// guards: stop at CommitRange, break off if pulled beyond
		// MaxPursuitDist from the engagement anchor, avoid that target
		// for AvoidCooldownTicks after a break-off.
		if bestEnemyID != 0 {
			// LKP refresh: a visible enemy is the freshest intel we can
			// have — record its position + tick so the hunt/seek behavior
			// in the no-enemy branch has a real target to converge on
			// when contact is later lost. (Plan: ai-hunt-seek.)
			as.lastEnemySightingX = bestEnemyX
			as.lastEnemySightingY = bestEnemyY
			as.lastEnemySightingTick = tick
			// Force-ratio check: retreat if badly outnumbered and not at full strength.
			squadStrength := assessment.Strength
			if squadStrength > 0 && enemyStrength > 0 &&
				float64(enemyStrength)/float64(squadStrength) > ForceRatioRetreat &&
				hpRatio < 0.60 {
				state.TargetUnitID = bestEnemyID
				state.State = StateRetreat
				if !as.MoveDisabled {
					cmds = append(cmds, as.retreatCommand(squadID, pos))
				}
				continue
			}

			// Avoid-cooldown gate: this enemy was broken off recently.
			// Skip combat this eval and fall through to strategic behavior
			// by clearing bestEnemyID (the no-enemy path runs below).
			if state.AvoidUnitID == bestEnemyID && tick < state.AvoidUntilTick {
				bestEnemyID = 0
			} else {
				// Anchor bookkeeping — record squad position on first
				// detection of this target.
				if state.EngageEnemyID != bestEnemyID {
					state.EngageEnemyID = bestEnemyID
					state.EngageAnchorX = pos.X
					state.EngageAnchorY = pos.Y
				}

				// Break-off: squad pulled beyond MaxPursuitDist from its
				// engagement anchor. Drop the target, return to anchor,
				// set avoid-cooldown. Compare squared distances in raw
				// fixed² units (no ISqrt needed here).
				maxPursuit := fixed.FromFloat(MaxPursuitDist)
				adx := pos.X - state.EngageAnchorX
				ady := pos.Y - state.EngageAnchorY
				if adx*adx+ady*ady > maxPursuit*maxPursuit {
					state.AvoidUnitID = bestEnemyID
					state.AvoidUntilTick = tick + AvoidCooldownTicks
					state.EngageEnemyID = 0
					state.TargetUnitID = 0
					state.State = StateGuard
					if !as.MoveDisabled {
						cmds = append(cmds, AICommand{
							Type:    CmdMove,
							SquadID: squadID,
							TargetX: state.EngageAnchorX,
							TargetY: state.EngageAnchorY,
						})
					}
					continue
				}

				state.TargetUnitID = bestEnemyID

				// Close vs Guard. commitRange is fixed-point tiles; bestDist
				// is the raw squared fixed-product (dx*dx + dy*dy, no shift),
				// so it compares directly against commitRange*commitRange.
				commitRange := assessment.CommitRange()
				if bestDist > commitRange*commitRange {
					// Out of range → Approach. Move to a point at
					// commitRange from the enemy (toward the squad), NOT
					// the enemy tile — stops the squad from overrunning
					// into melee and lets ranged squads hover at max range.
					dirX := pos.X - bestEnemyX
					dirY := pos.Y - bestEnemyY
					// bestDist is raw (dx*dx + dy*dy) in fixed² units; shift
					// to fixed-point tiles² before ISqrt so the result is a
					// fixed-point tile distance.
					dist := fixed.ISqrt(bestDist >> 12)
					var tx, ty int64
					if dist > 0 {
						ux := fixed.Div(dirX, dist)
						uy := fixed.Div(dirY, dist)
						tx = bestEnemyX + fixed.Mul(ux, commitRange)
						ty = bestEnemyY + fixed.Mul(uy, commitRange)
					} else {
						tx = bestEnemyX
						ty = bestEnemyY
					}
					state.State = StateApproach
					if !as.MoveDisabled {
						cmds = append(cmds, AICommand{
							Type:    CmdMove,
							SquadID: squadID,
							TargetX: tx,
							TargetY: ty,
						})
					}
					continue
				}

				// In range → Guard + fire (today's behavior).
				state.State = StateGuard
				cmds = append(cmds, AICommand{
					Type:     CmdAttack,
					SquadID:  squadID,
					TargetID: bestEnemyID,
				})
				continue
			}
		}

		// --- STRATEGIC BEHAVIORS (no enemy visible) ---
		if as.MoveDisabled {
			continue
		}

		// Clear combat states
		if state.State == StateAttack || state.State == StateApproach {
			state.State = StateIdle
		}

		// Early-game exploration: first squad scouts fogged areas
		if tick < ExploreDuration && squadID == as.firstSquadID() {
			scoutCmd := as.exploreCommand(squadID, state, pos, aiFog)
			if scoutCmd != nil {
				cmds = append(cmds, *scoutCmd)
				continue
			}
		}

		// Hunt: if we have a recent last-known enemy position, converge on
		// it. LKP is *where enemies actually are*; the push below goes to
		// the enemy base (where they may not be). Hunt takes priority so
		// survivors that slipped out of vision are re-acquired instead of
		// the AI oscillating back to the base. (Plan: ai-hunt-seek.)
		if huntCmd := as.huntCommand(squadID, state, pos, tick); huntCmd != nil {
			cmds = append(cmds, *huntCmd)
			continue
		}

		// v2: Offensive push for elimination objective — advance toward enemy base.
		if as.Objective != nil && as.Objective.Type == tilemap.ObjectiveElimination &&
			as.hasEnemyBase() {
			pushCmd := as.offensivePushCommand(squadID, state, pos)
			if pushCmd != nil {
				cmds = append(cmds, *pushCmd)
				continue
			}
		}

		// Stronghold capture: send squad to nearest unvisited stronghold
		shCmd := as.strongholdCommand(squadID, state, pos)
		if shCmd != nil {
			cmds = append(cmds, *shCmd)
			continue
		}

		// Sweep fallback: no recent LKP and already at/without an enemy base —
		// bias the patrol toward the enemy half and fogged tiles rather than
		// a fully random point, so the AI keeps looking for enemies. (Plan:
		// ai-hunt-seek.) Only fires when there is a real fog grid, so ad-hoc
		// unit tests without fog fall through to plain patrol as before.
		if aiFog != nil && state.State != StateDefend && state.State != StateRetreat {
			if sweepCmd := as.sweepCommand(squadID, state, pos, aiFog); sweepCmd != nil {
				cmds = append(cmds, *sweepCmd)
				continue
			}
		}

		// Default: patrol
		state.State = StatePatrol
		cmds = append(cmds, AICommand{
			Type:    CmdMove,
			SquadID: squadID,
			TargetX: state.PatrolX,
			TargetY: state.PatrolY,
		})
		dx := state.PatrolX - pos.X
		dy := state.PatrolY - pos.Y
		if dx*dx+dy*dy < fixed.FromFloat(4.0)*fixed.FromFloat(4.0)>>12 {
			as.pickPatrolTarget(state)
		}
	}

	return cmds
}

// assessSquad computes combat stats for a squad by iterating its members.
// Includes the commander plus all combat units (BoidComponent with matching SquadID).
func (as *AISystem) assessSquad(
	squadID uint32,
	boidPool *ecs.ComponentPool[component.BoidComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	cmdEntity ecs.Entity,
	cmdHealth *component.HealthComponent,
) SquadAssessment {
	a := SquadAssessment{
		MaxRange: fixed.FromFloat(DefaultEngageRange),
	}

	// Helper to process a single entity's contribution
	processUnit := func(e ecs.Entity, hp component.HealthComponent) {
		a.UnitCount++
		a.TotalHP += hp.HP
		a.TotalMaxHP += hp.MaxHP

		ut, ok := unitTypePool.Get(e)
		if !ok {
			a.Strength += 1
			a.MeleeCount++
			return
		}

		stats := component.CombatUnitTypeTable[ut.Type]
		rangeFix := fixed.FromFloat(float64(stats.Range))
		if rangeFix > a.MaxRange {
			a.MaxRange = rangeFix
		}

		if ut.Armor == component.ArmorHeavy {
			a.Strength += 2
		} else {
			a.Strength += 1
		}

		role := unitRole[ut.Type]
		if role == RoleRanged || role == RoleHeavy {
			a.RangedCount++
		} else {
			a.MeleeCount++
		}
	}

	// Include commander
	processUnit(cmdEntity, *cmdHealth)

	// Iterate squad members
	boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		if bc.SquadID != squadID || bc.Role == component.RoleCommander {
			return
		}
		hp, hasHP := healthPool.Get(e)
		if !hasHP || hp.HP <= 0 {
			return
		}
		processUnit(e, hp)
	})

	if a.TotalMaxHP > 0 {
		a.HPRatio = float64(a.TotalHP) / float64(a.TotalMaxHP)
	}
	return a
}

// scanEnemiesScored finds the best-scoring enemy target and counts enemy strength.
// Returns (bestScore, distSq, enemyID, enemyX, enemyY, enemyStrength).
// enemyID=0 if no visible enemy found. Also accumulates persistent enemy intel.
func (as *AISystem) scanEnemiesScored(
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	aiFog *fog.FogGrid,
	pos component.PositionComponent,
) (bestScore float64, bestDist int64, bestEnemyID uint32, bestEnemyX, bestEnemyY int64, enemyStrength int) {
	bestScore = -1
	bestDist = -1

	// Single pass through healthPool — catches both commanders and combat units.
	healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner || owner.PlayerID == as.AIPlayerID {
			return
		}
		ePos, hasEPos := posPool.Get(e)
		if !hasEPos {
			return
		}
		if aiFog != nil && !aiFog.IsVisible(int32(ePos.X>>12), int32(ePos.Y>>12)) {
			return
		}

		// Record persistent enemy intel
		ut, hasUT := unitTypePool.Get(e)
		if hasUT {
			as.EnemyUnits[ut.Type]++
		}

		// Strength estimate
		_, isCommander := cmdPool.Get(e)
		if hasUT && ut.Armor == component.ArmorHeavy {
			enemyStrength += 2
		} else {
			enemyStrength += 1
		}

		// Distance
		dx := ePos.X - pos.X
		dy := ePos.Y - pos.Y
		dist := dx*dx + dy*dy

		// Score this target
		hpRatio := 1.0
		if hp.MaxHP > 0 {
			hpRatio = float64(hp.HP) / float64(hp.MaxHP)
		}
		enemyArmor := component.ArmorLight
		if hasUT {
			enemyArmor = ut.Armor
		}
		score := scoreTarget(dist, isCommander, hpRatio, enemyArmor)

		if score > bestScore {
			bestScore = score
			bestDist = dist
			bestEnemyID = uint32(e)
			bestEnemyX = ePos.X
			bestEnemyY = ePos.Y
		}
	})

	return
}

// scoreTarget returns a priority score for an enemy unit. Higher = better target.
//
// Factors:
//   - Distance: closer is preferred (inverse distance)
//   - Commander: 3x priority (elimination win condition)
//   - Low HP: finish kills for bounty and to reduce enemy force
//   - Heavy armor: high-threat, worth prioritizing
func scoreTarget(distSq int64, isCommander bool, hpRatio float64, enemyArmor component.ArmorType) float64 {
	if distSq <= 0 {
		distSq = 1
	}
	// Convert from fixed-point squared to tiles: sqrt(distSq) / One
	distTiles := math.Sqrt(float64(distSq)) / float64(fixed.One)

	// Base: inverse distance (closer = better, with +1 to avoid div-by-zero)
	score := 10.0 / (distTiles + 1.0)

	// Commander priority — winning the game matters most
	if isCommander {
		score *= 3.0
	}

	// Low HP bonus — finish off weakened enemies (bounty + removes threat)
	if hpRatio < 0.3 {
		score *= 1.8
	} else if hpRatio < 0.5 {
		score *= 1.3
	}

	// Heavy armor priority — high-threat units worth focusing
	if enemyArmor == component.ArmorHeavy {
		score *= 1.2
	}

	return score
}

// retreatCommand creates a move command toward the AI's base (or own side of map).
func (as *AISystem) retreatCommand(squadID uint32, pos component.PositionComponent) AICommand {
	retreatX := as.BaseX
	retreatY := as.BaseY
	if retreatX == 0 && retreatY == 0 {
		retreatX = fixed.FromFloat(1.0)
		if fixed.ToFloat(pos.X) > float64(as.MapW)/2 {
			retreatX = fixed.FromFloat(float64(as.MapW) - 2)
		}
		retreatY = pos.Y
	}
	return AICommand{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: retreatX,
		TargetY: retreatY,
	}
}

// offensivePushCommand sends a squad toward the enemy base via the nearest-to-enemy
// stronghold as a natural waypoint (creates force concentration at forward positions).
// Returns nil if the squad is already near the enemy base.
func (as *AISystem) offensivePushCommand(squadID uint32, state *AIState, pos component.PositionComponent) *AICommand {
	targetX := as.EnemyBaseX
	targetY := as.EnemyBaseY

	// Already at enemy base — let other behaviors take over (scout/patrol in enemy area)
	dx := targetX - pos.X
	dy := targetY - pos.Y
	arriveThreshold := fixed.FromFloat(10.0) * fixed.FromFloat(10.0)
	if dx*dx+dy*dy < arriveThreshold {
		return nil
	}

	// Use the stronghold closest to the enemy base as a rally waypoint.
	// This naturally concentrates multiple squads before the final push.
	bestSH := -1
	bestSHDistToEnemy := int64(-1)
	for i, sh := range as.Strongholds {
		shX := fixed.FromFloat(float64(sh[0]))
		shY := fixed.FromFloat(float64(sh[1]))
		sdx := shX - targetX
		sdy := shY - targetY
		dToEnemy := sdx*sdx + sdy*sdy
		if bestSHDistToEnemy < 0 || dToEnemy < bestSHDistToEnemy {
			bestSHDistToEnemy = dToEnemy
			bestSH = i
		}
	}

	if bestSH >= 0 {
		sh := as.Strongholds[bestSH]
		shX := fixed.FromFloat(float64(sh[0]))
		shY := fixed.FromFloat(float64(sh[1]))
		// If we haven't reached the waypoint stronghold yet, go there first
		dxSH := shX - pos.X
		dySH := shY - pos.Y
		waypointThreshold := fixed.FromFloat(6.0) * fixed.FromFloat(6.0)
		if dxSH*dxSH+dySH*dySH > waypointThreshold {
			targetX = shX
			targetY = shY
		}
	}

	state.State = StatePush
	return &AICommand{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: targetX,
		TargetY: targetY,
	}
}

// countEnemiesNearBase returns how many enemy units are within BaseDefenseRadius of AI base.
func (as *AISystem) countEnemiesNearBase(
	cmdPool *ecs.ComponentPool[component.CommanderComponent],
	posPool *ecs.ComponentPool[component.PositionComponent],
	ownerPool *ecs.ComponentPool[component.OwnerComponent],
	healthPool *ecs.ComponentPool[component.HealthComponent],
	unitTypePool *ecs.ComponentPool[component.UnitTypeComponent],
	aiFog *fog.FogGrid,
) int {
	if as.BaseX == 0 && as.BaseY == 0 {
		return 0
	}
	radiusSq := fixed.FromFloat(BaseDefenseRadius) * fixed.FromFloat(BaseDefenseRadius)
	count := 0

	healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		owner, hasOwner := ownerPool.Get(e)
		if !hasOwner || owner.PlayerID == as.AIPlayerID {
			return
		}
		ePos, hasEPos := posPool.Get(e)
		if !hasEPos {
			return
		}
		dx := ePos.X - as.BaseX
		dy := ePos.Y - as.BaseY
		if dx*dx+dy*dy <= radiusSq {
			count++
		}
	})

	return count
}

// exploreCommand sends a squad to scout a fogged area of the map.
func (as *AISystem) exploreCommand(squadID uint32, state *AIState, pos component.PositionComponent, aiFog *fog.FogGrid) *AICommand {
	if aiFog == nil {
		return nil
	}

	// Pick a fogged sector to explore — try random points toward map center
	// (enemies typically spawn on the opposite side)
	for attempts := 0; attempts < 5; attempts++ {
		// Bias exploration toward the center and opposite side of the map
		enemySide := float64(as.MapW) * 0.25 // toward player's side
		if fixed.ToFloat(pos.X) > float64(as.MapW)/2 {
			enemySide = float64(as.MapW) * 0.75
		}
		tx := int32(enemySide + as.rnd.Float64()*10 - 5)
		ty := int32(as.rnd.Float64() * float64(as.MapH))
		if tx < 1 {
			tx = 1
		}
		if tx >= as.MapW-1 {
			tx = as.MapW - 2
		}
		if ty < 1 {
			ty = 1
		}
		if ty >= as.MapH-1 {
			ty = as.MapH - 2
		}

		if !aiFog.IsVisible(tx, ty) {
			state.State = StateScout
			return &AICommand{
				Type:    CmdMove,
				SquadID: squadID,
				TargetX: fixed.FromFloat(float64(tx)),
				TargetY: fixed.FromFloat(float64(ty)),
			}
		}
	}

	return nil
}

// huntCommand moves a squad toward the most recent last-known enemy position
// (LKP). Returns nil if no sighting has ever been recorded, or if the LKP is
// older than HuntMemoryTicks (the enemy is presumed long-gone). On a fresh LKP
// the squad re-enters StateScout; on arrival it re-scans from closer range and
// either re-detects (→ combat branch, LKP refreshes) or finds nothing (LKP
// eventually goes stale → falls through to push/sweep). Mirrors the style of
// exploreCommand/offensivePushCommand. (Plan: ai-hunt-seek.)
func (as *AISystem) huntCommand(squadID uint32, state *AIState, pos component.PositionComponent, tick uint32) *AICommand {
	// No sighting ever recorded.
	if as.lastEnemySightingTick == 0 {
		return nil
	}
	// Stale — enemy seen too long ago to be worth chasing.
	if tick-as.lastEnemySightingTick > HuntMemoryTicks {
		return nil
	}

	// Already at the LKP — nothing more to hunt here; let other behaviors run.
	dx := as.lastEnemySightingX - pos.X
	dy := as.lastEnemySightingY - pos.Y
	arriveThreshold := fixed.FromFloat(3.0) * fixed.FromFloat(3.0)
	if dx*dx+dy*dy < arriveThreshold {
		return nil
	}

	state.State = StateScout
	return &AICommand{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: as.lastEnemySightingX,
		TargetY: as.lastEnemySightingY,
	}
}

// sweepCommand is the no-LKP fallback: send the squad on a rotating sweep
// across the enemy half of the map, preferring currently-fogged tiles so the
// AI keeps uncovering enemies rather than milling around its own base. Runs
// only when hunt returns nil (no recent LKP) and offensivePushCommand also
// returned nil (already at/without an enemy base). Lightweight: a single
// deterministic sector point per call, reusing the exploreCommand fog check.
// (Plan: ai-hunt-seek.)
func (as *AISystem) sweepCommand(squadID uint32, state *AIState, pos component.PositionComponent, aiFog *fog.FogGrid) *AICommand {
	if as.MapW == 0 || as.MapH == 0 {
		return nil
	}

	// Center the sweep on the enemy half of the map. The AI spawns at the
	// bottom for player 2; the enemy half is the top. Generalize by flipping
	// relative to the AI's own base when known, else by map quadrants.
	centerX := float64(as.MapW) / 2
	centerY := float64(as.MapH) / 2
	if as.BaseY != 0 {
		// Enemy half is the half of the map the AI does NOT spawn on.
		if fixed.ToFloat(as.BaseY) > centerY {
			// AI spawns low → enemy half is the top half.
			centerY = float64(as.MapH) * 0.25
		} else {
			centerY = float64(as.MapH) * 0.75
		}
	}

	// Try a few fog-biased points; accept the first fogged one. If fog is
	// unavailable, accept the first deterministic point.
	for attempts := 0; attempts < 4; attempts++ {
		tx := int32(centerX + (as.rnd.Float64()*2 - 1) * float64(as.MapW) * 0.3)
		ty := int32(centerY + (as.rnd.Float64()*2 - 1) * float64(as.MapH) * 0.25)
		if tx < 1 {
			tx = 1
		}
		if tx >= as.MapW-1 {
			tx = as.MapW - 2
		}
		if ty < 1 {
			ty = 1
		}
		if ty >= as.MapH-1 {
			ty = as.MapH - 2
		}

		if aiFog == nil || !aiFog.IsVisible(tx, ty) {
			state.State = StateScout
			return &AICommand{
				Type:    CmdMove,
				SquadID: squadID,
				TargetX: fixed.FromFloat(float64(tx)),
				TargetY: fixed.FromFloat(float64(ty)),
			}
		}
	}

	return nil
}

// strongholdCommand sends a squad to the nearest unvisited stronghold.
func (as *AISystem) strongholdCommand(squadID uint32, state *AIState, pos component.PositionComponent) *AICommand {
	if len(as.Strongholds) == 0 {
		return nil
	}

	bestIdx := -1
	bestDist := int64(-1)

	for i, sh := range as.Strongholds {
		if as.visitedSH[i] {
			continue
		}
		// Skip strongholds this AI already owns — only target capturable
		// (neutral or enemy) ones. (#56 phase 2.)
		if i < len(as.StrongholdFactions) && as.StrongholdFactions[i] == as.AIFaction {
			continue
		}
		shX := fixed.FromFloat(float64(sh[0]))
		shY := fixed.FromFloat(float64(sh[1]))
		dx := shX - pos.X
		dy := shY - pos.Y
		dist := dx*dx + dy*dy
		if bestDist < 0 || dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}

	if bestIdx < 0 {
		// All visited — reset and allow re-visiting
		as.visitedSH = make(map[int]bool)
		return nil
	}

	as.visitedSH[bestIdx] = true
	sh := as.Strongholds[bestIdx]
	state.State = StateCapture
	return &AICommand{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: fixed.FromFloat(float64(sh[0])),
		TargetY: fixed.FromFloat(float64(sh[1])),
	}
}

// firstSquadID returns the smallest registered squad ID (used for scouting).
// Deterministic — Go randomizes map iteration order, so picking "the first
// key range gives us" would make the scout (and thus the whole early game)
// non-reproducible run-to-run (ADR-0028).
func (as *AISystem) firstSquadID() uint32 {
	min := uint32(0)
	first := true
	for id := range as.States {
		if first || id < min {
			min = id
			first = false
		}
	}
	return min
}

// captureDefense returns commands to move AI squad to the capture target.
func (as *AISystem) captureDefense(squadID uint32, state *AIState, pos component.PositionComponent) []AICommand {
	if as.Objective == nil {
		return nil
	}
	targetX := fixed.FromFloat(float64(as.Objective.TargetX))
	targetY := fixed.FromFloat(float64(as.Objective.TargetY))
	dx := targetX - pos.X
	dy := targetY - pos.Y
	distSq := dx*dx + dy*dy

	// If already at the target, no movement needed
	if distSq < fixed.FromFloat(4.0)*fixed.FromFloat(4.0)>>12 {
		state.State = StateDefend
		return nil
	}

	state.State = StateDefend
	return []AICommand{{
		Type:    CmdMove,
		SquadID: squadID,
		TargetX: targetX,
		TargetY: targetY,
	}}
}

// ============================================================================
// RECRUITMENT
// ============================================================================

// recruitDecisions returns recruit commands based on adaptive role-balanced strategy.
// v2: Wave-based timing — accumulates gold for coordinated bursts instead of trickle.
// Role ratios shift based on observed enemy composition (persistent intel).
func (as *AISystem) recruitDecisions(tick uint32) []AICommand {
	if as.PlayerGold == nil {
		return nil
	}
	gold := as.PlayerGold[as.AIPlayerID]
	if gold < 15 { // cheapest unit is LI at 15
		return nil
	}

	// Wave-based timing: wait between waves unless gold is piling up.
	// First wave (lastRecruitWave == 0) is always immediate.
	if as.lastRecruitWave > 0 && tick-as.lastRecruitWave < RecruitWaveInterval {
		// In cooldown — only recruit if gold is excessive (3x wave minimum)
		if gold < RecruitWaveMinGold*3 {
			return nil
		}
	}
	as.lastRecruitWave = tick

	// Compute adaptive role ratios based on enemy intel
	adaptiveRatio := as.adaptiveRoleRatio()

	// Count living units by role
	roleCount := [3]int{}

	var cmds []AICommand
	for i := 0; i < 3; i++ { // max 3 recruits per tick
		if gold < 15 {
			break
		}

		// Pick the most underrepresented role using adaptive ratios
		role := as.pickRoleWithRatio(roleCount, adaptiveRatio)
		if role < 0 {
			role = RoleFrontline // fallback
		}

		// Pick an affordable unit from that role
		ut := as.pickAffordableUnit(role, gold)
		if ut == nil {
			// Can't afford any unit in this role, try cheapest overall
			ut = as.cheapestAffordableUnit(gold)
			if ut == nil {
				break
			}
		}

		cost := component.CombatUnitTypeTable[*ut].RecruitCost
		gold -= cost
		roleCount[unitRole[*ut]]++
		cmds = append(cmds, AICommand{
			Type:     CmdRecruit,
			UnitType: *ut,
		})
	}

	return cmds
}

// adaptiveRoleRatio adjusts target ratios based on observed enemy composition.
// If no enemies seen, returns the default balanced ratio.
func (as *AISystem) adaptiveRoleRatio() [3]float64 {
	total := 0
	roleSeen := [3]int{}
	for ut, count := range as.EnemyUnits {
		total += count
		roleSeen[unitRole[ut]] += count
	}

	if total == 0 {
		return roleTargetRatio // no intel — use default
	}

	// Adjust ratios based on what the enemy is building
	enemyFrontPct := float64(roleSeen[RoleFrontline]) / float64(total)
	enemyRangedPct := float64(roleSeen[RoleRanged]) / float64(total)
	enemyHeavyPct := float64(roleSeen[RoleHeavy]) / float64(total)

	ratio := roleTargetRatio

	// Counter-logic:
	// - Enemy many Ranged (Snipers) → boost Frontline (close distance fast)
	if enemyRangedPct > 0.4 {
		ratio[RoleFrontline] += 0.15
		ratio[RoleRanged] -= 0.10
		ratio[RoleHeavy] -= 0.05
	}

	// - Enemy many Heavy → boost Ranged (Anti-Armor is ranged)
	if enemyHeavyPct > 0.4 {
		ratio[RoleRanged] += 0.15
		ratio[RoleFrontline] -= 0.10
		ratio[RoleHeavy] -= 0.05
	}

	// - Enemy many Frontline → boost Ranged (Snipers pick off infantry)
	if enemyFrontPct > 0.5 {
		ratio[RoleRanged] += 0.10
		ratio[RoleHeavy] -= 0.05
		ratio[RoleFrontline] -= 0.05
	}

	// Clamp ratios to valid range
	for i := range ratio {
		if ratio[i] < 0.10 {
			ratio[i] = 0.10
		}
		if ratio[i] > 0.60 {
			ratio[i] = 0.60
		}
	}

	// Normalize to sum = 1.0
	sum := ratio[0] + ratio[1] + ratio[2]
	for i := range ratio {
		ratio[i] /= sum
	}

	return ratio
}

// decayIntel reduces enemy composition counts to age out stale sightings.
func (as *AISystem) decayIntel() {
	for ut, count := range as.EnemyUnits {
		decayed := int(float64(count) * IntelDecayFactor)
		if decayed <= 0 {
			delete(as.EnemyUnits, ut)
		} else {
			as.EnemyUnits[ut] = decayed
		}
	}
}

// pickRoleWithRatio returns the role index most underrepresented relative to given ratios.
func (as *AISystem) pickRoleWithRatio(roleCount [3]int, ratio [3]float64) int {
	total := roleCount[0] + roleCount[1] + roleCount[2]
	if total == 0 {
		return RoleFrontline
	}

	worstRole := -1
	worstDeficit := 0.0
	for r := 0; r < 3; r++ {
		actual := float64(roleCount[r]) / float64(total)
		deficit := ratio[r] - actual
		if deficit > worstDeficit {
			worstDeficit = deficit
			worstRole = r
		}
	}
	if worstRole < 0 {
		worstRole = as.rnd.Intn(3)
	}
	return worstRole
}

// pickAffordableUnit returns a random affordable unit from the given role.
func (as *AISystem) pickAffordableUnit(role int, gold int32) *component.CombatUnitType {
	candidates := roleUnits[role]
	idx := as.rnd.Intn(len(candidates))
	for i := 0; i < len(candidates); i++ {
		ut := candidates[(idx+i)%len(candidates)]
		if component.CombatUnitTypeTable[ut].RecruitCost <= gold {
			return &ut
		}
	}
	return nil
}

// cheapestAffordableUnit returns the cheapest unit the AI can afford across all roles.
func (as *AISystem) cheapestAffordableUnit(gold int32) *component.CombatUnitType {
	var best *component.CombatUnitType
	var bestCost int32 = math.MaxInt32
	for _, units := range roleUnits {
		for _, ut := range units {
			cost := component.CombatUnitTypeTable[ut].RecruitCost
			if cost <= gold && cost < bestCost {
				u := ut
				best = &u
				bestCost = cost
			}
		}
	}
	return best
}

func (as *AISystem) pickPatrolTarget(state *AIState) {
	margin := 5.0
	state.PatrolX = fixed.FromFloat(margin + as.rnd.Float64()*(float64(as.MapW)-margin*2))
	state.PatrolY = fixed.FromFloat(margin + as.rnd.Float64()*(float64(as.MapH)-margin*2))
}
