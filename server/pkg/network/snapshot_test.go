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

func TestSnapshotNewUnitCarriesUnitTypeAndTeam(t *testing.T) {
	sg := NewSnapshotGenerator()
	sg.Generate(1, []EntityState{}, []uint32{})

	state := EntityState{X: 100, Y: 200, HP: 50, UnitType: 3, Team: 2}
	snap := sg.Generate(2, []EntityState{state}, []uint32{42})

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 new unit, got %d", len(snap.Units))
	}
	u := snap.Units[0]
	if u.UnitType != 3 {
		t.Errorf("UnitType = %d, want 3", u.UnitType)
	}
	if u.Team != 2 {
		t.Errorf("Team = %d, want 2", u.Team)
	}
	if u.ChangedMask != MaskFull {
		t.Errorf("mask = %02x, want %02x", u.ChangedMask, MaskFull)
	}

	// Encode and verify the UnitType+Team bytes are appended after SquadID
	data := EncodeSnapshot(snap)
	if len(data) == 0 {
		t.Fatal("encoded snapshot empty")
	}

	// Decode manually: header = 4+4+2+1=11 bytes
	// Per new unit: entityID(4) + mask(1) + X(8) + Y(8) + Vx(8) + Vy(8) + Angle(2) + HP(4) + TargetID(4) + Morale(4) + State(1) + SquadID(4) + UnitType(1) + Team(1) = 58
	offset := 11
	entityID := uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24
	if entityID != 42 {
		t.Errorf("decoded entityID = %d, want 42", entityID)
	}
	offset += 4 + 1 // entityID + mask

	// Skip position (16), velocity (16), angle (2), HP (4), targetID (4), morale (4), state (1), squadID (4) = 51
	offset += 16 + 16 + 2 + 4 + 4 + 4 + 1 + 4

	if offset+2 > len(data) {
		t.Fatalf("data too short for UnitType+Team: len=%d, need offset=%d", len(data), offset+2)
	}
	if data[offset] != 3 {
		t.Errorf("decoded UnitType = %d, want 3", data[offset])
	}
	if data[offset+1] != 2 {
		t.Errorf("decoded Team = %d, want 2", data[offset+1])
	}
}

func TestSnapshotDiffUnitOmitsUnitTypeAndTeam(t *testing.T) {
	sg := NewSnapshotGenerator()
	s1 := EntityState{X: 100, Y: 200, HP: 100, UnitType: 3, Team: 2}
	sg.Generate(1, []EntityState{s1}, []uint32{1})

	// Change HP only — this is a diff, not a new unit
	s2 := s1
	s2.HP = 50
	snap := sg.Generate(2, []EntityState{s2}, []uint32{1})

	if len(snap.Units) != 1 {
		t.Fatalf("expected 1 diff unit, got %d", len(snap.Units))
	}
	u := snap.Units[0]
	// UnitType/Team should be zero for diff updates (not sent)
	if u.UnitType != 0 || u.Team != 0 {
		t.Errorf("diff unit should have zero UnitType/Team, got %d/%d", u.UnitType, u.Team)
	}
	if u.ChangedMask == MaskFull {
		t.Error("diff unit should not have MaskFull")
	}
}
