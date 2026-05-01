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
		"../client",                         // running from server/cmd/server/
		"../../client",                      // running from server/
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

	// 2. Declare hub early so callbacks can reference it
	var hub *network.Hub

	// 3. Create matchmaker — on match, spawn squads for each player
	mm := game.NewMatchmaker(func(players []game.QueuePlayer) {
		log.Printf("Match found with %d players!", len(players))
		for i, p := range players {
			playerID := uint32(i + 1)
			// Spawn 2 squads per player
			gs.SpawnSquad(playerID, uint32(2*i+1),
				fixed.FromFloat(float64(10+40*i)), fixed.FromFloat(10), 8)
			gs.SpawnSquad(playerID, uint32(2*i+2),
				fixed.FromFloat(float64(15+40*i)), fixed.FromFloat(10), 8)

			// Send match_found to this player
			mw, mh := gs.MapSize()
			hub.SendJSON(p.ClientID, map[string]interface{}{
				"type":      "match_found",
				"player_id": playerID,
				"players":   len(players),
				"map_w":     mw,
				"map_h":     mh,
			})
			// Send map terrain data as binary
			hub.SendToClient(p.ClientID, append([]byte{0xFF, 0xFE}, gs.MapData()...))
		}
	})

	// 4. Create WebSocket Hub with command dispatch and text message handler
	hub = network.NewHub(
		func(clientID uint32, cmd *network.Command) {
			gs.HandleCommand(clientID, cmd)
		},
		func(clientID uint32, msg map[string]interface{}) {
			msgType, _ := msg["type"].(string)
			switch msgType {
			case "login":
				name, _ := msg["name"].(string)
				hub.SetClientName(clientID, name)
				hub.SendJSON(clientID, map[string]string{"type": "login_ok"})
				log.Printf("client %d logged in as %q", clientID, name)
			case "join_queue":
				name := hub.GetClientName(clientID)
				if mm.Join(clientID, name) {
					hub.SendJSON(clientID, map[string]interface{}{
						"type":  "queue_joined",
						"count": mm.QueueLen(),
					})
					log.Printf("client %d (%s) joined queue (count=%d)", clientID, name, mm.QueueLen())
				}
			case "leave_queue":
				mm.Leave(clientID)
				hub.SendJSON(clientID, map[string]string{"type": "queue_left"})
				log.Printf("client %d left queue", clientID)
			case "start_solo":
				name := hub.GetClientName(clientID)
				log.Printf("client %d (%s) starting solo game", clientID, name)
				gs.SpawnSquad(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 8)
				gs.SpawnSquad(1, 2, fixed.FromFloat(15), fixed.FromFloat(10), 8)
				gs.SpawnSquad(2, 3, fixed.FromFloat(50), fixed.FromFloat(50), 8)
				gs.SpawnSquad(2, 4, fixed.FromFloat(45), fixed.FromFloat(50), 8)
				mw, mh := gs.MapSize()
				hub.SendJSON(clientID, map[string]interface{}{
					"type":      "match_found",
					"player_id": uint32(1),
					"players":   1,
					"map_w":     mw,
					"map_h":     mh,
				})
				hub.SendToClient(clientID, append([]byte{0xFF, 0xFE}, gs.MapData()...))
			}
		},
	)

	// 5. Start 15Hz game loop
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
			// Tick the matchmaker to check for matches
			mm.Tick(2)

			gs.Tick()

			data := gs.GenerateSnapshot(0, fullView)
			if data != nil {
				hub.Broadcast(data)
			}
		}
	}()

	// 6. Serve static client files on "/"
	clientDir := resolveClientDir()
	if clientDir != "" {
		fs := http.FileServer(http.Dir(clientDir))
		http.Handle("/", fs)
		log.Printf("Client files served from: %s", clientDir)
	}

	// 7. Start server — Serve() registers /ws and calls http.ListenAndServe
	addr := ":8090"
	log.Printf("Paper War server starting on %s", addr)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws", addr)
	log.Printf("Client files served from: %s", clientDir)
	if err := network.Serve(addr, hub); err != nil {
		log.Fatal(err)
	}
}
