package game

import (
	"testing"
	"time"
)

// TestMatchRegistryIssueAndValidate verifies the basic happy path:
// issue a token, validate it, get the same playerID back.
func TestMatchRegistryIssueAndValidate(t *testing.T) {
	r := NewMatchRegistry()
	token := r.IssueToken(42)

	if token == "" {
		t.Fatal("IssueToken returned empty string")
	}
	if len(token) != 32 { // 16 bytes hex-encoded = 32 chars
		t.Errorf("token length = %d, want 32 (16 bytes hex)", len(token))
	}

	pid, ok := r.Validate(token)
	if !ok {
		t.Fatal("Validate returned false for fresh token")
	}
	if pid != 42 {
		t.Errorf("playerID = %d, want 42", pid)
	}
}

// TestMatchRegistryUnknownToken verifies that a never-issued token is rejected.
func TestMatchRegistryUnknownToken(t *testing.T) {
	r := NewMatchRegistry()
	pid, ok := r.Validate("deadbeef")
	if ok {
		t.Error("Validate should return false for unknown token")
	}
	if pid != 0 {
		t.Errorf("playerID for unknown token = %d, want 0", pid)
	}
}

// TestMatchRegistryExpiredTokenRejected verifies that tokens past their
// TTL are rejected and reaped from the registry.
func TestMatchRegistryExpiredTokenRejected(t *testing.T) {
	r := NewMatchRegistry()
	r.SetTTL(1 * time.Millisecond)
	token := r.IssueToken(7)

	// Advance virtual clock past TTL
	now := time.Now()
	r.SetNowFunc(func() time.Time { return now.Add(1 * time.Hour) })

	pid, ok := r.Validate(token)
	if ok {
		t.Error("Validate should return false for expired token")
	}
	if pid != 0 {
		t.Errorf("playerID for expired token = %d, want 0", pid)
	}
	// Token should also be reaped from the registry
	if r.TokenCount() != 0 {
		t.Errorf("after expiry + Validate, TokenCount = %d, want 0 (reaped)", r.TokenCount())
	}
}

// TestMatchRegistryClearWipesAllTokens verifies that Clear removes all
// issued tokens. This is called on match start and match end to revoke
// stale tokens from the previous match.
func TestMatchRegistryClearWipesAllTokens(t *testing.T) {
	r := NewMatchRegistry()
	r.IssueToken(1)
	r.IssueToken(2)
	r.IssueToken(3)
	if r.TokenCount() != 3 {
		t.Fatalf("before Clear: TokenCount = %d, want 3", r.TokenCount())
	}

	r.Clear()

	if r.TokenCount() != 0 {
		t.Errorf("after Clear: TokenCount = %d, want 0", r.TokenCount())
	}
	// Previously-issued tokens should no longer validate
	pid, ok := r.Validate("anything")
	if ok || pid != 0 {
		t.Errorf("after Clear, Validate should return (0,false), got (%d,%v)", pid, ok)
	}
}

// TestMatchRegistryIssueReplacesPrevious verifies that issuing a new
// token for an existing player removes their old token (so they can't
// reconnect with both).
func TestMatchRegistryIssueReplacesPrevious(t *testing.T) {
	r := NewMatchRegistry()
	t1 := r.IssueToken(5)
	if r.TokenCount() != 1 {
		t.Fatalf("after first IssueToken: TokenCount = %d, want 1", r.TokenCount())
	}
	t2 := r.IssueToken(5)
	if r.TokenCount() != 1 {
		t.Errorf("after second IssueToken: TokenCount = %d, want 1 (replaced)", r.TokenCount())
	}
	if t1 == t2 {
		t.Error("two IssueToken calls returned the same token string — should be unique")
	}
	// Old token should be invalid
	if _, ok := r.Validate(t1); ok {
		t.Error("old token still validates after re-issue")
	}
	// New token should be valid
	if _, ok := r.Validate(t2); !ok {
		t.Error("new token fails to validate after re-issue")
	}
}

// TestMatchRegistryUniqueTokens verifies that tokens generated for
// different players are unique. (Crypto/rand should make collisions
// astronomically unlikely; this test guards against a future regression
// that uses a deterministic source.)
func TestMatchRegistryUniqueTokens(t *testing.T) {
	r := NewMatchRegistry()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok := r.IssueToken(uint32(i))
		if seen[tok] {
			t.Fatalf("duplicate token generated on iteration %d", i)
		}
		seen[tok] = true
	}
	if len(seen) != 100 {
		t.Errorf("got %d unique tokens, want 100", len(seen))
	}
}

// TestMatchRegistryConcurrentIssueAndValidate verifies the registry
// is safe under concurrent access (run with -race to catch data races).
func TestMatchRegistryConcurrentIssueAndValidate(t *testing.T) {
	r := NewMatchRegistry()
	done := make(chan struct{})
	const workers = 8

	// Half the workers issue tokens, half validate random strings
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				if id%2 == 0 {
					r.IssueToken(uint32(id*1000 + j))
				} else {
					r.Validate("bogus-token")
				}
			}
		}(i)
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	// No data race / no panic = success. Run with: go test -race
}

// TestMatchRegistryTokenCountAfterExpiryNoValidate verifies that an
// expired-but-not-yet-Validated token still counts (reaping is lazy,
// only happens on Validate). This documents the behavior.
func TestMatchRegistryTokenCountAfterExpiryNoValidate(t *testing.T) {
	r := NewMatchRegistry()
	r.SetTTL(1 * time.Millisecond)
	r.IssueToken(1)

	// Advance virtual clock past TTL
	now := time.Now()
	r.SetNowFunc(func() time.Time { return now.Add(1 * time.Hour) })

	// Without calling Validate, the expired token is still counted
	if r.TokenCount() != 1 {
		t.Errorf("TokenCount = %d, want 1 (lazy reaping — token exists until Validate)", r.TokenCount())
	}
}
