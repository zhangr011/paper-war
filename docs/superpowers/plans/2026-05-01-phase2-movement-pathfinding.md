# Phase 2: Movement & Pathfinding — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the tilemap, Flow Field pathfinding, Boid flocking forces, formation system, and movement system for squad-based group movement.

**Architecture:** ECS systems operate on component pools registered in World. TileMap provides terrain data. FlowFieldCache computes and caches per-profile direction fields. BoidSystem computes per-unit forces. MovementSystem composes all forces and updates positions each tick.

**Tech Stack:** Go, pkg/fixed (int64 12-bit), pkg/ecs, pkg/spatial

**Spec reference:** `docs/superpowers/specs/2026-05-01-paper-war-rts-design.md` Sections 4-5

---

## File Structure

```
server/pkg/
  component/
    position.go       # PositionComponent, VelocityComponent
    movement.go       # MovementComponent, MovementProfile, TerrainType constants
    boid.go           # BoidComponent, BoidRole constants
    formation.go      # FormationComponent, FormationRoleComponent, FormationType constants
    commander.go      # CommanderComponent, TacticalState constants
    pathfinding.go    # PathfindingComponent
  tilemap/
    tilemap.go        # GameMap, Tile struct, NewTestMap helper
    tilemap_test.go
  pathfinding/
    flowfield.go      # FlowField struct, Compute, GetDirection
    cache.go          # FlowFieldCache (LRU, ProfileID key)
    pathfinding_test.go
  boid/
    forces.go         # SeparationForce, CohesionForce, AlignmentForce
    boid.go           # BoidSystem (ECS System)
    boid_test.go
  formation/
    formation.go      # FormationSystem, CalcOffsets for each formation type
    formation_test.go
  movement/
    movement.go       # MovementSystem (ECS System: force composition + position update)
    movement_test.go
```

---

### Task 1: Game Component Types

**Files:**
- Create: `server/pkg/component/position.go`
- Create: `server/pkg/component/movement.go`
- Create: `server/pkg/component/boid.go`
- Create: `server/pkg/component/formation.go`
- Create: `server/pkg/component/commander.go`
- Create: `server/pkg/component/pathfinding.go`

- [ ] **Step 1: Create all component files**

```go
// server/pkg/component/position.go
package component

type PositionComponent struct {
	X, Y  int64
	Angle int16 // 0-3599, 0.1 degree precision
}

type VelocityComponent struct {
	Vx, Vy int64
	Speed  int64
}
```

```go
// server/pkg/component/movement.go
package component

type TerrainType uint8

const (
	TerrainPlain  TerrainType = 0
	TerrainRoad   TerrainType = 1
	TerrainShallow TerrainType = 2
	TerrainDeep   TerrainType = 3
	TerrainForest TerrainType = 4
	TerrainHill   TerrainType = 5
	TerrainSwamp  TerrainType = 6
	TerrainBridge TerrainType = 7
	TerrainWall   TerrainType = 8
	TerrainSnow   TerrainType = 9
	TerrainDesert TerrainType = 10
)

type MovementProfile struct {
	ID           uint8
	TerrainCosts [16]uint8 // 0=impassable, 1=normal, 2=slow, 3=very slow
}

type MovementComponent struct {
	ProfileID uint8
}
```

```go
// server/pkg/component/boid.go
package component

type BoidRole uint8

const (
	RoleMelee   BoidRole = 0
	RoleRanged  BoidRole = 1
	RoleFlanker BoidRole = 2
	RoleCommander BoidRole = 3
)

type BoidComponent struct {
	SquadID       uint32
	Role          BoidRole
	SeparationW   int64
	CohesionW     int64
	AlignmentW    int64
	FormationW    int64
	NeighborRange int64
}
```

```go
// server/pkg/component/formation.go
package component

type FormationType uint8

const (
	FormationLine    FormationType = 0
	FormationWedge   FormationType = 1
	FormationCircle  FormationType = 2
	FormationScatter FormationType = 3
)

type RoleOffset struct {
	Role BoidRole
	DX   int64
	DY   int64
}

type FormationComponent struct {
	FormationType FormationType
	Spacing       int64
	RoleOffsets   []RoleOffset
}

type FormationRoleComponent struct {
	OffsetX int64
	OffsetY int64
	Role    BoidRole
}
```

```go
// server/pkg/component/commander.go
package component

type TacticalState uint8

const (
	TacticalFollow  TacticalState = 0
	TacticalCharge  TacticalState = 1
	TacticalRetreat TacticalState = 2
	TacticalHold    TacticalState = 3
)

type CommanderComponent struct {
	SquadID         uint32
	AuraRadius      int64
	AuraMoraleBonus int32
	TacticalState   TacticalState
	IsAlive         bool
}
```

```go
// server/pkg/component/pathfinding.go
package component

type PathfindingComponent struct {
	TargetX    int64
	TargetY    int64
	FlowFieldID uint32
	Stuck      bool
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd server && go build ./pkg/component/`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add server/pkg/component/
git commit -m "feat: add Phase 2 game component types"
```

---

### Task 2: TileMap

**Files:**
- Create: `server/pkg/tilemap/tilemap.go`
- Create: `server/pkg/tilemap/tilemap_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/tilemap/tilemap_test.go
package tilemap

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestNewGameMap(t *testing.T) {
	gm := NewGameMap(4, 4)
	if gm.Width != 4 || gm.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", gm.Width, gm.Height)
	}
	// Default tiles should be plain with cost 1
	tile := gm.TileAt(0, 0)
	if tile.TerrainType != component.TerrainPlain {
		t.Errorf("default terrain = %d, want plain", tile.TerrainType)
	}
}

func TestSetTerrain(t *testing.T) {
	gm := NewGameMap(4, 4)
	gm.SetTerrain(1, 1, component.TerrainForest)
	tile := gm.TileAt(1, 1)
	if tile.TerrainType != component.TerrainForest {
		t.Errorf("terrain = %d, want forest", tile.TerrainType)
	}
}

func TestCostAt(t *testing.T) {
	profile := component.MovementProfile{ID: 0}
	profile.TerrainCosts[component.TerrainPlain] = 1
	profile.TerrainCosts[component.TerrainForest] = 2
	profile.TerrainCosts[component.TerrainDeep] = 0

	gm := NewGameMap(4, 4)
	gm.SetTerrain(2, 2, component.TerrainForest)
	gm.SetTerrain(3, 3, component.TerrainDeep)

	if gm.CostAt(0, 0, &profile) != 1 {
		t.Errorf("plain cost = %d, want 1", gm.CostAt(0, 0, &profile))
	}
	if gm.CostAt(2, 2, &profile) != 2 {
		t.Errorf("forest cost = %d, want 2", gm.CostAt(2, 2, &profile))
	}
	if gm.CostAt(3, 3, &profile) != 0 {
		t.Errorf("deep water cost = %d, want 0 (impassable)", gm.CostAt(3, 3, &profile))
	}
}

func TestOutOfBounds(t *testing.T) {
	gm := NewGameMap(4, 4)
	tile := gm.TileAt(-1, 0)
	if tile != nil {
		t.Error("out of bounds should return nil")
	}
}

func TestNewTestMap(t *testing.T) {
	// 5x5 map with a wall in the middle column (x=2)
	gm := NewTestMap(5, 5, func(x, y int32) component.TerrainType {
		if x == 2 {
			return component.TerrainWall
		}
		return component.TerrainPlain
	})
	if gm.CostAt(2, 2, defaultInfantryProfile()) != 0 {
		t.Error("wall should be impassable")
	}
	if gm.CostAt(0, 0, defaultInfantryProfile()) != 1 {
		t.Error("plain should be passable")
	}
}

func defaultInfantryProfile() *component.MovementProfile {
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	p.TerrainCosts[component.TerrainRoad] = 1
	p.TerrainCosts[component.TerrainShallow] = 2
	p.TerrainCosts[component.TerrainForest] = 2
	p.TerrainCosts[component.TerrainHill] = 2
	p.TerrainCosts[component.TerrainSwamp] = 3
	p.TerrainCosts[component.TerrainBridge] = 1
	// Deep, Wall = 0 (impassable)
	return p
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/tilemap/ -v`
Expected: Compilation error.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/tilemap/tilemap.go
package tilemap

import "github.com/user/paper-war/server/pkg/component"

type Tile struct {
	TerrainType component.TerrainType
	Elevation   int8
	BlockLOS    bool
	Health      int32
	MaxHealth   int32
}

type GameMap struct {
	Width, Height int32
	Tiles         []Tile
}

func NewGameMap(w, h int32) *GameMap {
	tiles := make([]Tile, w*h)
	for i := range tiles {
		tiles[i] = Tile{TerrainType: component.TerrainPlain}
	}
	return &GameMap{Width: w, Height: h, Tiles: tiles}
}

func NewTestMap(w, h int32, fn func(x, y int32) component.TerrainType) *GameMap {
	gm := NewGameMap(w, h)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			gm.SetTerrain(x, y, fn(x, y))
		}
	}
	return gm
}

func (m *GameMap) index(x, y int32) int {
	return int(y*m.Width + x)
}

func (m *GameMap) inBounds(x, y int32) bool {
	return x >= 0 && x < m.Width && y >= 0 && y < m.Height
}

func (m *GameMap) TileAt(x, y int32) *Tile {
	if !m.inBounds(x, y) {
		return nil
	}
	return &m.Tiles[m.index(x, y)]
}

func (m *GameMap) SetTerrain(x, y int32, tt component.TerrainType) {
	if !m.inBounds(x, y) {
		return
	}
	m.Tiles[m.index(x, y)].TerrainType = tt
}

func (m *GameMap) CostAt(x, y int32, profile *component.MovementProfile) uint8 {
	if !m.inBounds(x, y) {
		return 0
	}
	tt := m.Tiles[m.index(x, y)].TerrainType
	return profile.TerrainCosts[tt]
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/tilemap/ -v`
Expected: All 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/tilemap/
git commit -m "feat: add TileMap with terrain and movement cost lookup"
```

---

### Task 3: Flow Field Computation

**Files:**
- Create: `server/pkg/pathfinding/flowfield.go`
- Create: `server/pkg/pathfinding/flowfield_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/pathfinding/flowfield_test.go
package pathfinding

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestFlowFieldOpenPlane(t *testing.T) {
	// 5x5 all plains, target at (2,2)
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	ff := Compute(gm, 2, 2, profile)

	// Direction at (0,0) should point toward (2,2) — roughly (+1, +1)
	dir := ff.GetDirection(0, 0)
	if dir.DX <= 0 || dir.DY <= 0 {
		t.Errorf("direction from (0,0) to (2,2) = (%d,%d), want positive", dir.DX, dir.DY)
	}
}

func TestFlowFieldWallBypass(t *testing.T) {
	// 5x5 with wall at x=2 (full column), target at (4,2)
	// Units at x<2 must go around the wall
	gm := tilemap.NewTestMap(5, 5, func(x, y int32) component.TerrainType {
		if x == 2 {
			return component.TerrainWall
		}
		return component.TerrainPlain
	})
	profile := testInfantryProfile()
	ff := Compute(gm, 4, 2, profile)

	// Direction at (1,2) should not point directly right (blocked by wall)
	dir := ff.GetDirection(1, 2)
	if dir.DX > 0 && dir.DY == 0 {
		t.Errorf("(1,2) direction points straight right into wall, should bypass")
	}
}

func TestFlowFieldTargetCell(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	ff := Compute(gm, 2, 2, profile)

	dir := ff.GetDirection(2, 2)
	// Target cell should have zero or near-zero direction
	if dir.DX > fixed.FromFloat(0.1) || dir.DY > fixed.FromFloat(0.1) {
		t.Errorf("target cell direction should be near zero, got (%d,%d)", dir.DX, dir.DY)
	}
}

func TestFlowFieldImpassable(t *testing.T) {
	// Target on impassable terrain — flow field should still compute,
	// but target cell itself is unreachable
	gm := tilemap.NewGameMap(3, 3)
	gm.SetTerrain(1, 1, component.TerrainDeep)
	profile := testInfantryProfile()
	ff := Compute(gm, 1, 1, profile)

	// All cells should still have directions (even if path goes near impassable)
	dir := ff.GetDirection(0, 0)
	_ = dir // just ensure it doesn't panic
}

func testInfantryProfile() *component.MovementProfile {
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	p.TerrainCosts[component.TerrainRoad] = 1
	p.TerrainCosts[component.TerrainShallow] = 2
	p.TerrainCosts[component.TerrainForest] = 2
	p.TerrainCosts[component.TerrainBridge] = 1
	return p
}
```

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/pathfinding/flowfield.go
package pathfinding

import (
	"container/heap"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type Direction struct {
	DX, DY int64 // fixed-point unit vector
}

type FlowField struct {
	Width, Height int32
	Directions    []Direction // one per cell
	Costs         []uint32    // integration costs (MaxUint32 = unreachable)
}

func (f *FlowField) GetDirection(x, y int32) Direction {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return Direction{}
	}
	return f.Directions[y*f.Width+x]
}

func Compute(gm *tilemap.GameMap, targetX, targetY int32, profile *component.MovementProfile) *FlowField {
	w, h := gm.Width, gm.Height
	size := int(w * h)

	costs := make([]uint32, size)
	for i := range costs {
		costs[i] = ^uint32(0) // MaxUint32 = unreachable
	}

	// Dijkstra from target
	type node struct {
		x, y int32
		cost uint32
	}
	pq := &priorityQueue{}
	heap.Init(pq)

	// Set target cell cost to 0 (even if terrain is impassable, target is reachable by definition)
	idx := targetY*w + targetX
	costs[idx] = 0
	heap.Push(pq, &pqItem{x: targetX, y: targetY, cost: 0})

	// 4-directional neighbors
	dirs := [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*pqItem)
		if cur.cost > costs[cur.y*w+cur.x] {
			continue
		}
		for _, d := range dirs {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx < 0 || nx >= w || ny < 0 || ny >= h {
				continue
			}
			mc := gm.CostAt(nx, ny, profile)
			if mc == 0 {
				continue // impassable
			}
			newCost := cur.cost + uint32(mc)
			ni := ny*w + nx
			if newCost < costs[ni] {
				costs[ni] = newCost
				heap.Push(pq, &pqItem{x: nx, y: ny, cost: newCost})
			}
		}
	}

	// Build direction field: each cell points to neighbor with lowest cost
	dirs8 := [8][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	directions := make([]Direction, size)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			i := y*w + x
			if costs[i] == ^uint32(0) {
				continue // unreachable, direction stays zero
			}
			if x == targetX && y == targetY {
				continue // target cell, zero direction
			}
			bestCost := costs[i]
			bestDX, bestDY := int32(0), int32(0)
			for _, d := range dirs8 {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || nx >= w || ny < 0 || ny >= h {
					continue
				}
				ni := ny*w + nx
				if costs[ni] < bestCost {
					bestCost = costs[ni]
					bestDX = d[0]
					bestDY = d[1]
				}
			}
			if bestDX != 0 || bestDY != 0 {
				// Normalize to fixed-point unit vector
				length := fixed.ISqrt(fixed.FromFloat(float64(bestDX*bestDX + bestDY*bestDY)))
				if length > 0 {
					directions[i] = Direction{
						DX: fixed.Div(fixed.FromFloat(float64(bestDX)), length),
						DY: fixed.Div(fixed.FromFloat(float64(bestDY)), length),
					}
				}
			}
		}
	}

	return &FlowField{Width: w, Height: h, Directions: directions, Costs: costs}
}

// Priority queue for Dijkstra
type pqItem struct {
	x, y int32
	cost uint32
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].cost < pq[j].cost }
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*pqItem)) }
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/pathfinding/ -v`
Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/pathfinding/
git commit -m "feat: add Flow Field pathfinding with Dijkstra integration"
```

---

### Task 4: Flow Field Cache

**Files:**
- Create: `server/pkg/pathfinding/cache.go`
- Modify: `server/pkg/pathfinding/flowfield_test.go` (append)

- [ ] **Step 1: Write the failing tests**

Append to `server/pkg/pathfinding/flowfield_test.go`:

```go
func TestFlowFieldCache(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 10)

	ff1 := cache.Get(2, 2, profile)
	if ff1 == nil {
		t.Fatal("Get should return a flow field")
	}

	// Same key should return cached version
	ff2 := cache.Get(2, 2, profile)
	if ff2 != ff1 {
		t.Error("second Get should return same cached flow field")
	}

	// Different target should compute new one
	ff3 := cache.Get(0, 0, profile)
	if ff3 == ff1 {
		t.Error("different target should return different flow field")
	}
}

func TestFlowFieldCacheEviction(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 2) // max 2 entries

	cache.Get(0, 0, profile)
	cache.Get(1, 1, profile)
	cache.Get(2, 2, profile) // should evict oldest

	if cache.Size() > 2 {
		t.Errorf("cache size = %d, want <= 2", cache.Size())
	}
}

func TestFlowFieldCacheInvalidate(t *testing.T) {
	gm := tilemap.NewGameMap(5, 5)
	profile := testInfantryProfile()
	cache := NewCache(gm, 10)

	ff1 := cache.Get(2, 2, profile)
	cache.Invalidate(2, 2, profile) // remove specific entry
	ff2 := cache.Get(2, 2, profile)
	if ff2 == ff1 {
		t.Error("after Invalidate, should recompute")
	}
}
```

- [ ] **Step 2: Write the implementation**

Create `server/pkg/pathfinding/cache.go`:

```go
package pathfinding

import (
	"container/list"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type cacheKey struct {
	X, Y      int32
	ProfileID uint8
}

type cacheEntry struct {
	key  cacheKey
	ff   *FlowField
	elem *list.Element
}

type Cache struct {
	gm       *tilemap.GameMap
	maxSize  int
	entries  map[cacheKey]*cacheEntry
	order    *list.List // front = most recent
}

func NewCache(gm *tilemap.GameMap, maxSize int) *Cache {
	return &Cache{
		gm:      gm,
		maxSize: maxSize,
		entries: make(map[cacheKey]*cacheKey),
		order:   list.New(),
	}
}

func (c *Cache) Size() int { return len(c.entries) }

func (c *Cache) Get(targetX, targetY int32, profile *component.MovementProfile) *FlowField {
	key := cacheKey{X: targetX, Y: targetY, ProfileID: profile.ID}
	if entry, ok := c.entries[key]; ok {
		c.order.MoveToFront(entry.elem)
		return entry.ff
	}

	ff := Compute(c.gm, targetX, targetY, profile)

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
	key := cacheKey{X: targetX, Y: targetY, ProfileID: profile.ID}
	if entry, ok := c.entries[key]; ok {
		c.order.Remove(entry.elem)
		delete(c.entries, key)
	}
}

func (c *Cache) InvalidateAll() {
	c.entries = make(map[cacheKey]*cacheEntry)
	c.order.Init()
}
```

- [ ] **Step 3: Fix compilation error — `c.entries` map value type**

The `NewCache` function initializes `c.entries` with wrong value type. Fix:

```go
func NewCache(gm *tilemap.GameMap, maxSize int) *Cache {
	return &Cache{
		gm:      gm,
		maxSize: maxSize,
		entries: make(map[cacheKey]*cacheEntry),
		order:   list.New(),
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/pathfinding/ -v`
Expected: All 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/pathfinding/
git commit -m "feat: add Flow Field cache with LRU eviction"
```

---

### Task 5: Boid Forces

**Files:**
- Create: `server/pkg/boid/forces.go`
- Create: `server/pkg/boid/forces_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/boid/forces_test.go
package boid

import (
	"testing"

	"github.com/user/paper-war/server/pkg/fixed"
)

func TestSeparationForce(t *testing.T) {
	// Two units close together: force pushes them apart
	self := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	neighbors := [][2]int64{
		{fixed.FromFloat(6.0), fixed.FromFloat(5.0)}, // 1 unit to the right
	}
	fx, fy := SeparationForce(self, neighbors, fixed.FromFloat(3.0))
	// Should push left (negative X)
	if fx >= 0 {
		t.Errorf("separation should push away from neighbor, fx=%d", fx)
	}
}

func TestCohesionForce(t *testing.T) {
	// Unit at (0,0), neighbors at (10,10) → cohesion pulls toward center
	self := [2]int64{0, 0}
	neighbors := [][2]int64{
		{fixed.FromFloat(10.0), fixed.FromFloat(10.0)},
	}
	fx, fy := CohesionForce(self, neighbors)
	// Should pull toward positive X and Y
	if fx <= 0 || fy <= 0 {
		t.Errorf("cohesion should pull toward neighbor center, fx=%d fy=%d", fx, fy)
	}
}

func TestAlignmentForce(t *testing.T) {
	// Self going right, neighbors going up → alignment steers toward up
	selfVel := [2]int64{fixed.FromFloat(1.0), 0}
	neighborVels := [][2]int64{
		{0, fixed.FromFloat(1.0)},
	}
	fx, fy := AlignmentForce(selfVel, neighborVels)
	// Should steer upward (positive Y)
	if fy <= 0 {
		t.Errorf("alignment should steer toward neighbor velocity, fy=%d", fy)
	}
}

func TestSeparationNoNeighbors(t *testing.T) {
	self := [2]int64{fixed.FromFloat(5.0), fixed.FromFloat(5.0)}
	fx, fy := SeparationForce(self, nil, fixed.FromFloat(3.0))
	if fx != 0 || fy != 0 {
		t.Errorf("no neighbors = zero force, got (%d,%d)", fx, fy)
	}
}
```

- [ ] **Step 2: Write the implementation**

```go
// server/pkg/boid/forces.go
package boid

import "github.com/user/paper-war/server/pkg/fixed"

// SeparationForce pushes away from nearby neighbors to avoid overlap.
func SeparationForce(self [2]int64, neighbors [][2]int64, range_ int64) (fx, fy int64) {
	for _, n := range neighbors {
		dx := self[0] - n[0]
		dy := self[1] - n[1]
		distSq := fixed.DistSq(dx, dy)
		if distSq <= 0 {
			continue
		}
		dist := fixed.ISqrt(distSq)
		if dist > range_ {
			continue
		}
		// Strength inversely proportional to distance
		strength := fixed.Div(range_-dist, dist)
		fx += fixed.Mul(dx, strength)
		fy += fixed.Mul(dy, strength)
	}
	return
}

// CohesionForce pulls toward the center of mass of neighbors.
func CohesionForce(self [2]int64, neighbors [][2]int64) (fx, fy int64) {
	if len(neighbors) == 0 {
		return 0, 0
	}
	var cx, cy int64
	for _, n := range neighbors {
		cx += n[0]
		cy += n[1]
	}
	cx = cx / int64(len(neighbors))
	cy = cy / int64(len(neighbors))
	// Direction toward center, scaled down
	fx = (cx - self[0]) >> 4 // divide by 16 for gentle pull
	fy = (cy - self[1]) >> 4
	return
}

// AlignmentForce steers toward the average velocity of neighbors.
func AlignmentForce(selfVel [2]int64, neighborVels [][2]int64) (fx, fy int64) {
	if len(neighborVels) == 0 {
		return 0, 0
	}
	var avgVx, avgVy int64
	for _, v := range neighborVels {
		avgVx += v[0]
		avgVy += v[1]
	}
	avgVx /= int64(len(neighborVels))
	avgVy /= int64(len(neighborVels))
	// Steer toward average velocity
	fx = (avgVx - selfVel[0]) >> 3 // divide by 8 for gentle steer
	fy = (avgVy - selfVel[1]) >> 3
	return
}
```

- [ ] **Step 3: Run tests**

Run: `cd server && go test ./pkg/boid/ -v`
Expected: All 4 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/boid/
git commit -m "feat: add Boid flocking force calculations"
```

---

### Task 6: Formation System

**Files:**
- Create: `server/pkg/formation/formation.go`
- Create: `server/pkg/formation/formation_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/formation/formation_test.go
package formation

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

func TestLineFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationLine, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleMelee, component.RoleRanged, component.RoleRanged,
	})
	if len(offsets) != 4 {
		t.Fatalf("expected 4 offsets, got %d", len(offsets))
	}
	// Line formation: melee in front (negative Y), ranged behind (positive Y)
	// All spread along X axis
	if offsets[0].DY >= 0 {
		t.Error("melee should be in front (negative Y)")
	}
}

func TestWedgeFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationWedge, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleMelee, component.RoleRanged,
	})
	if len(offsets) != 3 {
		t.Fatalf("expected 3 offsets, got %d", len(offsets))
	}
	// First unit (index 0) should be at tip (most negative Y)
	if offsets[0].DY >= offsets[1].DY {
		t.Error("wedge tip should have most negative Y")
	}
}

func TestCircleFormation(t *testing.T) {
	offsets := CalcOffsets(component.FormationCircle, fixed.FromFloat(2.0), []component.BoidRole{
		component.RoleMelee, component.RoleRanged, component.RoleMelee, component.RoleRanged,
	})
	if len(offsets) != 4 {
		t.Fatalf("expected 4 offsets, got %d", len(offsets))
	}
	// Melee should be closer to center (smaller radius) than ranged
	meleeDist := abs(offsets[0].DX) + abs(offsets[0].DY)
	rangedDist := abs(offsets[1].DX) + abs(offsets[1].DY)
	if meleeDist >= rangedDist {
		t.Error("melee should be closer to center than ranged in circle formation")
	}
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
```

- [ ] **Step 2: Write the implementation**

```go
// server/pkg/formation/formation.go
package formation

import (
	"math"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
)

type Offset struct {
	DX, DY int64
	Role   component.BoidRole
}

func CalcOffsets(ft component.FormationType, spacing int64, roles []component.BoidRole) []Offset {
	switch ft {
	case component.FormationLine:
		return lineFormation(spacing, roles)
	case component.FormationWedge:
		return wedgeFormation(spacing, roles)
	case component.FormationCircle:
		return circleFormation(spacing, roles)
	case component.FormationScatter:
		return scatterFormation(spacing, roles)
	default:
		return lineFormation(spacing, roles)
	}
}

func lineFormation(spacing int64, roles []component.BoidRole) []Offset {
	offsets := make([]Offset, len(roles))
	meleeCount := 0
	rangedCount := 0
	for _, r := range roles {
		if r == component.RoleMelee || r == component.RoleFlanker {
			meleeCount++
		} else {
			rangedCount++
		}
	}

	meleeIdx := 0
	rangedIdx := 0
	for i, r := range roles {
		if r == component.RoleMelee || r == component.RoleFlanker {
			// Front row, spread along X
			x := int64(meleeIdx) * spacing
			if meleeCount > 1 {
				x -= spacing * int64(meleeCount-1) / 2
			}
			offsets[i] = Offset{DX: x, DY: -spacing, Role: r}
			meleeIdx++
		} else {
			// Back row
			x := int64(rangedIdx) * spacing
			if rangedCount > 1 {
				x -= spacing * int64(rangedCount-1) / 2
			}
			offsets[i] = Offset{DX: x, DY: spacing, Role: r}
			rangedIdx++
		}
	}
	return offsets
}

func wedgeFormation(spacing int64, roles []component.BoidRole) []Offset {
	offsets := make([]Offset, len(roles))
	for i := range roles {
		row := int64(i)
		// Tip at (0, -row*spacing), spread wider each row
		offsets[i] = Offset{
			DX:   spacing * row / 2, // approximate spread
			DY:   -spacing * row,
			Role: roles[i],
		}
	}
	// Center the wedge
	if len(roles) > 1 {
		for i := range offsets {
			offsets[i].DX -= spacing * int64(len(roles)-1) / 4
		}
	}
	return offsets
}

func circleFormation(spacing int64, roles []component.BoidRole) []Offset {
	n := len(roles)
	offsets := make([]Offset, n)
	melee := 0
	ranged := 0
	for i, r := range roles {
		var radius int64
		var idx int
		if r == component.RoleMelee || r == component.RoleFlanker {
			radius = spacing
			idx = melee
			melee++
		} else {
			radius = spacing * 2
			idx = ranged
			ranged++
		}
		var count int
		if r == component.RoleMelee || r == component.RoleFlanker {
			count = melee
		} else {
			count = ranged
		}
		if count == 0 {
			count = 1
		}
		angle := 2 * math.Pi * float64(idx) / float64(count)
		offsets[i] = Offset{
			DX:   fixed.FromFloat(math.Cos(angle) * float64(radius)),
			DY:   fixed.FromFloat(math.Sin(angle) * float64(radius)),
			Role: r,
		}
	}
	return offsets
}

func scatterFormation(spacing int64, roles []component.BoidRole) []Offset {
	// Scatter = wide spread, high separation
	offsets := make([]Offset, len(roles))
	for i := range roles {
		angle := 2 * math.Pi * float64(i) / float64(len(roles))
		r := float64(spacing * 3)
		offsets[i] = Offset{
			DX:   fixed.FromFloat(math.Cos(angle) * r),
			DY:   fixed.FromFloat(math.Sin(angle) * r),
			Role: roles[i],
		}
	}
	return offsets
}
```

- [ ] **Step 3: Run tests**

Run: `cd server && go test ./pkg/formation/ -v`
Expected: All 3 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/formation/
git commit -m "feat: add formation system with line/wedge/circle/scatter"
```

---

### Task 7: MovementSystem (ECS System)

**Files:**
- Create: `server/pkg/movement/movement.go`
- Create: `server/pkg/movement/movement_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/movement/movement_test.go
package movement

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestMovementSystemMovesTowardTarget(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(10, 10)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(2.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		gm:     gm,
		cache:  cache,
		sh:     sh,
		profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	// Create a unit at (1,1), target (8,8)
	e := em.Create()
	posPool.Add(e, component.PositionComponent{X: fixed.FromFloat(1.0), Y: fixed.FromFloat(1.0)})
	velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
	boidPool.Add(e, component.BoidComponent{
		SquadID: 1, Role: component.RoleMelee,
		SeparationW: fixed.FromFloat(1.5),
		CohesionW:   fixed.FromFloat(1.0),
		AlignmentW:  fixed.FromFloat(1.0),
		FormationW:  fixed.FromFloat(2.0),
		NeighborRange: fixed.FromFloat(3.0),
	})
	movePool.Add(e, component.MovementComponent{ProfileID: 0})
	pathPool.Add(e, component.PathfindingComponent{
		TargetX: fixed.FromFloat(8.0), TargetY: fixed.FromFloat(8.0),
	})

	// Run a few ticks
	for tick := uint32(1); tick <= 5; tick++ {
		w.Tick(tick)
	}

	// Unit should have moved toward (8,8)
	pos, _ := posPool.Get(e)
	if pos.X <= fixed.FromFloat(1.0) || pos.Y <= fixed.FromFloat(1.0) {
		t.Errorf("unit didn't move toward target: (%v, %v)", fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y))
	}
}

func testProfile() *component.MovementProfile {
	p := &component.MovementProfile{ID: 0}
	p.TerrainCosts[component.TerrainPlain] = 1
	return p
}
```

- [ ] **Step 2: Write the implementation**

```go
// server/pkg/movement/movement.go
package movement

import (
	"github.com/user/paper-war/server/pkg/boid"
	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

type MovementSystem struct {
	gm       *tilemap.GameMap
	cache    *pathfinding.Cache
	sh       *spatial.Hash
	profiles map[uint8]*component.MovementProfile

	posPool  *ecs.ComponentPool[component.PositionComponent]
	velPool  *ecs.ComponentPool[component.VelocityComponent]
	boidPool *ecs.ComponentPool[component.BoidComponent]
	movePool *ecs.ComponentPool[component.MovementComponent]
	pathPool *ecs.ComponentPool[component.PathfindingComponent]
}

func (s *MovementSystem) Name() string  { return "MovementSystem" }
func (s *MovementSystem) Priority() int { return 60 }

func (s *MovementSystem) Init(w *ecs.World) {
	s.posPool = w.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
	s.velPool = w.Pool(component.VelocityComponent{}).(*ecs.ComponentPool[component.VelocityComponent])
	s.boidPool = w.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
	s.movePool = w.Pool(component.MovementComponent{}).(*ecs.ComponentPool[component.MovementComponent])
	s.pathPool = w.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
}

func (s *MovementSystem) Tick(w *ecs.World, tick uint32) {
	// Rebuild spatial hash with current positions
	s.sh.Clear()
	s.posPool.Each(func(e ecs.Entity, pos *component.PositionComponent) {
		s.sh.Insert(uint64(e), pos.X, pos.Y)
	})

	// For each unit with boid + pathfinding, compute forces and update position
	s.boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
		pos, ok := s.posPool.Get(e)
		if !ok {
			return
		}
		vel, hasVel := s.velPool.Get(e)

		// 1. Flow field force
		var flowFX, flowFY int64
		if path, ok := s.pathPool.Get(e); ok {
			profile := s.profiles[0] // default
			if mc, ok := s.movePool.Get(e); ok {
				if p, exists := s.profiles[mc.ProfileID]; exists {
					profile = p
				}
			}
			tileX := int32(pos.X >> 12)
			tileY := int32(pos.Y >> 12)
			ff := s.cache.Get(int32(path.TargetX>>12), int32(path.TargetY>>12), profile)
			dir := ff.GetDirection(tileX, tileY)
			flowW := fixed.FromFloat(2.5)
			flowFX = fixed.Mul(dir.DX, flowW)
			flowFY = fixed.Mul(dir.DY, flowW)
		}

		// 2. Boid forces (find neighbors)
		neighborPositions := s.queryNeighborPositions(pos.X, pos.Y, bc.NeighborRange, uint64(e))
		sepFX, sepFY := boid.SeparationForce(
			[2]int64{pos.X, pos.Y}, neighborPositions, bc.NeighborRange,
		)
		cohFX, cohFY := boid.CohesionForce([2]int64{pos.X, pos.Y}, neighborPositions)

		var aliFX, aliFY int64
		if hasVel {
			neighborVels := s.queryNeighborVelocities(pos.X, pos.Y, bc.NeighborRange, uint64(e))
			aliFX, aliFY = boid.AlignmentForce([2]int64{vel.Vx, vel.Vy}, neighborVels)
		}

		// 3. Compose total force
		totalFX := flowFX +
			fixed.Mul(sepFX, bc.SeparationW) +
			fixed.Mul(cohFX, bc.CohesionW) +
			fixed.Mul(aliFX, bc.AlignmentW)
		totalFY := flowFY +
			fixed.Mul(sepFY, bc.SeparationW) +
			fixed.Mul(cohFY, bc.CohesionW) +
			fixed.Mul(aliFY, bc.AlignmentW)

		// 4. Clamp force
		maxForce := fixed.FromFloat(5.0)
		totalFX = fixed.Clamp(totalFX, -maxForce, maxForce)
		totalFY = fixed.Clamp(totalFY, -maxForce, maxForce)

		// 5. Update velocity and position
		if hasVel {
			speed := vel.Speed
			vel.Vx = fixed.Clamp(totalFX, -speed, speed)
			vel.Vy = fixed.Clamp(totalFY, -speed, speed)
			pos.X += vel.Vx
			pos.Y += vel.Vy
		} else {
			pos.X += totalFX
			pos.Y += totalFY
		}
	})
}

func (s *MovementSystem) queryNeighborPositions(x, y, range_ int64, exclude uint64) [][2]int64 {
	ids := s.sh.Query(x, y, range_)
	var result [][2]int64
	for _, id := range ids {
		if id == exclude {
			continue
		}
		if pos, ok := s.posPool.Get(ecs.Entity(id)); ok {
			result = append(result, [2]int64{pos.X, pos.Y})
		}
	}
	return result
}

func (s *MovementSystem) queryNeighborVelocities(x, y, range_ int64, exclude uint64) [][2]int64 {
	ids := s.sh.Query(x, y, range_)
	var result [][2]int64
	for _, id := range ids {
		if id == exclude {
			continue
		}
		if vel, ok := s.velPool.Get(ecs.Entity(id)); ok {
			result = append(result, [2]int64{vel.Vx, vel.Vy})
		}
	}
	return result
}
```

- [ ] **Step 3: Run tests**

Run: `cd server && go test ./pkg/movement/ -v`
Expected: Test passes — unit moves toward target.

- [ ] **Step 4: Run full test suite**

Run: `cd server && go test ./... -count=1`
Expected: All packages pass.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/movement/
git commit -m "feat: add MovementSystem with boid forces + flow field"
```

---

### Task 8: Integration Test — Squad Movement

**Files:**
- Create: `server/pkg/movement/integration_test.go`

- [ ] **Step 1: Write the integration test**

```go
// server/pkg/movement/integration_test.go
package movement

import (
	"testing"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/formation"
	"github.com/user/paper-war/server/pkg/pathfinding"
	"github.com/user/paper-war/server/pkg/spatial"
	"github.com/user/paper-war/server/pkg/tilemap"
)

func TestSquadMovesTogether(t *testing.T) {
	em := ecs.NewEntityManager()
	w := ecs.NewWorld(em)

	gm := tilemap.NewGameMap(20, 20)
	profile := testProfile()
	cache := pathfinding.NewCache(gm, 10)
	sh := spatial.NewHash(fixed.FromFloat(3.0))

	posPool := ecs.NewComponentPool[component.PositionComponent]()
	velPool := ecs.NewComponentPool[component.VelocityComponent]()
	boidPool := ecs.NewComponentPool[component.BoidComponent]()
	movePool := ecs.NewComponentPool[component.MovementComponent]()
	pathPool := ecs.NewComponentPool[component.PathfindingComponent]()

	w.RegisterPool(component.PositionComponent{}, posPool)
	w.RegisterPool(component.VelocityComponent{}, velPool)
	w.RegisterPool(component.BoidComponent{}, boidPool)
	w.RegisterPool(component.MovementComponent{}, movePool)
	w.RegisterPool(component.PathfindingComponent{}, pathPool)

	ms := &MovementSystem{
		gm:       gm,
		cache:    cache,
		sh:       sh,
		profiles: map[uint8]*component.MovementProfile{0: profile},
	}
	w.AddSystem(ms)
	w.Init()

	// Create a squad of 4 units
	roles := []component.BoidRole{
		component.RoleMelee, component.RoleMelee,
		component.RoleRanged, component.RoleRanged,
	}
	offsets := formation.CalcOffsets(component.FormationLine, fixed.FromFloat(1.5), roles)
	squadCenterX := fixed.FromFloat(5.0)
	squadCenterY := fixed.FromFloat(5.0)

	for i := 0; i < 4; i++ {
		e := em.Create()
		posPool.Add(e, component.PositionComponent{
			X: squadCenterX + offsets[i].DX,
			Y: squadCenterY + offsets[i].DY,
		})
		velPool.Add(e, component.VelocityComponent{Speed: fixed.FromFloat(0.5)})
		boidPool.Add(e, component.BoidComponent{
			SquadID:       1,
			Role:          roles[i],
			SeparationW:   fixed.FromFloat(1.5),
			CohesionW:     fixed.FromFloat(1.0),
			AlignmentW:    fixed.FromFloat(1.0),
			FormationW:    fixed.FromFloat(2.0),
			NeighborRange: fixed.FromFloat(3.0),
		})
		movePool.Add(e, component.MovementComponent{ProfileID: 0})
		pathPool.Add(e, component.PathfindingComponent{
			TargetX: fixed.FromFloat(15.0),
			TargetY: fixed.FromFloat(15.0),
		})
	}

	// Run 10 ticks
	for tick := uint32(1); tick <= 10; tick++ {
		w.Tick(tick)
	}

	// All units should have moved toward target
	for i := 0; i < 4; i++ {
		e := ecs.Entity(uint64(i + 1))
		pos, ok := posPool.Get(e)
		if !ok {
			t.Errorf("unit %d not found", i)
			continue
		}
		if pos.X <= fixed.FromFloat(5.0)+offsets[i].DX {
			t.Errorf("unit %d didn't move: (%v, %v)", i, fixed.ToFloat(pos.X), fixed.ToFloat(pos.Y))
		}
	}

	// Units should maintain some cohesion (not spread too far apart)
	positions := make([][2]int64, 4)
	for i := 0; i < 4; i++ {
		pos, _ := posPool.Get(ecs.Entity(uint64(i + 1)))
		positions[i] = [2]int64{pos.X, pos.Y}
	}
	maxDist := int64(0)
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			dx := positions[i][0] - positions[j][0]
			dy := positions[i][1] - positions[j][1]
			dist := fixed.ISqrt(fixed.DistSq(dx, dy))
			if dist > maxDist {
				maxDist = dist
			}
		}
	}
	if fixed.ToFloat(maxDist) > 8.0 {
		t.Errorf("squad spread too far: max distance = %v", fixed.ToFloat(maxDist))
	}
}
```

- [ ] **Step 2: Run full test suite**

Run: `cd server && go test ./... -count=1`
Expected: All packages pass.

- [ ] **Step 3: Commit**

```bash
git add server/pkg/movement/integration_test.go
git commit -m "feat: add squad movement integration test"
```

---

## Self-Review

### Spec Coverage
- [x] Terrain types & TileMap (Spec 5.1) → Task 2
- [x] Movement profile cost matrix (Spec 5.2) → Task 2
- [x] Flow Field: Cost → Integration → Direction (Spec 5.3) → Task 3
- [x] Flow Field cache with ProfileID key (Spec 5.3) → Task 4
- [x] Boid forces: Separation, Cohesion, Alignment (Spec 4.1) → Task 5
- [x] Formation types: Line/Wedge/Circle/Scatter (Spec 4.3) → Task 6
- [x] MovementSystem force composition (Spec 5.5) → Task 7
- [x] Squad integration test → Task 8
- [ ] Commander "lead bird" model → deferred to Phase 3 (needs CommanderComponent + tactical AI)
- [ ] Dynamic terrain change + Flow Field recompute → deferred to Phase 3

### Placeholder scan: None found.

### Type consistency:
- component.PositionComponent used consistently across all packages
- fixed.FromFloat / fixed.ToFloat used consistently
- ecs.ComponentPool[T] typed access pattern consistent
