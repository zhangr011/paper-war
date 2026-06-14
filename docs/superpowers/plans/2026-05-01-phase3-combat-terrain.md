# Phase 3: Combat & Dynamic Terrain — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Build the combat system (instant attack + artillery projectiles), commander aura/tactical system, death handling, and dynamic terrain with Flow Field re-computation.

**Architecture:** CombatSystem uses Spatial Hash for range-based enemy detection, applies instant damage. CommanderSystem provides morale aura and handles commander death (squad AI fallback). TerrainSystem processes destructible objects and invalidates affected Flow Fields.

**Tech Stack:** Go, pkg/fixed, pkg/ecs, pkg/spatial, pkg/tilemap, pkg/pathfinding

**Spec reference:** `docs/superpowers/specs/2026-05-01-paper-war-rts-design.md` Sections 6, 4.2, 5.4

---

## File Structure

```
server/pkg/
  component/
    health.go        # HealthComponent, AttackComponent
  combat/
    combat.go        # CombatSystem (ECS System: auto-attack, damage)
    combat_test.go
  commander/
    commander.go     # CommanderSystem (aura, death → AI fallback)
    commander_test.go
  terrain/
    dynamic.go       # TerrainSystem (destructible objects, FF invalidation)
    dynamic_test.go
```

---

### Task 1: Health & Attack Components

**Files:**
- Create: `server/pkg/component/health.go`

```go
package component

type HealthComponent struct {
	HP, MaxHP int32
	Armor     int32
	Morale    int32
}

type AttackType uint8

const (
	AttackMelee     AttackType = 0
	AttackRanged    AttackType = 1
	AttackArtillery AttackType = 2
)

type AttackComponent struct {
	Range      int64
	Damage     int32
	Cooldown   uint8
	LastAttack uint32
	TargetID   uint32
	AttackType AttackType
}

type ProjectileComponent struct {
	X, Y       int64
	DX, DY     int64
	TargetX, TargetY int64
	Damage     int32
	ImpactTick uint32
	SplashRadius int64
}
```

Verify: `cd server && go build ./pkg/component/`
Commit: `git add server/pkg/component/health.go && git commit -m "feat: add health, attack, and projectile components"`

---

### Task 2: CombatSystem

**Files:**
- Create: `server/pkg/combat/combat.go`
- Create: `server/pkg/combat/combat_test.go`

CombatSystem logic:
- Each tick, for each entity with AttackComponent:
  - Skip if cooldown not ready (CurrentTick - LastAttack < Cooldown)
  - If TargetID set and target alive and in range → attack
  - If no target, use Spatial Hash to find nearest enemy in range → auto-acquire
  - For Melee/Ranged: instant damage (finalDamage = max(Damage - Armor, 1))
  - For Artillery: spawn Projectile entity

```go
// server/pkg/combat/combat.go
package combat

import (
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/spatial"
)

type CombatSystem struct {
	Sh       *spatial.Hash
	TickRate uint8

	posPool    *ecs.ComponentPool[component.PositionComponent]
	healthPool *ecs.ComponentPool[component.HealthComponent]
	attackPool *ecs.ComponentPool[component.AttackComponent]
	boidPool   *ecs.ComponentPool[component.BoidComponent]
}

func (s *CombatSystem) Name() string  { return "CombatSystem" }
func (s *CombatSystem) Priority() int { return 80 }

func (s *CombatSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.healthPool = w.Pool(component.HealthComponent{}).(*ecs.ComponentPool[component.HealthComponent])
	s.attackPool = w.Pool(component.AttackComponent{}).(*ecs.ComponentPool[component.AttackComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
}

func (s *CombatSystem) Tick(w *ecs.World, tick uint32) {
	s.attackPool.Each(func(e ecs.Entity, ac *component.AttackComponent) {
		// Cooldown check
		if ac.Cooldown > 0 && tick-ac.LastAttack < uint32(ac.Cooldown) {
			return
		}

		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}
		_, hasHealth := s.healthPool.Get(e)
		_ = hasHealth

		// If no target, auto-acquire nearest enemy in range
		if ac.TargetID == 0 {
			ids := s.Sh.Query(pos.X, pos.Y, ac.Range)
			selfID := uint64(e)
			for _, id := range ids {
				if id == selfID {
					continue
				}
				// Skip allies (same squad)
				if selfBoid, ok := s.boidPool.Get(e); ok {
					if otherBoid, ok := s.boidPool.Get(ecs.Entity(id)); ok {
						if selfBoid.SquadID == otherBoid.SquadID {
							continue
						}
					}
				}
				ac.TargetID = uint32(id)
				break
			}
		}

		if ac.TargetID == 0 {
			return
		}

		// Check target in range
		targetPos, ok := s.posPool.Get(ecs.Entity(ac.TargetID))
		if !ok {
			ac.TargetID = 0
			return
		}

		dx := targetPos.X - pos.X
		dy := targetPos.Y - pos.Y
		distSq := (dx*dx + dy*dy) >> 12
		rangeSq := (ac.Range * ac.Range) >> 12

		if distSq > rangeSq {
			ac.TargetID = 0 // out of range, drop target
			return
		}

		// Apply damage (instant for melee/ranged)
		if ac.AttackType != component.AttackArtillery {
			targetHealth, ok := s.healthPool.GetPtr(ecs.Entity(ac.TargetID))
			if !ok {
				ac.TargetID = 0
				return
			}
			dmg := ac.Damage
			if targetHealth.Armor > 0 {
				dmg -= targetHealth.Armor
			}
			if dmg < 1 {
				dmg = 1
			}
			targetHealth.HP -= dmg
			ac.LastAttack = tick
		}
		// Artillery would spawn projectile here (deferred)
	})
}
```

Test that a unit auto-acquires and damages an enemy in range.

---

### Task 3: CommanderSystem

**Files:**
- Create: `server/pkg/commander/commander.go`
- Create: `server/pkg/commander/commander_test.go`

CommanderSystem logic:
- Each tick, for each entity with CommanderComponent:
  - If IsAlive, apply morale bonus to nearby squad members within AuraRadius
  - If commander is dead (HP <= 0 and IsAlive was true):
    - Set IsAlive = false
    - Change all squad members' BoidComponent weights (increase Boid weights, decrease FormationW)
    - Squad falls back to self-defense AI

---

### Task 4: DeathSystem

**Files:**
- Create: `server/pkg/combat/death.go`
- Modify: `server/pkg/combat/combat_test.go` (append)

DeathSystem runs after CombatSystem (priority 90):
- Each tick, check all entities with HealthComponent
- If HP <= 0, mark for removal
- If entity also has CommanderComponent, trigger CommanderSystem death logic
- Remove entity components and destroy entity

---

### Task 5: TerrainSystem (Dynamic)

**Files:**
- Create: `server/pkg/terrain/dynamic.go`
- Create: `server/pkg/terrain/dynamic_test.go`

TerrainSystem logic:
- Process queued terrain change events (e.g., bridge destroyed)
- Update TileMap terrain
- Invalidate affected Flow Fields in Cache
- Broadcast terrain change event
