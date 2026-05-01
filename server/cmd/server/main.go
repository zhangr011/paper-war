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

// resolveClientDir tries several paths to find the client directory.
func resolveClientDir() string {
	candidates := []string{
		"../client",                       // running from server/cmd/server/
		"../../client",                    // running from server/
		filepath.Join("..", "..", "client"), // fallback
	}
	// Also try relative to executable (for `go run`)
	if execPath, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(execPath), "..", "..", "client"),
		)
	}
	for _, dir := range candidates {
		abs, _ := filepath.Abs(dir)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	log.Println("Warning: client directory not found (static file serving disabled)")
	return ""
}

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
	clientDir := resolveClientDir()
	if clientDir != "" {
		fs := http.FileServer(http.Dir(clientDir))
		http.Handle("/", fs)
		log.Printf("Client files served from: %s", clientDir)
	}

	// 6. Start server — Serve() registers /ws and calls http.ListenAndServe
	addr := ":8090"
	log.Printf("Paper War server starting on %s", addr)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws", addr)
	log.Printf("Client files served from: %s", clientDir)
	if err := network.Serve(addr, hub); err != nil {
		log.Fatal(err)
	}
}
