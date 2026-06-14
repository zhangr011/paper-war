package game

import (
	"testing"
	"time"
)

func TestIssueAndValidateToken(t *testing.T) {
	r := NewMatchRegistry()
	token := r.IssueToken(1)
	if token == "" {
		t.Fatal("token is empty")
	}
	pid, ok := r.Validate(token)
	if !ok {
		t.Fatal("valid token rejected")
	}
	if pid != 1 {
		t.Errorf("playerID = %d, want 1", pid)
	}
}

func TestValidateUnknownToken(t *testing.T) {
	r := NewMatchRegistry()
	_, ok := r.Validate("bogus")
	if ok {
		t.Error("unknown token accepted")
	}
}

func TestTokenExpires(t *testing.T) {
	r := NewMatchRegistry()
	now := time.Now()
	r.SetNowFunc(func() time.Time { return now })
	r.SetTTL(1 * time.Second)

	token := r.IssueToken(1)

	// Advance past TTL
	r.SetNowFunc(func() time.Time { return now.Add(2 * time.Second) })

	_, ok := r.Validate(token)
	if ok {
		t.Error("expired token accepted")
	}
	if r.TokenCount() != 0 {
		t.Errorf("after expiry, token count = %d, want 0 (should be reaped)", r.TokenCount())
	}
}

func TestIssueReplacesOldToken(t *testing.T) {
	r := NewMatchRegistry()
	t1 := r.IssueToken(1)
	t2 := r.IssueToken(1)
	if t1 == t2 {
		t.Fatal("re-issue returned identical token")
	}
	// Old token should be invalid
	_, ok := r.Validate(t1)
	if ok {
		t.Error("old token still valid after re-issue")
	}
	// New token should be valid
	_, ok = r.Validate(t2)
	if !ok {
		t.Error("new token rejected")
	}
	if r.TokenCount() != 1 {
		t.Errorf("token count = %d, want 1", r.TokenCount())
	}
}

func TestMultiplePlayers(t *testing.T) {
	r := NewMatchRegistry()
	t1 := r.IssueToken(1)
	t2 := r.IssueToken(2)

	pid1, ok := r.Validate(t1)
	if !ok || pid1 != 1 {
		t.Errorf("token1: pid=%d ok=%v, want 1/true", pid1, ok)
	}
	pid2, ok := r.Validate(t2)
	if !ok || pid2 != 2 {
		t.Errorf("token2: pid=%d ok=%v, want 2/true", pid2, ok)
	}
	if r.TokenCount() != 2 {
		t.Errorf("token count = %d, want 2", r.TokenCount())
	}
}

func TestClearWipesAllTokens(t *testing.T) {
	r := NewMatchRegistry()
	r.IssueToken(1)
	r.IssueToken(2)
	r.Clear()
	if r.TokenCount() != 0 {
		t.Errorf("after Clear, token count = %d, want 0", r.TokenCount())
	}
}

func TestClearInvalidatesOldTokens(t *testing.T) {
	r := NewMatchRegistry()
	token := r.IssueToken(1)
	r.Clear()
	_, ok := r.Validate(token)
	if ok {
		t.Error("token accepted after Clear")
	}
}

func TestTokenIsHex(t *testing.T) {
	r := NewMatchRegistry()
	token := r.IssueToken(1)
	if len(token) != 32 {
		t.Errorf("token length = %d, want 32 (16 bytes hex-encoded)", len(token))
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("token contains non-hex char %q", c)
			break
		}
	}
}

// Reject empty / blank tokens — these come from malformed client messages.
func TestValidateEmptyToken(t *testing.T) {
	r := NewMatchRegistry()
	for _, bad := range []string{"", "   ", "\t\n"} {
		if _, ok := r.Validate(bad); ok {
			t.Errorf("blank token %q was accepted", bad)
		}
	}
}

// TTL boundary: a token validated exactly at the TTL instant is still valid;
// one nanosecond later it is not. This protects against off-by-one reaping.
func TestTokenTTLBoundary(t *testing.T) {
	r := NewMatchRegistry()
	now := time.Now()
	r.SetNowFunc(func() time.Time { return now })
	r.SetTTL(60 * time.Second)

	token := r.IssueToken(1)

	// Exactly at TTL — should still be valid (<= comparison)
	r.SetNowFunc(func() time.Time { return now.Add(60 * time.Second) })
	if _, ok := r.Validate(token); !ok {
		t.Error("token rejected exactly at TTL boundary")
	}

	// One nanosecond past TTL — must be rejected
	r.SetNowFunc(func() time.Time { return now.Add(60*time.Second + 1) })
	if _, ok := r.Validate(token); ok {
		t.Error("token accepted past TTL boundary")
	}
}

// Concurrent issue + validate must be safe under the race detector.
func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewMatchRegistry()
	done := make(chan struct{})

	// Issuer goroutine
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.IssueToken(uint32(i%5 + 1))
		}
	}()

	// Validator goroutine (runs concurrently with issuer)
	for i := 0; i < 100; i++ {
		r.Validate("deadbeef") // unknown token, exercises read path
	}
	<-done
}

// A re-issued token for the same player must not collide with tokens issued
// to other players between the two IssueToken calls.
func TestReissueDoesNotClobberOthers(t *testing.T) {
	r := NewMatchRegistry()
	t1 := r.IssueToken(1)
	t2 := r.IssueToken(2) // other player issues in between
	t3 := r.IssueToken(1) // player 1 re-issues

	// Player 1's old token is dead, new one is live
	if _, ok := r.Validate(t1); ok {
		t.Error("player 1 old token still valid")
	}
	if _, ok := r.Validate(t3); !ok {
		t.Error("player 1 new token rejected")
	}
	// Player 2's token is unaffected
	pid, ok := r.Validate(t2)
	if !ok || pid != 2 {
		t.Errorf("player 2 token clobbered: pid=%d ok=%v", pid, ok)
	}
}
