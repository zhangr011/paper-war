package network

import (
	"testing"
)

func TestSnapshotNoChange(t *testing.T) {
	sg := NewSnapshotGenerator()
	state := EntityState{X: 100, Y: 200, HP: 100}

	sg.Generate(1, []EntityState{state}, []uint32{1})
	snap := sg.Generate(2, []EntityState{state}, []uint32{1})

	if len(snap.Units) != 0 {
		t.Errorf("no-change frame should have 0 units, got %d", len(snap.Units))
	}
}

func TestSnapshotDetectsPositionChange(t *testing.T) {
	sg := NewSnapshotGenerator()
	s1 := EntityState{X: 100, Y: 200, HP: 100}
	sg.Generate(1, []EntityState{s1}, []uint32{1})

	s2 := EntityState{X: 500, Y: 200, HP: 100}
	snap := sg.Generate(2, []EntityState{s2}, []uint32{1})

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 changed unit, got %d", len(snap.Units))
	}
	if snap.Units[0].ChangedMask&ChangedPosition == 0 {
		t.Error("position change not detected")
	}
	if snap.Units[0].ChangedMask&ChangedHP != 0 {
		t.Error("HP should not be flagged as changed")
	}
}

func TestSnapshotNewUnit(t *testing.T) {
	sg := NewSnapshotGenerator()
	sg.Generate(1, []EntityState{}, []uint32{})

	state := EntityState{X: 100, Y: 200, HP: 50, Angle: 900}
	snap := sg.Generate(2, []EntityState{state}, []uint32{1})

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 new unit, got %d", len(snap.Units))
	}
	if snap.Units[0].ChangedMask != 0xFF {
		t.Errorf("new unit mask = %08b, want 11111111", snap.Units[0].ChangedMask)
	}
}

func TestSnapshotDetectsSquadIDChange(t *testing.T) {
	sg := NewSnapshotGenerator()
	s1 := EntityState{X: 100, Y: 200, HP: 100, SquadID: 1}
	sg.Generate(1, []EntityState{s1}, []uint32{1})

	s2 := s1
	s2.SquadID = 2
	snap := sg.Generate(2, []EntityState{s2}, []uint32{1})

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 changed unit, got %d", len(snap.Units))
	}
	if snap.Units[0].ChangedMask&ChangedSquadID == 0 {
		t.Error("squad ID change not detected")
	}
	if snap.Units[0].SquadID != 2 {
		t.Errorf("squad ID = %d, want 2", snap.Units[0].SquadID)
	}
}

func TestSnapshotEncode(t *testing.T) {
	sg := NewSnapshotGenerator()
	state := EntityState{X: 100, Y: 200, HP: 100}
	sg.Generate(1, []EntityState{state}, []uint32{1})

	state.X = 500
	snap := sg.Generate(2, []EntityState{state}, []uint32{1})
	data := EncodeSnapshot(snap)

	if len(data) == 0 {
		t.Error("encoded snapshot should not be empty")
	}
	// Verify tick is encoded correctly (first 4 bytes LE)
	tick := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	if tick != 2 {
		t.Errorf("encoded tick = %d, want 2", tick)
	}
}

func TestSnapshotMultipleUnits(t *testing.T) {
	sg := NewSnapshotGenerator()
	states := []EntityState{
		{X: 100, Y: 100, HP: 100},
		{X: 200, Y: 200, HP: 80},
		{X: 300, Y: 300, HP: 60},
	}
	ids := []uint32{1, 2, 3}
	sg.Generate(1, states, ids)

	// Change only unit 2's HP
	states[1].HP = 50
	snap := sg.Generate(2, states, ids)

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 changed unit, got %d", len(snap.Units))
	}
	if snap.Units[0].EntityID != 2 {
		t.Errorf("changed unit = %d, want 2", snap.Units[0].EntityID)
	}
}
