# Phase 1: Core Engine — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the foundational ECS framework, fixed-point math library, spatial hash, and game loop for the Paper War RTS server.

**Architecture:** Go server using a custom ECS (Entity-Component-System) pattern. All game math uses int64 fixed-point arithmetic (12-bit fraction) for cross-platform determinism. Systems execute in a fixed 15Hz game loop.

**Tech Stack:** Go 1.22+, module path `github.com/user/paper-war/server`

**Spec reference:** `docs/superpowers/specs/2026-05-01-paper-war-rts-design.md`

---

## File Structure

```
server/
├── go.mod
├── pkg/
│   ├── fixed/           # Fixed-point arithmetic
│   │   ├── fixed.go
│   │   └── fixed_test.go
│   ├── ecs/
│   │   ├── entity.go    # Entity ID manager
│   │   ├── pool.go      # Component storage (dense typed pools)
│   │   ├── system.go    # System interface & scheduler
│   │   ├── world.go     # World: ties entities + components + systems
│   │   └── ecs_test.go  # Integration tests
│   ├── spatial/
│   │   ├── hash.go      # Spatial hash grid
│   │   └── hash_test.go
│   └── game/
│       ├── loop.go      # 15Hz game loop
│       └── loop_test.go
```

---

### Task 1: Initialize Go Module & Project Structure

**Files:**
- Create: `server/go.mod`

- [ ] **Step 1: Create server directory and initialize Go module**

```bash
mkdir -p server/pkg/{fixed,ecs,spatial,game}
cd server
go mod init github.com/user/paper-war/server
```

- [ ] **Step 2: Verify module structure**

Run: `ls -R server/pkg/`
Expected: `fixed ecs spatial game` directories exist, `go.mod` contains module path.

- [ ] **Step 3: Commit**

```bash
git add server/
git commit -m "chore: initialize Go module and project structure"
```

---

### Task 2: Fixed-Point Arithmetic Library

**Files:**
- Create: `server/pkg/fixed/fixed.go`
- Create: `server/pkg/fixed/fixed_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/fixed/fixed_test.go
package fixed

import (
	"math"
	"testing"
)

func TestFractionBits(t *testing.T) {
	if FractionBits != 12 {
		t.Errorf("FractionBits = %d, want 12", FractionBits)
	}
	if One != 1<<12 {
		t.Errorf("One = %d, want %d", One, 1<<12)
	}
}

func TestFromFloat(t *testing.T) {
	tests := []struct {
		f    float64
		want int64
	}{
		{0.0, 0},
		{1.0, 4096},
		{2.0, 8192},
		{-1.0, -4096},
		{0.5, 2048},
		{100.5, 411648},
	}
	for _, tt := range tests {
		got := FromFloat(tt.f)
		if got != tt.want {
			t.Errorf("FromFloat(%v) = %d, want %d", tt.f, got, tt.want)
		}
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		fix  int64
		want float64
	}{
		{0, 0.0},
		{4096, 1.0},
		{8192, 2.0},
		{-4096, -1.0},
		{2048, 0.5},
	}
	for _, tt := range tests {
		got := ToFloat(tt.fix)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("ToFloat(%d) = %v, want %v", tt.fix, got, tt.want)
		}
	}
}

func TestMul(t *testing.T) {
	// 2.0 * 3.0 = 6.0
	a, b := FromFloat(2.0), FromFloat(3.0)
	got := Mul(a, b)
	want := FromFloat(6.0)
	if got != want {
		t.Errorf("Mul(%d, %d) = %d, want %d", a, b, got, want)
	}
	// negative
	got = Mul(FromFloat(-2.0), FromFloat(3.0))
	want = FromFloat(-6.0)
	if got != want {
		t.Errorf("Mul(-2, 3) = %d, want %d", got, want)
	}
}

func TestDiv(t *testing.T) {
	// 6.0 / 3.0 = 2.0
	a, b := FromFloat(6.0), FromFloat(3.0)
	got := Div(a, b)
	want := FromFloat(2.0)
	if got != want {
		t.Errorf("Div(%d, %d) = %d, want %d", a, b, got, want)
	}
}

func TestISqrt(t *testing.T) {
	// sqrt(9.0) = 3.0
	got := ISqrt(FromFloat(9.0))
	want := FromFloat(3.0)
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 50 { // allow ~0.01 error
		t.Errorf("ISqrt(9.0) = %d (~%v), want %d (~%v)", got, ToFloat(got), want, ToFloat(want))
	}
}

func TestDistSq(t *testing.T) {
	// (3,4) -> distSq = 25.0
	got := DistSq(FromFloat(3.0), FromFloat(4.0))
	want := FromFloat(25.0)
	if got != want {
		t.Errorf("DistSq(3,4) = %d, want %d", got, want)
	}
}

func TestClamp(t *testing.T) {
	got := Clamp(FromFloat(15.0), FromFloat(-10.0), FromFloat(10.0))
	want := FromFloat(10.0)
	if got != want {
		t.Errorf("Clamp(15, -10, 10) = %d, want %d", got, want)
	}
	got = Clamp(FromFloat(-15.0), FromFloat(-10.0), FromFloat(10.0))
	want = FromFloat(-10.0)
	if got != want {
		t.Errorf("Clamp(-15, -10, 10) = %d, want %d", got, want)
	}
}

func TestLerp(t *testing.T) {
	// lerp(0.0, 10.0, 0.5) = 5.0
	got := Lerp(FromFloat(0.0), FromFloat(10.0), FromFloat(0.5))
	want := FromFloat(5.0)
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 10 {
		t.Errorf("Lerp(0, 10, 0.5) = %d, want %d", got, want)
	}
}

func TestAngleLerp(t *testing.T) {
	// shortest path: 350° → 10° should go through 0°, not 180°
	got := AngleLerp(3500, 100, FromFloat(0.5))
	// midpoint should be near 0° (0)
	if got > 1800 && got < 3400 {
		t.Errorf("AngleLerp(3500, 100, 0.5) = %d, should take short path through 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/fixed/ -v`
Expected: Compilation error — package functions not defined.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/fixed/fixed.go
package fixed

const (
	FractionBits = 12
	One          = 1 << FractionBits // 4096
	Half         = One >> 1          // 2048
)

// FromFloat converts a float64 to fixed-point int64.
func FromFloat(f float64) int64 {
	return int64(f * float64(One))
}

// ToFloat converts a fixed-point int64 to float64.
func ToFloat(fix int64) float64 {
	return float64(fix) / float64(One)
}

// Mul returns a * b in fixed-point.
func Mul(a, b int64) int64 {
	return (a * b) >> FractionBits
}

// Div returns a / b in fixed-point.
func Div(a, b int64) int64 {
	return (a << FractionBits) / b
}

// ISqrt returns integer square root approximation of a fixed-point value.
// Uses Newton's method.
func ISqrt(val int64) int64 {
	if val <= 0 {
		return 0
	}
	// Work in raw integer space, then convert back
	x := val
	// Initial guess: bit shift for approximation
	guess := int64(1) << ((bitLen(x) + FractionBits) / 2)
	for i := 0; i < 10; i++ {
		if guess == 0 {
			break
		}
		guess = (guess + Div(val, guess)) >> 1
	}
	return guess
}

func bitLen(x int64) int {
	n := 0
	for x > 0 {
		x >>= 1
		n++
	}
	return n
}

// DistSq returns dx*dx + dy*dy in fixed-point.
func DistSq(dx, dy int64) int64 {
	return (dx*dx + dy*dy) >> FractionBits
}

// Clamp clamps val to [min, max].
func Clamp(val, min, max int64) int64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// Lerp returns linear interpolation: a + (b-a)*t.
func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}

// AngleLerp interpolates between two angles (0-3599) taking shortest path.
func AngleLerp(from, to int16, t int64) int16 {
	diff := int32(to) - int32(from)
	// Normalize to [-1800, 1800)
	if diff > 1800 {
		diff -= 3600
	} else if diff < -1800 {
		diff += 3600
	}
	result := int32(from) + int32(Mul(int64(diff), t))
	result %= 3600
	if result < 0 {
		result += 3600
	}
	return int16(result)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/fixed/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/fixed/
git commit -m "feat: add fixed-point arithmetic library (12-bit fraction)"
```

---

### Task 3: ECS Entity Manager

**Files:**
- Create: `server/pkg/ecs/entity.go`
- Modify: `server/pkg/ecs/ecs_test.go` (new)

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/ecs/ecs_test.go
package ecs

import "testing"

func TestEntityManagerCreate(t *testing.T) {
	em := NewEntityManager()
	e1 := em.Create()
	if e1 != 1 {
		t.Errorf("first entity = %d, want 1", e1)
	}
	e2 := em.Create()
	if e2 != 2 {
		t.Errorf("second entity = %d, want 2", e2)
	}
}

func TestEntityManagerDestroy(t *testing.T) {
	em := NewEntityManager()
	e1 := em.Create()
	em.Destroy(e1)
	if em.Alive(e1) {
		t.Error("destroyed entity should not be alive")
	}
}

func TestEntityManagerRecycle(t *testing.T) {
	em := NewEntityManager()
	e1 := em.Create()
	e2 := em.Create()
	em.Destroy(e1)
	e3 := em.Create()
	// e3 should reuse e1's slot (generation incremented)
	if e3 == e1 {
		t.Error("recycled entity should have different ID (new generation)")
	}
	if !em.Alive(e3) {
		t.Error("recycled entity should be alive")
	}
	if em.Alive(e1) {
		t.Error("original entity ID should not be alive after recycle")
	}
}

func TestEntityManagerMaxEntities(t *testing.T) {
	em := NewEntityManagerWithMax(10)
	for i := 0; i < 10; i++ {
		_ = em.Create()
	}
	e := em.Create()
	if e != 0 {
		t.Errorf("creating beyond max should return 0, got %d", e)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/ecs/ -v`
Expected: Compilation error — types/functions not defined.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/ecs/entity.go
package ecs

import "sync"

// Entity is a uint64: upper 32 bits = generation, lower 32 bits = index.
type Entity uint64

const InvalidEntity Entity = 0

func entityGen(e Entity) uint32 { return uint32(e >> 32) }
func entityIdx(e Entity) uint32 { return uint32(e & 0xFFFFFFFF) }
func makeEntity(idx, gen uint32) Entity {
	return Entity(uint64(gen)<<32 | uint64(idx))
}

// EntityManager creates, destroys, and recycles entity IDs.
type EntityManager struct {
	mu        sync.Mutex
	generations []uint32
	freeList   []uint32
	nextIdx    uint32
	maxEntities uint32
}

func NewEntityManager() *EntityManager {
	return NewEntityManagerWithMax(1 << 20) // ~1M entities
}

func NewEntityManagerWithMax(max uint32) *EntityManager {
	return &EntityManager{
		generations:  make([]uint32, 0, 1024),
		freeList:     make([]uint32, 0, 256),
		maxEntities:  max,
	}
}

// Create allocates a new Entity. Returns InvalidEntity if at capacity.
func (em *EntityManager) Create() Entity {
	em.mu.Lock()
	defer em.mu.Unlock()

	var idx uint32
	if len(em.freeList) > 0 {
		idx = em.freeList[len(em.freeList)-1]
		em.freeList = em.freeList[:len(em.freeList)-1]
	} else {
		idx = em.nextIdx
		if idx >= em.maxEntities {
			return InvalidEntity
		}
		em.nextIdx++
		if int(idx) >= len(em.generations) {
			em.generations = append(em.generations, make([]uint32, idx+1-uint32(len(em.generations)))...)
		}
	}

	gen := em.generations[idx]
	return makeEntity(idx, gen)
}

// Destroy marks an entity as dead and increments its generation.
func (em *EntityManager) Destroy(e Entity) {
	em.mu.Lock()
	defer em.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(em.generations) {
		return
	}
	if em.generations[idx] != entityGen(e) {
		return // already destroyed or stale reference
	}
	em.generations[idx]++
	em.freeList = append(em.freeList, idx)
}

// Alive checks if an entity is still alive (generation matches).
func (em *EntityManager) Alive(e Entity) bool {
	em.mu.Lock()
	defer em.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(em.generations) {
		return false
	}
	return em.generations[idx] == entityGen(e)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/ecs/ -v -run TestEntity`
Expected: All 4 entity tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/ecs/entity.go server/pkg/ecs/ecs_test.go
git commit -m "feat: add ECS entity manager with ID recycling"
```

---

### Task 4: ECS Component Pool

**Files:**
- Create: `server/pkg/ecs/pool.go`

- [ ] **Step 1: Write the failing tests**

Append to `server/pkg/ecs/ecs_test.go`:

```go
// --- Component types for testing ---

type PosComponent struct {
	X, Y int64
}

type VelComponent struct {
	Vx, Vy int64
}

// --- Pool tests ---

func TestComponentPoolAddGet(t *testing.T) {
	em := NewEntityManager()
	e := em.Create()

	pool := NewComponentPool[PosComponent]()
	pool.Add(e, PosComponent{X: 100, Y: 200})

	got, ok := pool.Get(e)
	if !ok {
		t.Fatal("component not found")
	}
	if got.X != 100 || got.Y != 200 {
		t.Errorf("got {%d, %d}, want {100, 200}", got.X, got.Y)
	}
}

func TestComponentPoolOverwrite(t *testing.T) {
	em := NewEntityManager()
	e := em.Create()

	pool := NewComponentPool[PosComponent]()
	pool.Add(e, PosComponent{X: 1, Y: 2})
	pool.Add(e, PosComponent{X: 10, Y: 20})

	got, _ := pool.Get(e)
	if got.X != 10 || got.Y != 20 {
		t.Errorf("got {%d, %d}, want {10, 20}", got.X, got.Y)
	}
}

func TestComponentPoolRemove(t *testing.T) {
	em := NewEntityManager()
	e := em.Create()

	pool := NewComponentPool[PosComponent]()
	pool.Add(e, PosComponent{X: 1, Y: 2})
	pool.Remove(e)

	_, ok := pool.Get(e)
	if ok {
		t.Error("removed component should not be found")
	}
}

func TestComponentPoolIterate(t *testing.T) {
	em := NewEntityManager()
	pool := NewComponentPool[PosComponent]()

	entities := make([]Entity, 3)
	for i := range entities {
		entities[i] = em.Create()
		pool.Add(entities[i], PosComponent{X: int64(i), Y: int64(i * 10)})
	}

	count := 0
	pool.Each(func(e Entity, c *PosComponent) {
		count++
		idx := entityIdx(e)
		if c.X != int64(idx-1) {
			t.Errorf("entity %d: X = %d, want %d", e, c.X, idx-1)
		}
	})
	if count != 3 {
		t.Errorf("itercount = %d, want 3", count)
	}
}

func TestComponentPoolAbsent(t *testing.T) {
	em := NewEntityManager()
	e := em.Create()

	pool := NewComponentPool[PosComponent]()
	_, ok := pool.Get(e)
	if ok {
		t.Error("non-existent component should return false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/ecs/ -v -run TestComponent`
Expected: Compilation error — NewComponentPool not defined.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/ecs/pool.go
package ecs

import "sync"

// ComponentPool stores components of type T, indexed by Entity.
// Uses a sparse array for O(1) access by entity index.
type ComponentPool[T any] struct {
	mu       sync.RWMutex
	sparse   []int32   // entity index → dense index (-1 = absent)
	dense    []uint32  // dense indices back into entity space
	data     []T       // packed component data
}

func NewComponentPool[T any]() *ComponentPool[T] {
	return &ComponentPool[T]{
		sparse: make([]int32, 0, 1024),
		dense:  make([]uint32, 0, 256),
		data:   make([]T, 0, 256),
	}
}

func (p *ComponentPool[T]) ensureSparse(idx uint32) {
	if int(idx) >= len(p.sparse) {
		newSparse := make([]int32, idx+1, idx*2)
		for i := range newSparse {
			newSparse[i] = -1
		}
		copy(newSparse, p.sparse)
		p.sparse = newSparse
	}
}

// Add adds or overwrites a component for the given entity.
func (p *ComponentPool[T]) Add(e Entity, comp T) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := entityIdx(e)
	p.ensureSparse(idx)

	if p.sparse[idx] >= 0 {
		// overwrite existing
		p.data[p.sparse[idx]] = comp
		return
	}

	// new entry
	di := int32(len(p.data))
	p.sparse[idx] = di
	p.dense = append(p.dense, idx)
	p.data = append(p.data, comp)
}

// Get returns the component and true, or zero value and false.
func (p *ComponentPool[T]) Get(e Entity) (T, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		var zero T
		return zero, false
	}
	di := p.sparse[idx]
	if di < 0 {
		var zero T
		return zero, false
	}
	return p.data[di], true
}

// GetPtr returns a pointer to the component for in-place mutation.
func (p *ComponentPool[T]) GetPtr(e Entity) (*T, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		return nil, false
	}
	di := p.sparse[idx]
	if di < 0 {
		return nil, false
	}
	return &p.data[di], true
}

// Remove removes the component for the given entity.
// Uses swap-erase for O(1) removal.
func (p *ComponentPool[T]) Remove(e Entity) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		return
	}
	di := p.sparse[idx]
	if di < 0 {
		return
	}

	// swap with last
	last := int32(len(p.data) - 1)
	if di != last {
		p.data[di] = p.data[last]
		p.dense[di] = p.dense[last]
		p.sparse[p.dense[last]] = di
	}

	p.data = p.data[:last]
	p.dense = p.dense[:last]
	p.sparse[idx] = -1
}

// Each iterates over all components. Do not modify the pool during iteration.
func (p *ComponentPool[T]) Each(fn func(Entity, *T)) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for i, comp := range p.data {
		fn(makeEntity(p.dense[i], 0), &p.data[i])
	}
}

// Len returns the number of stored components.
func (p *ComponentPool[T]) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/ecs/ -v -run TestComponent`
Expected: All 5 component pool tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/ecs/pool.go server/pkg/ecs/ecs_test.go
git commit -m "feat: add generic ECS component pool with sparse-set storage"
```

---

### Task 5: ECS System Interface & Scheduler

**Files:**
- Create: `server/pkg/ecs/system.go`

- [ ] **Step 1: Write the failing tests**

Append to `server/pkg/ecs/ecs_test.go`:

```go
// --- System tests ---

type TestSystem struct {
	executed bool
	ordinal  int
}

func (s *TestSystem) Name() string    { return "TestSystem" }
func (s *TestSystem) Priority() int   { return s.ordinal }
func (s *TestSystem) Init(w *World)   {}
func (s *TestSystem) Tick(w *World, tick uint32) {
	s.executed = true
}

func TestSchedulerExecutesSystems(t *testing.T) {
	em := NewEntityManager()
	w := NewWorld(em)

	sys := &TestSystem{ordinal: 10}
	w.AddSystem(sys)

	w.Tick(1)
	if !sys.executed {
		t.Error("system should have been executed")
	}
}

func TestSchedulerOrderByPriority(t *testing.T) {
	em := NewEntityManager()
	w := NewWorld(em)

	var order []int
	for i := 0; i < 3; i++ {
		prio := i
		w.AddSystem(&OrderSystem{ordinal: prio, record: &order})
	}

	w.Tick(1)
	if len(order) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(order))
	}
	for i, got := range order {
		if got != i {
			t.Errorf("order[%d] = %d, want %d", i, got, i)
		}
	}
}

type OrderSystem struct {
	ordinal int
	record  *[]int
}

func (s *OrderSystem) Name() string                              { return "OrderSystem" }
func (s *OrderSystem) Priority() int                             { return s.ordinal }
func (s *OrderSystem) Init(w *World)                             {}
func (s *OrderSystem) Tick(_ *World, _ uint32) { *s.record = append(*s.record, s.ordinal) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/ecs/ -v -run TestScheduler`
Expected: Compilation error — System interface, World type not defined.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/ecs/system.go
package ecs

import "sort"

// System is the interface all game systems implement.
type System interface {
	Name() string
	Priority() int // lower = runs first
	Init(w *World)
	Tick(w *World, tick uint32)
}

// systemEntry wraps a System with its registration state.
type systemEntry struct {
	System
	initialized bool
}

// Scheduler manages system execution order and lifecycle.
type Scheduler struct {
	systems []systemEntry
}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// AddSystem registers a system. Must be called before Init.
func (s *Scheduler) AddSystem(sys System) {
	s.systems = append(s.systems, systemEntry{System: sys})
	sort.Slice(s.systems, func(i, j int) bool {
		return s.systems[i].Priority() < s.systems[j].Priority()
	})
}

// Init calls Init on all systems in priority order.
func (s *Scheduler) Init(w *World) {
	for i := range s.systems {
		if !s.systems[i].initialized {
			s.systems[i].Init(w)
			s.systems[i].initialized = true
		}
	}
}

// Tick executes all systems in priority order.
func (s *Scheduler) Tick(w *World, tick uint32) {
	for i := range s.systems {
		s.systems[i].Tick(w, tick)
	}
}

// SystemByName returns a system by name, or nil.
func (s *Scheduler) SystemByName(name string) System {
	for i := range s.systems {
		if s.systems[i].Name() == name {
			return s.systems[i].System
		}
	}
	return nil
}
```

- [ ] **Step 4: Write the World type (needed by System tests)**

Create `server/pkg/ecs/world.go`:

```go
// server/pkg/ecs/world.go
package ecs

import "sync"

// World is the central container holding the entity manager and systems.
// Component pools are registered by type via RegisterPool.
type World struct {
	em       *EntityManager
	sched    *Scheduler
	pools    []interface{} // heterogeneous *ComponentPool[T]
	poolMu   sync.RWMutex
}

func NewWorld(em *EntityManager) *World {
	return &World{
		em:    em,
		sched: NewScheduler(),
	}
}

// Entities returns the entity manager.
func (w *World) Entities() *EntityManager { return w.em }

// AddSystem registers a system.
func (w *World) AddSystem(sys System) {
	w.sched.AddSystem(sys)
}

// SystemByName looks up a system by name.
func (w *World) SystemByName(name string) System {
	return w.sched.SystemByName(name)
}

// RegisterPool adds a component pool to the world.
// Called during initialization before Init.
func (w *World) RegisterPool(p interface{}) {
	w.poolMu.Lock()
	defer w.poolMu.Unlock()
	w.pools = append(w.pools, p)
}

// Init initializes all systems.
func (w *World) Init() {
	w.sched.Init(w)
}

// Tick advances the simulation by one tick.
func (w *World) Tick(tick uint32) {
	w.sched.Tick(w, tick)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./pkg/ecs/ -v`
Expected: All entity, component pool, and scheduler tests PASS.

- [ ] **Step 6: Commit**

```bash
git add server/pkg/ecs/system.go server/pkg/ecs/world.go server/pkg/ecs/ecs_test.go
git commit -m "feat: add ECS system interface, scheduler, and world container"
```

---

### Task 6: Spatial Hash Grid

**Files:**
- Create: `server/pkg/spatial/hash.go`
- Create: `server/pkg/spatial/hash_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/spatial/hash_test.go
package spatial

import (
	"testing"

	"github.com/user/paper-war/server/pkg/fixed"
)

func TestSpatialHashInsertAndQuery(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0)) // cell size = 10 world units

	sh.Insert(1, fixed.FromFloat(5.0), fixed.FromFloat(5.0))   // cell (0,0)
	sh.Insert(2, fixed.FromFloat(15.0), fixed.FromFloat(15.0))  // cell (1,1)
	sh.Insert(3, fixed.FromFloat(6.0), fixed.FromFloat(6.0))    // cell (0,0)

	// Query around (5,5) with radius 10 → should find 1 and 3 (same cell + neighbors)
	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(10.0))
	if len(found) < 2 {
		t.Errorf("Query(5,5,10) found %d entries, want >= 2", len(found))
	}
	has1, has3 := false, false
	for _, id := range found {
		if id == 1 { has1 = true }
		if id == 3 { has3 = true }
	}
	if !has1 || !has3 {
		t.Errorf("missing entities: has1=%v has3=%v", has1, has3)
	}
}

func TestSpatialHashClear(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0))
	sh.Insert(1, fixed.FromFloat(5.0), fixed.FromFloat(5.0))
	sh.Clear()

	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(20.0))
	if len(found) != 0 {
		t.Errorf("after Clear, found %d entries, want 0", len(found))
	}
}

func TestSpatialHashRemove(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0))
	sh.Insert(1, fixed.FromFloat(5.0), fixed.FromFloat(5.0))
	sh.Remove(1)

	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(20.0))
	if len(found) != 0 {
		t.Errorf("after Remove, found %d entries, want 0", len(found))
	}
}

func TestSpatialHashUpdate(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0))
	sh.Insert(1, fixed.FromFloat(5.0), fixed.FromFloat(5.0))
	sh.Update(1, fixed.FromFloat(25.0), fixed.FromFloat(25.0)) // moved to cell (2,2)

	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(10.0))
	for _, id := range found {
		if id == 1 {
			t.Error("entity 1 should no longer be near (5,5)")
		}
	}

	found = sh.Query(fixed.FromFloat(25.0), fixed.FromFloat(25.0), fixed.FromFloat(10.0))
	has1 := false
	for _, id := range found {
		if id == 1 { has1 = true }
	}
	if !has1 {
		t.Error("entity 1 should be found near (25,25)")
	}
}

func TestSpatialHashLargeCount(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0))
	// Insert 1000 entities in a 100x100 grid
	for i := 0; i < 1000; i++ {
		x := fixed.FromFloat(float64(i % 100))
		y := fixed.FromFloat(float64(i / 100))
		sh.Insert(uint64(i+1), x, y)
	}

	// Query a small area should not return all 1000
	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(12.0))
	if len(found) >= 100 {
		t.Errorf("small area query returned %d, expected much less than 100", len(found))
	}
	if len(found) == 0 {
		t.Error("small area query should find at least some entities")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/spatial/ -v`
Expected: Compilation error.

- [ ] **Step 3: Write the implementation**

```go
// server/pkg/spatial/hash.go
package spatial

// cellKey is a hashable cell coordinate pair.
type cellKey struct {
	X, Y int32
}

// entry stores an entity's position within a cell.
type entry struct {
	ID uint64
	X  int64
	Y  int64
}

// Hash is a spatial hash grid for fast neighbor queries.
// Thread-unsafe: designed for single-threaded game tick use.
type Hash struct {
	CellSize  int64
	inverseCS int64 // (1<<12) / CellSize for fast cell coord calc
	cells     map[cellKey][]entry
	positions map[uint64]cellKey // entity ID → current cell
}

func NewHash(cellSize int64) *Hash {
	return &Hash{
		CellSize:  cellSize,
		inverseCS: (1 << 12) / cellSize,
		cells:     make(map[cellKey][]entry, 1024),
		positions: make(map[uint64]cellKey, 1024),
	}
}

func (h *Hash) cellCoord(x int64) int32 {
	return int32((x * h.inverseCS) >> 12)
}

func (h *Hash) cellKey(x, y int64) cellKey {
	return cellKey{X: h.cellCoord(x), Y: h.cellCoord(y)}
}

// Insert adds an entity at the given position.
func (h *Hash) Insert(id uint64, x, y int64) {
	ck := h.cellKey(x, y)
	h.cells[ck] = append(h.cells[ck], entry{ID: id, X: x, Y: y})
	h.positions[id] = ck
}

// Remove removes an entity by ID.
func (h *Hash) Remove(id uint64) {
	ck, ok := h.positions[id]
	if !ok {
		return
	}
	cell := h.cells[ck]
	for i, e := range cell {
		if e.ID == id {
			cell[i] = cell[len(cell)-1]
			h.cells[ck] = cell[:len(cell)-1]
			break
		}
	}
	if len(h.cells[ck]) == 0 {
		delete(h.cells, ck)
	}
	delete(h.positions, id)
}

// Update moves an entity to a new position.
func (h *Hash) Update(id uint64, x, y int64) {
	h.Remove(id)
	h.Insert(id, x, y)
}

// Clear removes all entities.
func (h *Hash) Clear() {
	for k := range h.cells {
		delete(h.cells, k)
	}
	for k := range h.positions {
		delete(h.positions, k)
	}
}

// Query returns all entity IDs within radius of (x, y).
// Checks the 9 neighboring cells around the cell containing (x,y).
func (h *Hash) Query(x, y, radius int64) []uint64 {
	radiusSq := (radius * radius) >> 12
	cx := h.cellCoord(x)
	cy := h.cellCoord(y)

	var result []uint64
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			ck := cellKey{X: cx + dx, Y: cy + dy}
			cell, ok := h.cells[ck]
			if !ok {
				continue
			}
			for _, e := range cell {
				ddx := e.X - x
				ddy := e.Y - y
				distSq := (ddx*ddx + ddy*ddy) >> 12
				if distSq <= radiusSq {
					result = append(result, e.ID)
				}
			}
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/spatial/ -v`
Expected: All 5 spatial hash tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/spatial/
git commit -m "feat: add spatial hash grid for neighbor queries"
```

---

### Task 7: Game Loop (15Hz)

**Files:**
- Create: `server/pkg/game/loop.go`
- Create: `server/pkg/game/loop_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// server/pkg/game/loop_test.go
package game

import (
	"sync/atomic"
	"testing"
	"time"
)

type counterSystem struct {
	count atomic.Int32
}

func (s *counterSystem) Name() string    { return "counter" }
func (s *counterSystem) Priority() int   { return 0 }
func (s *counterSystem) Init(_ interface{}) {}
func (s *counterSystem) Tick(_ interface{}, _ uint32) {
	s.count.Add(1)
}

func TestGameLoopRunAndStop(t *testing.T) {
	loop := NewLoop(15) // 15 Hz

	cs := &counterSystem{}
	loop.AddSystem(cs)

	loop.Start()
	time.Sleep(200 * time.Millisecond) // ~3 ticks at 15Hz
	loop.Stop()

	c := cs.count.Load()
	if c < 2 || c > 5 {
		t.Errorf("after 200ms at 15Hz, tick count = %d, expected 2-5", c)
	}
}

func TestGameLoopTickSequence(t *testing.T) {
	loop := NewLoop(100) // fast for testing

	var lastTick atomic.Uint32
	type tickSystem struct {
		ref *atomic.Uint32
	}
	ts := &tickSystem{ref: &lastTick}
	loop.AddSystem(&struct {
		NameFunc     func() string
		PriorityFunc func() int
		InitFunc     func(interface{})
		TickFunc     func(interface{}, uint32)
	}{
		NameFunc:     func() string { return "ticker" },
		PriorityFunc: func() int { return 0 },
		InitFunc:     func(interface{}) {},
		TickFunc:     func(_ interface{}, tick uint32) {
			ts.ref.Store(tick)
		},
	})

	loop.Start()
	time.Sleep(100 * time.Millisecond)
	loop.Stop()

	if lastTick.Load() < 5 {
		t.Errorf("lastTick = %d, expected >= 5", lastTick.Load())
	}
}

func TestGameLoopStopIdempotent(t *testing.T) {
	loop := NewLoop(15)
	loop.Start()
	loop.Stop()
	loop.Stop() // should not panic
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/game/ -v`
Expected: Compilation error.

- [ ] **Step 3: Write the implementation**

The game loop needs to accept a generic system interface. Since our ECS System interface depends on `*World`, and the game loop shouldn't import the ECS package directly, we define a minimal interface:

```go
// server/pkg/game/loop.go
package game

import (
	"sync"
	"time"
)

// System is the minimal interface the game loop requires.
type System interface {
	Name() string
	Priority() int
	Init(world interface{})
	Tick(world interface{}, tick uint32)
}

// Loop runs systems at a fixed tick rate.
type Loop struct {
	tickRate  int           // ticks per second
	interval  time.Duration
	systems   []System
	tickCount uint32
	running   bool
	stopCh    chan struct{}
	mu        sync.Mutex
	world     interface{} // optional world reference
}

func NewLoop(tickRate int) *Loop {
	return &Loop{
		tickRate: tickRate,
		interval: time.Second / time.Duration(tickRate),
		systems:  make([]System, 0),
		stopCh:   make(chan struct{}),
	}
}

// AddSystem registers a system. Must be called before Start.
func (l *Loop) AddSystem(sys System) {
	l.systems = append(l.systems, sys)
}

// SetWorld sets the world reference passed to system Init/Tick.
func (l *Loop) SetWorld(w interface{}) {
	l.world = w
}

// Start begins the game loop in a background goroutine.
func (l *Loop) Start() {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return
	}
	l.running = true
	l.stopCh = make(chan struct{})
	l.mu.Unlock()

	// Init all systems
	for _, sys := range l.systems {
		sys.Init(l.world)
	}

	go l.run()
}

// Stop gracefully stops the game loop.
func (l *Loop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	l.running = false
	close(l.stopCh)
}

func (l *Loop) run() {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.tickCount++
			for _, sys := range l.systems {
				sys.Tick(l.world, l.tickCount)
			}
		}
	}
}

// TickCount returns the current tick number.
func (l *Loop) TickCount() uint32 {
	return l.tickCount
}

// Running returns whether the loop is active.
func (l *Loop) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/game/ -v`
Expected: All 3 game loop tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/game/
git commit -m "feat: add 15Hz game loop with system scheduling"
```

---

### Task 8: Integration Test — Full ECS Pipeline

**Files:**
- Modify: `server/pkg/ecs/ecs_test.go`

- [ ] **Step 1: Write the integration test**

Append to `server/pkg/ecs/ecs_test.go`:

```go
// --- Integration: full ECS pipeline ---

type MoveSystem struct {
	pool *ComponentPool[PosComponent]
	vel  *ComponentPool[VelComponent]
}

func (s *MoveSystem) Name() string    { return "MoveSystem" }
func (s *MoveSystem) Priority() int   { return 10 }
func (s *MoveSystem) Init(w *World)   {
	s.pool = w.Pool(PosComponent{}).(*ComponentPool[PosComponent])
	s.vel = w.Pool(VelComponent{}).(*ComponentPool[VelComponent])
}
func (s *MoveSystem) Tick(w *World, tick uint32) {
	s.pool.Each(func(e Entity, pos *PosComponent) {
		vel, ok := s.vel.Get(e)
		if !ok {
			return
		}
		pos.X += vel.Vx
		pos.Y += vel.Vy
	})
}

func TestIntegrationMoveSystem(t *testing.T) {
	em := NewEntityManager()
	w := NewWorld(em)

	posPool := NewComponentPool[PosComponent]()
	velPool := NewComponentPool[VelComponent]()
	w.RegisterPool(posPool)
	w.RegisterPool(velPool)

	w.AddSystem(&MoveSystem{})

	e1 := em.Create()
	posPool.Add(e1, PosComponent{X: 100, Y: 100})
	velPool.Add(e1, VelComponent{Vx: 10, Vy: 5})

	e2 := em.Create()
	posPool.Add(e2, PosComponent{X: 0, Y: 0})
	velPool.Add(e2, VelComponent{Vx: -5, Vy: 3})

	w.Init()

	// Tick 1
	w.Tick(1)
	p1, _ := posPool.Get(e1)
	if p1.X != 110 || p1.Y != 105 {
		t.Errorf("e1 after tick 1: {%d, %d}, want {110, 105}", p1.X, p1.Y)
	}
	p2, _ := posPool.Get(e2)
	if p2.X != -5 || p2.Y != 3 {
		t.Errorf("e2 after tick 1: {%d, %d}, want {-5, 3}", p2.X, p2.Y)
	}

	// Tick 2
	w.Tick(2)
	p1, _ = posPool.Get(e1)
	if p1.X != 120 || p1.Y != 110 {
		t.Errorf("e1 after tick 2: {%d, %d}, want {120, 110}", p1.X, p1.Y)
	}
}
```

- [ ] **Step 2: Add Pool accessor to World**

The integration test needs `w.Pool()` to retrieve registered pools. Add to `server/pkg/ecs/world.go`:

```go
// Pool returns the first registered pool. Caller type-asserts.
// This is a minimal approach; a production ECS would use typed accessors.
func (w *World) Pool(sample interface{}) interface{} {
	w.poolMu.RLock()
	defer w.poolMu.RUnlock()
	if len(w.pools) == 0 {
		return nil
	}
	return w.pools[0] // simplified: real impl would match by type
}
```

Actually, a better approach is to use a type-keyed registry:

Replace the `RegisterPool` / `Pool` methods in `world.go` with:

```go
// poolKey uses a type's string representation as key.
type poolKey string

// RegisterPool stores a component pool keyed by its generic type.
// Uses Go's reflection-free pattern: caller passes a sample value of T.
func (w *World) RegisterPool(sample interface{}, pool interface{}) {
	w.poolMu.Lock()
	defer w.poolMu.Unlock()
	w.poolMap[poolKey(fmt.Sprintf("%T", sample))] = pool
}

// Pool retrieves a component pool by sample type.
func (w *World) Pool(sample interface{}) interface{} {
	w.poolMu.RLock()
	defer w.poolMu.RUnlock()
	return w.poolMap[poolKey(fmt.Sprintf("%T", sample))]
}
```

Update `World` struct:

```go
type World struct {
	em       *EntityManager
	sched    *Scheduler
	poolMap  map[poolKey]interface{}
	poolMu   sync.RWMutex
}

func NewWorld(em *EntityManager) *World {
	return &World{
		em:      em,
		sched:   NewScheduler(),
		poolMap: make(map[poolKey]interface{}),
	}
}
```

Add `"fmt"` to imports in world.go.

Update integration test to use typed pool access:

```go
w.RegisterPool(PosComponent{}, posPool)
w.RegisterPool(VelComponent{}, velPool)
```

Update MoveSystem.Init to use typed retrieval:

```go
func (s *MoveSystem) Init(w *World) {
	s.pool = w.Pool(PosComponent{}).(*ComponentPool[PosComponent])
	s.vel = w.Pool(VelComponent{}).(*ComponentPool[VelComponent])
}
```

- [ ] **Step 3: Run all tests**

Run: `cd server && go test ./pkg/... -v`
Expected: All tests across fixed, ecs, spatial, and game packages PASS.

- [ ] **Step 4: Commit**

```bash
git add server/
git commit -m "feat: add ECS integration test with type-keyed pool registry"
```

---

## Self-Review Checklist

### Spec Coverage
- [x] Fixed-point arithmetic (Spec Section 2) → Task 2
- [x] ECS Entity Manager → Task 3
- [x] ECS Component Pool (dense packed arrays) → Task 4
- [x] ECS System interface + Scheduler → Task 5
- [x] World container → Task 5
- [x] Spatial Hash → Task 6
- [x] Game Loop (15Hz) → Task 7
- [x] Integration test → Task 8

### Placeholder Scan
- No TBD/TODO/placeholders found.

### Type Consistency
- Entity = uint64 throughout
- Fixed-point = int64 throughout, using `fixed.FromFloat` / `fixed.ToFloat`
- Component pools use generic `ComponentPool[T]`
- World.Pool uses `%T` string key — consistent across register/retrieve
