package game

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
)

// TestSpawnedCombatUnitHasCollisionComponent is the sharp regression for the
// "collision not worked" root cause. SpawnSquadWithType (and every other spawn
// path) attaches CollisionComponent via gs.addComponent. addComponent routes
// through a type switch over component-pool types; if that switch has no case
// for CollisionComponent, the add is silently dropped — the collisionPool stays
// empty, CollisionSystem.Tick collects zero entries, and NO unit ever collides
// (the isolated pkg/collision tests still pass because they use colPool.Add
// directly, bypassing addComponent). This test spawns through the real path and
// asserts the component actually lands in the pool.
func TestSpawnedCombatUnitHasCollisionComponent(t *testing.T) {
	gs := NewGameSession()
	colPool := gs.World.Pool(component.CollisionComponent{}).(*ecs.ComponentPool[component.CollisionComponent])

	// 1 commander + 3 combat units = 4 CollisionComponents must land in the pool.
	gs.SpawnSquadWithType(
		1, 1, fixed.FromFloat(10.0), fixed.FromFloat(10.0), 3, component.UnitLightInfantry)

	if got := colPool.Len(); got < 4 {
		t.Fatalf("CollisionComponent dropped by addComponent: pool has %d, want >=4 (1 commander + 3 units) — collision is a no-op without these", got)
	}
}
