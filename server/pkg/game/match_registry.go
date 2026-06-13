package game

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultReconnectTTL is how long a reconnect token remains valid after issue.
// The game keeps running during this window; the player's units sit idle until
// they reconnect or the token expires.
const DefaultReconnectTTL = 120 * time.Second

// tokenEntry tracks one issued reconnect token.
type tokenEntry struct {
	playerID  uint32
	expiresAt time.Time
}

// MatchRegistry issues and validates reconnect tokens so a player can re-join
// an in-progress match after a dropped connection. There is one registry per
// server process. Tokens are cleared whenever a new match starts or the current
// match ends, so stale tokens from a previous match can never be reused.
type MatchRegistry struct {
	mu      sync.RWMutex
	tokens  map[string]*tokenEntry
	ttl     time.Duration
	nowFunc func() time.Time // injectable for tests
}

// NewMatchRegistry returns a registry with the default 120s token TTL.
func NewMatchRegistry() *MatchRegistry {
	return &MatchRegistry{
		tokens:  make(map[string]*tokenEntry),
		ttl:     DefaultReconnectTTL,
		nowFunc: time.Now,
	}
}

// IssueToken generates a new reconnect token for playerID and stores it.
// Any previous token for this player is replaced. Returns the hex token string.
func (r *MatchRegistry) IssueToken(playerID uint32) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand should never fail; fall back to time-based seed to avoid panic
		raw = []byte(time.Now().Format("20060102150405000000000000"))
	}
	token := hex.EncodeToString(raw)

	r.mu.Lock()
	defer r.mu.Unlock()
	// Remove any old token for this player (linear scan — token count is tiny: 1-2)
	for k, v := range r.tokens {
		if v.playerID == playerID {
			delete(r.tokens, k)
		}
	}
	r.tokens[token] = &tokenEntry{
		playerID:  playerID,
		expiresAt: r.nowFunc().Add(r.ttl),
	}
	return token
}

// Validate checks whether token is valid and unexpired. Returns the bound
// playerID and true on success, or (0, false) if the token is unknown or
// expired. Expired tokens are reaped on access.
func (r *MatchRegistry) Validate(token string) (uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tokens[token]
	if !ok {
		return 0, false
	}
	if r.nowFunc().After(entry.expiresAt) {
		delete(r.tokens, token)
		return 0, false
	}
	return entry.playerID, true
}

// Clear removes all tokens. Called on match start and match end.
func (r *MatchRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens = make(map[string]*tokenEntry)
}

// TokenCount returns the number of active (not necessarily unexpired) tokens.
// Useful for testing.
func (r *MatchRegistry) TokenCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tokens)
}

// SetTTL overrides the token TTL. Must be called before issuing tokens.
// Intended for testing.
func (r *MatchRegistry) SetTTL(ttl time.Duration) {
	r.ttl = ttl
}

// SetNowFunc overrides the clock. Intended for testing.
func (r *MatchRegistry) SetNowFunc(f func() time.Time) {
	r.nowFunc = f
}
