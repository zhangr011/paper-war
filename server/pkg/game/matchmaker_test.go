package game

import (
	"testing"
)

// TestMatchmakerJoin verifies basic queue behavior: a player joins and is counted.
func TestMatchmakerJoin(t *testing.T) {
	m := NewMatchmaker(nil)
	if m.QueueLen() != 0 {
		t.Fatalf("empty matchmaker QueueLen = %d, want 0", m.QueueLen())
	}
	if !m.Join(1, "alice") {
		t.Error("first Join returned false (should succeed)")
	}
	if m.QueueLen() != 1 {
		t.Errorf("after one Join, QueueLen = %d, want 1", m.QueueLen())
	}
}

// TestMatchmakerJoinDuplicate verifies that the same ClientID can't
// join twice — prevents a player from queueing against themselves.
func TestMatchmakerJoinDuplicate(t *testing.T) {
	m := NewMatchmaker(nil)
	m.Join(1, "alice")
	if m.Join(1, "alice") {
		t.Error("duplicate Join returned true — should reject same ClientID")
	}
	if m.QueueLen() != 1 {
		t.Errorf("after duplicate Join, QueueLen = %d, want 1", m.QueueLen())
	}
}

// TestMatchmakerLeave verifies that Leave removes the player from the queue.
func TestMatchmakerLeave(t *testing.T) {
	m := NewMatchmaker(nil)
	m.Join(1, "alice")
	m.Join(2, "bob")
	if m.QueueLen() != 2 {
		t.Fatalf("before Leave: QueueLen = %d, want 2", m.QueueLen())
	}

	m.Leave(1)

	if m.QueueLen() != 1 {
		t.Errorf("after Leave(1): QueueLen = %d, want 1", m.QueueLen())
	}
}

// TestMatchmakerLeaveNotInQueue verifies Leave is a no-op for unknown clientIDs.
func TestMatchmakerLeaveNotInQueue(t *testing.T) {
	m := NewMatchmaker(nil)
	m.Join(1, "alice")
	// Leave a clientID that's not in queue — should not panic, should not change length
	m.Leave(999)
	if m.QueueLen() != 1 {
		t.Errorf("after Leave(unknown): QueueLen = %d, want 1", m.QueueLen())
	}
}

// TestMatchmakerTickStartsMatch verifies that Tick fires the onMatch callback
// when enough players are queued, and removes them from the queue.
func TestMatchmakerTickStartsMatch(t *testing.T) {
	var matched [][]QueuePlayer
	m := NewMatchmaker(func(players []QueuePlayer) {
		matched = append(matched, players)
	})
	m.Join(1, "alice")
	m.Join(2, "bob")

	m.Tick(2)

	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if len(matched[0]) != 2 {
		t.Errorf("matched %d players, want 2", len(matched[0]))
	}
	// Queue should be drained
	if m.QueueLen() != 0 {
		t.Errorf("after match, QueueLen = %d, want 0", m.QueueLen())
	}
}

// TestMatchmakerTickBelowThreshold verifies that Tick is a no-op when
// the queue is below the threshold.
func TestMatchmakerTickBelowThreshold(t *testing.T) {
	var matched bool
	m := NewMatchmaker(func([]QueuePlayer) { matched = true })
	m.Join(1, "alice")

	m.Tick(2)

	if matched {
		t.Error("onMatch fired despite queue below threshold")
	}
	if m.QueueLen() != 1 {
		t.Errorf("QueueLen = %d, want 1 (player should still be queued)", m.QueueLen())
	}
}

// TestMatchmakerTickDrainsExcessPlayers verifies that after a match fires,
// queued players beyond the matchSize remain in the queue.
func TestMatchmakerTickDrainsExcessPlayers(t *testing.T) {
	var matched []QueuePlayer
	m := NewMatchmaker(func(p []QueuePlayer) { matched = p })
	m.Join(1, "alice")
	m.Join(2, "bob")
	m.Join(3, "carol") // excess player

	m.Tick(2)

	if len(matched) != 2 {
		t.Fatalf("match size = %d, want 2", len(matched))
	}
	if m.QueueLen() != 1 {
		t.Errorf("after match with excess: QueueLen = %d, want 1 (carol remains)", m.QueueLen())
	}
}

// TestMatchmakerNilCallback verifies that Tick doesn't panic when onMatch is nil.
func TestMatchmakerNilCallback(t *testing.T) {
	m := NewMatchmaker(nil)
	m.Join(1, "alice")
	m.Join(2, "bob")
	// Should not panic
	m.Tick(2)
	if m.QueueLen() != 0 {
		t.Errorf("QueueLen = %d, want 0 after Tick (drained regardless of callback)", m.QueueLen())
	}
}
