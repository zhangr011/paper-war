package game

import (
	"sync"
)

type QueuePlayer struct {
	ClientID uint32
	Name     string
}

type Matchmaker struct {
	mu      sync.Mutex
	queue   []QueuePlayer
	onMatch func(players []QueuePlayer)
}

func NewMatchmaker(onMatch func(players []QueuePlayer)) *Matchmaker {
	return &Matchmaker{onMatch: onMatch}
}

// Join adds a player to the matchmaking queue.
func (m *Matchmaker) Join(clientID uint32, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Check not already in queue
	for _, p := range m.queue {
		if p.ClientID == clientID {
			return false
		}
	}
	m.queue = append(m.queue, QueuePlayer{ClientID: clientID, Name: name})
	return true
}

// Leave removes a player from the queue.
func (m *Matchmaker) Leave(clientID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.queue {
		if p.ClientID == clientID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

// QueueLen returns the number of players in queue.
func (m *Matchmaker) QueueLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// Tick checks if enough players are queued to start a match.
// matchSize is the number of players needed (e.g., 2).
func (m *Matchmaker) Tick(matchSize int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) >= matchSize {
		players := make([]QueuePlayer, matchSize)
		copy(players, m.queue[:matchSize])
		m.queue = m.queue[matchSize:]
		if m.onMatch != nil {
			m.onMatch(players)
		}
	}
}
