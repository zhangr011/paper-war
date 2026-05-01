package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/game"
	"github.com/user/paper-war/server/pkg/network"
)

func main() {
	// 1. Initialize game session (64x64 map, ECS world, all systems)
	gs := game.NewGameSession()

	// 2. Spawn test squads: 2 players, 2 squads each
	// Player 1: squads near (10,10) and (15,10)
	gs.SpawnSquad(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 8)
	gs.SpawnSquad(1, 2, fixed.FromFloat(15), fixed.FromFloat(10), 8)
	// Player 2: squads near (50,50) and (45,50)
	gs.SpawnSquad(2, 3, fixed.FromFloat(50), fixed.FromFloat(50), 8)
	gs.SpawnSquad(2, 4, fixed.FromFloat(45), fixed.FromFloat(50), 8)

	log.Println("Spawned 4 test squads (2 per player)")

	// 3. Create WebSocket Hub with command dispatch
	hub := network.NewHub(func(clientID uint32, cmd *network.Command) {
		gs.HandleCommand(clientID, cmd)
	})

	// 4. Start 15Hz game loop
	go func() {
		ticker := time.NewTicker(time.Second / 15)
		defer ticker.Stop()

		// Full-map view rect for broadcast snapshots (no per-client culling yet)
		fullView := network.Rect{
			X: 0, Y: 0,
			W: fixed.FromFloat(64),
			H: fixed.FromFloat(64),
		}

		for range ticker.C {
			gs.Tick()

			data := gs.GenerateSnapshot(0, fullView)
			if data != nil {
				hub.Broadcast(data)
			}
		}
	}()

	// 5. Serve static client files on "/"
	// Resolve client directory relative to this binary's source location.
	execPath, _ := os.Executable()
	clientDir := filepath.Join(filepath.Dir(execPath), "..", "..", "client")
	if info, err := os.Stat(clientDir); err != nil || !info.IsDir() {
		// Fallback: try relative to working directory
		clientDir = filepath.Join("..", "..", "client")
		if info, err := os.Stat(clientDir); err != nil || !info.IsDir() {
			log.Printf("Warning: client directory not found at %s (static file serving disabled)", clientDir)
		}
	}
	fs := http.FileServer(http.Dir(clientDir))
	http.Handle("/", fs)

	// 6. Start server — Serve() registers /ws and calls http.ListenAndServe
	addr := ":8080"
	log.Printf("Paper War server starting on %s", addr)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws", addr)
	log.Printf("Client files served from: %s", clientDir)
	if err := network.Serve(addr, hub); err != nil {
		log.Fatal(err)
	}
}
