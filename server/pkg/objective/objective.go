package objective

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

// MatchResult indicates the outcome of a match.
type MatchResult struct {
	Winner uint8  // 0 = player, 1 = enemy
	Reason string // "elimination", "capture", "survival"
	Draw   bool
}

// ObjectiveSystem checks win conditions each tick based on the GameMap's Objective.
type ObjectiveSystem struct {
	gm *tilemap.GameMap

	healthPool *ecs.ComponentPool[component.HealthComponent]
	boidPool   *ecs.ComponentPool[component.BoidComponent]
	ownerPool  *ecs.ComponentPool[component.OwnerComponent]
	posPool    *ecs.ComponentPool[component.PositionComponent]

	// Capture state (tracks who is currently holding the capture point)
	captureHolder uint8 // 0=none, 1=player, 2=enemy (Faction + 1)

	result *MatchResult // set when match ends
}

func NewObjectiveSystem(gm *tilemap.GameMap) *ObjectiveSystem {
	return &ObjectiveSystem{gm: gm}
}

func (s *ObjectiveSystem) Name() string  { return "ObjectiveSystem" }
func (s *ObjectiveSystem) Priority() int { return 95 } // after Death(90)

func (s *ObjectiveSystem) Init(w *ecs.World) {
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	if p := w.Pool(component.OwnerComponent{}); p != nil {
		s.ownerPool = p.(*ecs.ComponentPool[component.OwnerComponent])
	}
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
}

func (s *ObjectiveSystem) Tick(w *ecs.World, tick uint32) {
	if s.result != nil {
		return // match already ended
	}

	switch s.gm.Objective.Type {
	case tilemap.ObjectiveElimination:
		s.checkElimination()
	case tilemap.ObjectiveCapture:
		s.checkCapture()
	case tilemap.ObjectiveSurvival:
		s.checkSurvival(tick)
	}
}

// Result returns the match result, or nil if the match is still ongoing.
func (s *ObjectiveSystem) Result() *MatchResult {
	return s.result
}

// CaptureState returns the current capture holder and hold counter for HUD display.
func (s *ObjectiveSystem) CaptureState() (holder uint8, counter int32) {
	return s.captureHolder, s.gm.Objective.HoldCounter
}

func (s *ObjectiveSystem) checkElimination() {
	playerAlive := 0
	enemyAlive := 0

	s.healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		if s.ownerPool != nil {
			if owner, ok := s.ownerPool.Get(e); ok {
				if owner.Faction == component.FactionPlayer {
					playerAlive++
				} else {
					enemyAlive++
				}
				return
			}
		}
		playerAlive++
	})

	if playerAlive == 0 {
		s.result = &MatchResult{Winner: component.FactionEnemy, Reason: "elimination"}
	} else if enemyAlive == 0 {
		s.result = &MatchResult{Winner: component.FactionPlayer, Reason: "elimination"}
	}
}

func (s *ObjectiveSystem) checkCapture() {
	// Convert tile coords to fixed-point for distance check
	targetX := fixed.FromFloat(float64(s.gm.Objective.TargetX))
	targetY := fixed.FromFloat(float64(s.gm.Objective.TargetY))

	// Find the faction with units closest to the capture point
	bestFaction := uint8(255)
	bestDist := int64(0x7FFFFFFFFFFFFFFF)

	s.healthPool.Each(func(e ecs.Entity, hp *component.HealthComponent) {
		if hp.HP <= 0 {
			return
		}
		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}

		dx := pos.X - targetX
		dy := pos.Y - targetY
		distSq := dx*dx + dy*dy

		faction := component.FactionPlayer
		if s.ownerPool != nil {
			if owner, ok := s.ownerPool.Get(e); ok {
				faction = owner.Faction
			}
		}

		if distSq < bestDist {
			bestDist = distSq
			bestFaction = faction
		}
	})

	// Convert faction (0=player, 1=enemy) to holder (1=player, 2=enemy)
	holder := uint8(0)
	if bestFaction != 255 {
		holder = bestFaction + 1
	}

	if holder == s.captureHolder && holder != 0 {
		s.gm.Objective.HoldCounter++
		if s.gm.Objective.HoldCounter >= s.gm.Objective.HoldTarget {
			s.result = &MatchResult{Winner: bestFaction, Reason: "capture"}
		}
	} else {
		s.captureHolder = holder
		s.gm.Objective.HoldCounter = 1
	}
}

func (s *ObjectiveSystem) checkSurvival(tick uint32) {
	// Check if enemy is eliminated first
	s.checkElimination()
	if s.result != nil {
		return
	}

	// Check timer
	if s.gm.Objective.Duration > 0 && tick >= uint32(s.gm.Objective.Duration) {
		s.result = &MatchResult{Winner: component.FactionPlayer, Reason: "survival"}
	}
}
