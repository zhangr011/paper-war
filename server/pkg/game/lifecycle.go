package game

import (
	"sync"
	"time"
)

// MatchPhase represents the current phase of a match.
type MatchPhase uint8

const (
	PhaseLobby   MatchPhase = 0
	PhasePlaying MatchPhase = 1
	PhaseEnded   MatchPhase = 2
)

// MatchLifecycle manages the phases of a single match.
type MatchLifecycle struct {
	mu       sync.Mutex
	Phase    MatchPhase
	Started  time.Time
	Ended    time.Time
	FlushSec int // seconds after end to flush (default 30)

	WinnerFaction  uint8 // set when match ends
	WinReason      string
	MatchResultSent bool // true once MsgMatchResult has been sent to clients

	onStart func()
	onEnd   func(winnerFaction uint8, reason string)
}

func NewMatchLifecycle(onStart func(), onEnd func(uint8, string)) *MatchLifecycle {
	return &MatchLifecycle{
		Phase:    PhaseLobby,
		FlushSec: 30,
		onStart:  onStart,
		onEnd:    onEnd,
	}
}

// Start transitions from Lobby to Playing.
func (ml *MatchLifecycle) Start() {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.Phase != PhaseLobby {
		return
	}
	ml.Phase = PhasePlaying
	ml.Started = time.Now()
	if ml.onStart != nil {
		ml.onStart()
	}
}

// End transitions from Playing to Ended.
func (ml *MatchLifecycle) End(winnerFaction uint8, reason string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.Phase != PhasePlaying {
		return
	}
	ml.Phase = PhaseEnded
	ml.Ended = time.Now()
	ml.WinnerFaction = winnerFaction
	ml.WinReason = reason
	if ml.onEnd != nil {
		ml.onEnd(winnerFaction, reason)
	}
}

// ShouldFlush returns true if the match has been ended for longer than FlushSec.
func (ml *MatchLifecycle) ShouldFlush() bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.Phase != PhaseEnded {
		return false
	}
	return time.Since(ml.Ended) >= time.Duration(ml.FlushSec)*time.Second
}

// Duration returns how long the match has been running (or ran).
func (ml *MatchLifecycle) Duration() time.Duration {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.Phase == PhasePlaying {
		return time.Since(ml.Started)
	}
	if ml.Phase == PhaseEnded {
		return ml.Ended.Sub(ml.Started)
	}
	return 0
}
