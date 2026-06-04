package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/fixed"
	"github.com/user/paper-war/server/pkg/game"
	"github.com/user/paper-war/server/pkg/network"
	"github.com/user/paper-war/server/pkg/persist"
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
	// 1. Initialize game session (portrait map, ECS world, all systems)
	gs := game.NewGameSession()

	// 1b. Persistence: connect to PostgreSQL if DATABASE_URL is set
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		ctx := context.Background()
		pgStore, err := persist.NewPostgresStore(ctx, dbURL)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		gs.Store = pgStore
		log.Printf("Connected to PostgreSQL (DATABASE_URL set)")
	} else {
		// Dev mode: in-memory store
		gs.Store = persist.NewMockStore()
		log.Printf("No DATABASE_URL — using in-memory MockStore")
	}

	// 2. Declare hub early so callbacks can reference it
	var hub *network.Hub

	// 3. Create matchmaker — on match, spawn squads for each player
	mm := game.NewMatchmaker(func(players []game.QueuePlayer) {
		log.Printf("Match found with %d players!", len(players))
		gs.Reset()
		for i, p := range players {
			playerID := uint32(i + 1)
			hub.SetClientPlayerID(p.ClientID, playerID)
			// Spawn 2 squads per player
			spawnSquadsForPlayer(gs, playerID, i, len(players))

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
			hub.SendToClient(p.ClientID, append([]byte{0xFF, 0xFD}, gs.MapData()...))
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
			// Read commander type selection (0=LI, 1=HI, 2=Sniper, 3=AAI)
			cmdType := uint8(0) // default LightInfantry
			if ct, ok := msg["commander_type"].(float64); ok && ct >= 0 && ct <= 3 {
				cmdType = uint8(ct)
			}
			log.Printf("client %d (%s) starting solo game (cmdType=%d)", clientID, name, cmdType)
			gs.Reset()
			hub.SetClientPlayerID(clientID, 1)
			hub.SetClientInGame(clientID, true)

			// Player 1: spawn from roster if Store is available
			if gs.Store != nil {
				spawnFromStore(gs, 1, 0, 2, cmdType)
			} else {
				spawnSquadsForPlayerWithCmdType(gs, 1, 0, 2, cmdType)
			}
			// AI player (player 2): always default spawn
			spawnSquadsForPlayer(gs, 2, 1, 2)
			mw, mh := gs.MapSize()
			hub.SendJSON(clientID, map[string]interface{}{
				"type":      "match_found",
				"player_id": uint32(1),
				"players":   1,
				"map_w":     mw,
				"map_h":     mh,
			})
			hub.SendToClient(clientID, append([]byte{0xFF, 0xFD}, gs.MapData()...))
		case "start_clash":
			log.Printf("client %d starting clash test", clientID)

			// Parse team sizes from message
			t1Units := 10
			t2Units := 10
			if v, ok := msg["team1_units"]; ok {
				if f, ok := v.(float64); ok && f >= 1 && f <= 40 {
					t1Units = int(f)
				}
			}
			if v, ok := msg["team2_units"]; ok {
				if f, ok := v.(float64); ok && f >= 1 && f <= 40 {
					t2Units = int(f)
				}
			}

			// Parse terrain preset and determine seed
			terrainPreset := "random"
			if v, ok := msg["terrain"]; ok {
				if s, ok := v.(string); ok {
					terrainPreset = s
				}
			}
			terrainSeeds := map[string]int64{
				"random":   0, // 0 means use random seed in ResetWithSeed
				"plains":   42,
				"fortress": 777,
				"river":    1337,
			}
			seed, ok := terrainSeeds[terrainPreset]
			if !ok {
				seed = 0
			}
			if seed == 0 {
				gs.Reset()
			} else {
				gs.ResetWithSeed(seed)
			}
			gs.EnableClashMode()

			// Spectator: playerID=0 means no fog, full map visibility
			hub.SetClientPlayerID(clientID, 0)
			hub.SetClientInGame(clientID, true)

			mw, mh := gs.MapSize()
			cx1 := fixed.FromFloat(float64(mw)/2 - 2)
			cx2 := fixed.FromFloat(float64(mw)/2 + 2)
			cy := fixed.FromFloat(float64(mh) / 2)

			// Spawn teams, splitting into squads of max 10
			squadID := uint32(1)
			for t1Units > 0 {
				n := t1Units
				if n > 10 {
					n = 10
				}
				gs.SpawnSquadWithType(1, squadID, cx1, cy, n, component.UnitLightInfantry)
				t1Units -= n
				squadID++
			}
			for t2Units > 0 {
				n := t2Units
				if n > 10 {
					n = 10
				}
				gs.SpawnSquadWithType(2, squadID, cx2, cy, n, component.UnitLightInfantry)
				t2Units -= n
				squadID++
			}

			hub.SendJSON(clientID, map[string]interface{}{
				"type":      "match_found",
				"player_id": uint32(0), // spectator
				"players":   2,
				"map_w":     mw,
				"map_h":     mh,
			})
			hub.SendToClient(clientID, append([]byte{0xFF, 0xFD}, gs.MapData()...))
		}
		},
	)

	// 5. Start game loop
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("GAME LOOP PANIC: %v\n%s", r, debug.Stack())
			}
		}()
		ticker := time.NewTicker(time.Second / game.ServerTicksPerSecond)
		defer ticker.Stop()

		for range ticker.C {
			// Tick the matchmaker to check for matches
			mm.Tick(2)

			gs.Tick()

			mw, mh := gs.MapSize()
			// Full-map view rect for broadcast snapshots (no per-client culling yet)
			fullView := network.Rect{
				X: 0, Y: 0,
				W: fixed.FromFloat(float64(mw)),
				H: fixed.FromFloat(float64(mh)),
			}

		for _, cid := range hub.ClientIDs() {
			pid := hub.GetClientPlayerID(cid)
			// Only send snapshots to clients that have started a game
			if !hub.GetClientInGame(cid) {
				continue
			}
			data := gs.GenerateSnapshot(pid, fullView)
			if data != nil {
				hub.SendToClient(cid, data)
			}
		}

		// Send GoldUpdate messages for any changed gold values
		for pid, gold := range gs.GetGoldUpdates() {
			for _, cid := range hub.ClientIDs() {
				if hub.GetClientPlayerID(cid) == pid {
					hub.SendToClient(cid, network.EncodeServerMessage(&network.ServerMessage{
						Type:  network.MsgGoldUpdate,
						Gold:  gold,
					}))
				}
			}
		}

		// Send MatchResult to all in-game clients when match ends
		if gs.Lifecycle.Phase == game.PhaseEnded && !gs.Lifecycle.MatchResultSent {
			gs.Lifecycle.MatchResultSent = true
			result := network.EncodeServerMessage(&network.ServerMessage{
				Type:   network.MsgMatchResult,
				Winner: gs.Lifecycle.WinnerFaction,
				Reason: gs.Lifecycle.WinReason,
			})
			for _, cid := range hub.ClientIDs() {
				if hub.GetClientInGame(cid) {
					hub.SendToClient(cid, result)
				}
			}
		}
		}
	}()

	// 6. Serve static client files on "/"
	clientDir := resolveClientDir()
	if clientDir != "" {
		fs := http.FileServer(http.Dir(clientDir))
		http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			fs.ServeHTTP(w, r)
		}))
		log.Printf("Client files served from: %s", clientDir)
	}

	// 7. Start server — Serve() registers /ws and calls http.ListenAndServe
	addr := ":9091"
	log.Printf("Paper War server starting on %s", addr)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws", addr)
	log.Printf("Client files served from: %s", clientDir)
	if err := network.Serve(addr, hub); err != nil {
		log.Fatal(err)
	}
}

func spawnSquadsForPlayer(gs *game.GameSession, playerID uint32, playerIndex, playerCount int) {
	spawnSquadsForPlayerWithCmdType(gs, playerID, playerIndex, playerCount, 0)
}

// spawnFromStore loads the player's roster from the persistence Store and spawns from it.
// Falls back to CreateStarterRoster for new players, then uses the selected commander type.
func spawnFromStore(gs *game.GameSession, playerID uint32, playerIndex, playerCount int, cmdType uint8) {
	mw, mh := gs.MapSize()
	x1 := float64(mw) * 0.42
	x2 := float64(mw) * 0.58
	y := 10.0
	if playerCount > 1 {
		y = 10.0 + float64(mh-20)*float64(playerIndex)/float64(playerCount-1)
	}

	baseSquadID := uint32(playerIndex*2 + 1)

	ctx := context.Background()
	roster, err := gs.Store.LoadRoster(ctx, playerID)
	if err != nil || len(roster) == 0 {
		// New player — create starter roster, then reload
		gs.Store.CreateStarterRoster(ctx, playerID)
		roster, err = gs.Store.LoadRoster(ctx, playerID)
		if err != nil || len(roster) == 0 {
			// Fallback to default spawn
			ct := component.CombatUnitType(cmdType)
			gs.SpawnTeamWithType(playerID, baseSquadID, fixed.FromFloat(x1), fixed.FromFloat(y), 1, ct)
			gs.SpawnTeamWithType(playerID, baseSquadID+1, fixed.FromFloat(x2), fixed.FromFloat(y), 1, ct)
			return
		}
	}

	// Use first commander from roster
	cmd := roster[0]

	// Override commander type if player selected a different one
	if cmdType > 0 {
		typeName := component.CombatUnitTypeName(component.CombatUnitType(cmdType))
		cmd.Type = typeName
	}

	gs.SpawnTeamFromRoster(playerID, baseSquadID, fixed.FromFloat(x1), fixed.FromFloat(y), cmd)

	// Second squad with same commander (mirror)
	gs.SpawnTeamFromRoster(playerID, baseSquadID+1, fixed.FromFloat(x2), fixed.FromFloat(y), cmd)
}

func spawnSquadsForPlayerWithCmdType(gs *game.GameSession, playerID uint32, playerIndex, playerCount int, cmdType uint8) {
	mw, mh := gs.MapSize()
	x1 := float64(mw) * 0.42
	x2 := float64(mw) * 0.58
	y := 10.0
	if playerCount > 1 {
		y = 10.0 + float64(mh-20)*float64(playerIndex)/float64(playerCount-1)
	}

	baseSquadID := uint32(playerIndex*2 + 1)
	ct := component.CombatUnitType(cmdType)
	gs.SpawnTeamWithType(playerID, baseSquadID, fixed.FromFloat(x1), fixed.FromFloat(y), 1, ct)
	gs.SpawnTeamWithType(playerID, baseSquadID+1, fixed.FromFloat(x2), fixed.FromFloat(y), 1, ct)
}
