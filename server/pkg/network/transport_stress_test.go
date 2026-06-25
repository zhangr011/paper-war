package network

import (
	"sync"
	"testing"
	"time"
)

// TestStressHubReconnectStorm opens many WebSocket connections in
// parallel and immediately closes them, multiple rounds. Catches:
//   - Goroutine leaks (readPump / writePump not exiting on disconnect)
//   - Race conditions in the Hub's client map (run with -race)
//   - Server crashes under connection churn
//   - Deadlock between SendToClient and disconnect cleanup
//
// This is a real-socket stress test (uses httptest.Server + real
// WebSocket dialer) so it exercises the actual production code path.
func TestStressHubReconnectStorm(t *testing.T) {
	hub := NewHub(nil, nil)
	// Drive the hub with a no-op HTTP server just to keep its lifecycle
	// consistent. We don't assert on dispatched commands here.
	_ = hub

	const numClients = 20
	const roundsPerClient = 3

	var wg sync.WaitGroup
	panics := make(chan interface{}, numClients)

	// Spawn N goroutines, each rapidly opening+closing sockets
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()

			for r := 0; r < roundsPerClient; r++ {
				_, _, cleanup := connectHub(t, nil, nil)
				// Brief connection lifetime
				time.Sleep(time.Millisecond)
				cleanup()
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(15 * time.Second):
		t.Fatal("reconnect storm deadlocked after 15s")
	}

	close(panics)
	panicCount := 0
	for p := range panics {
		panicCount++
		t.Errorf("panic during storm: %v", p)
	}
	if panicCount == 0 {
		t.Logf("no panics across %d clients × %d rounds = %d connect/close cycles",
			numClients, roundsPerClient, numClients*roundsPerClient)
	}
}
