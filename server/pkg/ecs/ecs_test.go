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