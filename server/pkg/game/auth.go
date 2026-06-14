package game

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// TokenType represents a player authentication token.
type TokenType string

// TokenStore manages player tokens.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[TokenType]uint32 // token -> playerID
}

func NewTokenStore() *TokenStore {
	return &TokenStore{
		tokens: make(map[TokenType]uint32),
	}
}

// GenerateToken creates a new token for the given player ID.
func (ts *TokenStore) GenerateToken(playerID uint32) TokenType {
	b := make([]byte, 16)
	rand.Read(b)
	token := TokenType(hex.EncodeToString(b))
	ts.mu.Lock()
	ts.tokens[token] = playerID
	ts.mu.Unlock()
	return token
}

// ValidateToken returns the player ID for a valid token, or 0 if invalid.
func (ts *TokenStore) ValidateToken(token TokenType) uint32 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.tokens[token]
}

// RevokeToken removes a token.
func (ts *TokenStore) RevokeToken(token TokenType) {
	ts.mu.Lock()
	delete(ts.tokens, token)
	ts.mu.Unlock()
}
