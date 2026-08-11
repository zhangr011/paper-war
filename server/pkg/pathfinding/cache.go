package pathfinding

import (
	"container/list"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type cacheKey struct {
	X, Y         int32
	ProfileID    uint8
	CreepFaction uint8
}

type cacheEntry struct {
	key  cacheKey
	ff   *FlowField
	elem *list.Element
}

type Cache struct {
	gm      *tilemap.GameMap
	maxSize int
	entries map[cacheKey]*cacheEntry
	order   *list.List
}

func NewCache(gm *tilemap.GameMap, maxSize int) *Cache {
	return &Cache{
		gm:      gm,
		maxSize: maxSize,
		entries: make(map[cacheKey]*cacheEntry),
		order:   list.New(),
	}
}

func (c *Cache) Size() int { return len(c.entries) }

// Get returns the cached flow field for (target, profile, creepFaction), or
// computes + caches it. creepFaction keys the cache because the friendly-creep
// discount changes the integrated costs, so each faction gets its own field
// for the same target/profile (Phase 4). creepFaction=0 is the neutral field
// used by validators/tests.
func (c *Cache) Get(targetX, targetY int32, profile *component.MovementProfile, creepFaction uint8) *FlowField {
	key := cacheKey{X: targetX, Y: targetY, ProfileID: profile.ID, CreepFaction: creepFaction}
	if entry, ok := c.entries[key]; ok {
		c.order.MoveToFront(entry.elem)
		return entry.ff
	}

	ff := Compute(c.gm, targetX, targetY, profile, creepFaction)
	elem := c.order.PushFront(key)
	c.entries[key] = &cacheEntry{key: key, ff: ff, elem: elem}

	for len(c.entries) > c.maxSize {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		oldKey := c.order.Remove(oldest).(cacheKey)
		delete(c.entries, oldKey)
	}

	return ff
}

func (c *Cache) Invalidate(targetX, targetY int32, profile *component.MovementProfile) {
	// Invalidate all creep-faction variants for this (target, profile) since
	// creep ownership changes invalidate both factions' fields.
	for _, cf := range [3]uint8{0, 1, 2} {
		key := cacheKey{X: targetX, Y: targetY, ProfileID: profile.ID, CreepFaction: cf}
		if entry, ok := c.entries[key]; ok {
			c.order.Remove(entry.elem)
			delete(c.entries, key)
		}
	}
}

func (c *Cache) InvalidateAll() {
	c.entries = make(map[cacheKey]*cacheEntry)
	c.order.Init()
}