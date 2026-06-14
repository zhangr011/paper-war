package spatial

import (
	"testing"

	"github.com/user/paper-war/server/pkg/fixed"
)

func TestSpatialHashInsertAndQuery(t *testing.T) {
	sh := NewHash(fixed.FromFloat(10.0))

	sh.Insert(1, fixed.FromFloat(5.0), fixed.FromFloat(5.0))
	sh.Insert(2, fixed.FromFloat(15.0), fixed.FromFloat(15.0))
	sh.Insert(3, fixed.FromFloat(6.0), fixed.FromFloat(6.0))

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
	sh.Update(1, fixed.FromFloat(25.0), fixed.FromFloat(25.0))

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
	for i := 0; i < 1000; i++ {
		x := fixed.FromFloat(float64(i % 100))
		y := fixed.FromFloat(float64(i / 100))
		sh.Insert(uint64(i+1), x, y)
	}

	found := sh.Query(fixed.FromFloat(5.0), fixed.FromFloat(5.0), fixed.FromFloat(2.0))
	if len(found) >= 100 {
		t.Errorf("small area query returned %d, expected much less than 100", len(found))
	}
	if len(found) == 0 {
		t.Error("small area query should find at least some entities")
	}
}