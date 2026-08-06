package network

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeMoveSquad(t *testing.T) {
	orig := &Command{
		Type: CmdMoveSquad, ClientSeq: 42, PredictedTick: 100,
		SquadID: 5, TargetX: 3200, TargetY: -1600,
	}
	data := EncodeCommand(orig)
	// Header (1+4+4) + SquadID(4) + TargetX(4) + TargetY(4) = 21 bytes
	if len(data) != 21 {
		t.Errorf("MoveSquad encoded to %d bytes, want 21", len(data))
	}

	decoded, err := DecodeCommand(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != orig.Type || decoded.SquadID != orig.SquadID ||
		decoded.TargetX != orig.TargetX || decoded.TargetY != orig.TargetY {
		t.Errorf("decoded = %+v, want %+v", decoded, orig)
	}
}

func TestEncodeDecodeAttackTarget(t *testing.T) {
	orig := &Command{
		Type: CmdAttackTarget, ClientSeq: 1, PredictedTick: 50,
		SquadID: 3, TargetID: 99,
	}
	data := EncodeCommand(orig)
	// Header(9) + SquadID(4) + TargetID(4) = 17
	if len(data) != 17 {
		t.Errorf("AttackTarget encoded to %d bytes, want 17", len(data))
	}

	decoded, err := DecodeCommand(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TargetID != 99 {
		t.Errorf("TargetID = %d, want 99", decoded.TargetID)
	}
}

func TestDecodeInvalidData(t *testing.T) {
	_, err := DecodeCommand([]byte{0xFF}) // too short
	if err == nil {
		t.Error("should fail on truncated data")
	}
}

func TestRoundTripAllTypes(t *testing.T) {
	cmds := []*Command{
		{Type: CmdMoveSquad, SquadID: 1, TargetX: 100, TargetY: 200},
		{Type: CmdAttackTarget, SquadID: 2, TargetID: 50},
		{Type: CmdAttackGround, SquadID: 3, TargetX: -500, TargetY: 300},
		{Type: CmdTacticalOrder, SquadID: 5, OrderType: 1},
	}
	for _, orig := range cmds {
		orig.ClientSeq = 1
		orig.PredictedTick = 1
		data := EncodeCommand(orig)
		decoded, err := DecodeCommand(data)
		if err != nil {
			t.Errorf("type %d: decode error: %v", orig.Type, err)
			continue
		}
		if !bytes.Equal(EncodeCommand(orig), EncodeCommand(decoded)) {
			t.Errorf("type %d: roundtrip mismatch", orig.Type)
		}
	}
}
