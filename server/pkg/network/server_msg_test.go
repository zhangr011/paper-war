package network

import (
	"testing"
)

func TestEncodeDecodeRecruitCommand(t *testing.T) {
	original := &Command{
		Type:        CmdRecruit,
		ClientSeq:   42,
		SquadID:     7,
		RecruitType: 3, // Cannon
	}

	data := EncodeCommand(original)
	decoded, err := DecodeCommand(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Type != CmdRecruit {
		t.Fatalf("type = %d, want %d", decoded.Type, CmdRecruit)
	}
	if decoded.RecruitType != 3 {
		t.Fatalf("recruit type = %d, want 3", decoded.RecruitType)
	}
	if decoded.SquadID != 7 {
		t.Fatalf("squad ID = %d, want 7", decoded.SquadID)
	}
}

func TestEncodeDecodeSelectCommander(t *testing.T) {
	original := &Command{
		Type:      CmdSelectCommander,
		ClientSeq: 1,
		SquadID:   5,
	}

	data := EncodeCommand(original)
	decoded, err := DecodeCommand(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Type != CmdSelectCommander {
		t.Fatalf("type = %d, want %d", decoded.Type, CmdSelectCommander)
	}
	if decoded.SquadID != 5 {
		t.Fatalf("squad ID = %d, want 5", decoded.SquadID)
	}
}

func TestServerMessageGoldUpdate(t *testing.T) {
	original := &ServerMessage{Type: MsgGoldUpdate, Gold: 150}
	data := EncodeServerMessage(original)
	decoded, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Gold != 150 {
		t.Fatalf("gold = %d, want 150", decoded.Gold)
	}
}

func TestServerMessageMatchResult(t *testing.T) {
	original := &ServerMessage{Type: MsgMatchResult, Winner: 0, Reason: "elimination"}
	data := EncodeServerMessage(original)
	decoded, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Winner != 0 {
		t.Fatalf("winner = %d, want 0", decoded.Winner)
	}
	if decoded.Reason != "elimination" {
		t.Fatalf("reason = %q, want %q", decoded.Reason, "elimination")
	}
}

func TestServerMessageRosterUpdate(t *testing.T) {
	rosterData := []byte{1, 2, 3, 4, 5}
	original := &ServerMessage{Type: MsgRosterUpdate, RosterData: rosterData}
	data := EncodeServerMessage(original)
	decoded, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(decoded.RosterData) != 5 {
		t.Fatalf("roster data len = %d, want 5", len(decoded.RosterData))
	}
}

func TestEncodeDecodeMatchStats(t *testing.T) {
	stats := [2]MatchStatsEntry{
		{Kills: 15, Deaths: 8, CommanderKills: 2, UnitsRecruited: 12, GoldEarned: 450, GoldSpent: 300},
		{Kills: 8, Deaths: 15, CommanderKills: 1, UnitsRecruited: 10, GoldEarned: 240, GoldSpent: 250},
	}
	msg := &ServerMessage{Type: MsgMatchStats, Stats: stats}
	encoded := EncodeServerMessage(msg)
	decoded, err := DecodeServerMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Type != MsgMatchStats {
		t.Fatalf("type = 0x%02x, want 0x%02x", decoded.Type, MsgMatchStats)
	}
	for i := 0; i < 2; i++ {
		if decoded.Stats[i] != stats[i] {
			t.Errorf("faction %d: got %+v, want %+v", i, decoded.Stats[i], stats[i])
		}
	}
}
