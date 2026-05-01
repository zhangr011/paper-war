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
	// e2 is intentionally alive to ensure we're testing recycling correctly
	if !em.Alive(e2) {
		t.Error("e2 should still be alive")
	}
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

func (s *OrderSystem) Name() string    { return "OrderSystem" }
func (s *OrderSystem) Priority() int   { return s.ordinal }
func (s *OrderSystem) Init(w *World)   {}
func (s *OrderSystem) Tick(_ *World, _ uint32) { *s.record = append(*s.record, s.ordinal) }

// --- Integration: full ECS pipeline ---

type MoveSystem struct {
	pool *ComponentPool[PosComponent]
	vel  *ComponentPool[VelComponent]
}

func (s *MoveSystem) Name() string    { return "MoveSystem" }
func (s *MoveSystem) Priority() int   { return 10 }
func (s *MoveSystem) Init(w *World) {
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
	w.RegisterPool(PosComponent{}, posPool)
	w.RegisterPool(VelComponent{}, velPool)

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