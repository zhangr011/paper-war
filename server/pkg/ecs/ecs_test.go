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