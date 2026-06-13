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
	"github.com/user/paper-war/server/pkg/tilemap"
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

	// 2b. Reconnect registry — issues tokens at match start so players can
	// re-join after a dropped connection. Tokens live for 120s after issue
	// and are cleared on every new match start / match end.
	registry := game.NewMatchRegistry()

	// 3. Create matchmaker — on match, spawn squads for each player
	mm := game.NewMatchmaker(func(players []game.QueuePlayer) {
		log.Printf("Match found with %d players!", len(players))
		registry.Clear()
		gs.Reset()
		gs.Map.Objective.Type = 0 // Force elimination
		for i, p := range players {
			playerID := uint32(i + 1)
			hub.SetClientPlayerID(p.ClientID, playerID)
			hub.SetClientInGame(p.ClientID, true)
			// Spawn 2 squads per player
			spawnSquadsForPlayer(gs, playerID, i, len(players))

			// Issue reconnect token
			token := registry.IssueToken(playerID)

			// Send match_found to this player
			mw, mh := gs.MapSize()
			hub.SendJSON(p.ClientID, map[string]interface{}{
				"type":            "match_found",
				"player_id":       playerID,
				"players":         len(players),
				"map_w":           mw,
				"map_h":           mh,
				"reconnect_token": token,
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
			registry.Clear()
			gs.Reset()
			// Force elimination objective for all modes
			gs.Map.Objective.Type = 0 // ObjectiveElimination = 0
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
			token := registry.IssueToken(1)
			mw, mh := gs.MapSize()
			hub.SendJSON(clientID, map[string]interface{}{
				"type":            "match_found",
				"player_id":       uint32(1),
				"players":         1,
				"map_w":           mw,
				"map_h":           mh,
				"reconnect_token": token,
			})
			hub.SendToClient(clientID, append([]byte{0xFF, 0xFD}, gs.MapData()...))
		case "start_clash":
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
				"random": 0,
			}
			seed, ok := terrainSeeds[terrainPreset]
			if !ok {
				seed = 0
			}
			// Try dedicated clash map first, then fallback to procedural
			if clashMap := tilemap.LoadClashMap(terrainPreset); clashMap != nil {
				gs.ResetWithMap(clashMap)
			} else if seed == 0 {
				gs.Reset()
			} else {
				gs.ResetWithSeed(seed)
			}
			gs.EnableClashMode()
			// Force elimination objective for clash mode
			gs.Map.Objective.Type = 0 // ObjectiveElimination = 0

			// Spectator: playerID=0 means no fog, full map visibility
			hub.SetClientPlayerID(clientID, 0)
			hub.SetClientInGame(clientID, true)

			// Parse commander types
			t1Cmd := component.UnitLightInfantry
			t2Cmd := component.UnitLightInfantry
			if v, ok := msg["team1_commander"]; ok {
				if f, ok := v.(float64); ok && f >= 0 && f <= 3 {
					t1Cmd = component.CombatUnitType(int(f))
				}
			}
			if v, ok := msg["team2_commander"]; ok {
				if f, ok := v.(float64); ok && f >= 0 && f <= 3 {
					t2Cmd = component.CombatUnitType(int(f))
				}
			}

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
				gs.SpawnSquadWithType(1, squadID, cx1, cy, n, t1Cmd)
				t1Units -= n
				squadID++
			}
			for t2Units > 0 {
				n := t2Units
				if n > 10 {
					n = 10
				}
				gs.SpawnSquadWithType(2, squadID, cx2, cy, n, t2Cmd)
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
		case "reconnect":
			// Re-bind a new WebSocket connection to an existing in-progress match.
			// The client sends the token it received in match_found; the server
			// validates it, restores playerID + InGame on the new clientID, and
			// re-sends match_found + map data so the client can re-initialize.
			token, _ := msg["token"].(string)
			if token == "" {
				hub.SendJSON(clientID, map[string]interface{}{
					"type":   "reconnect_failed",
					"reason": "missing_token",
				})
				return
			}
			playerID, ok := registry.Validate(token)
			if !ok {
				hub.SendJSON(clientID, map[string]interface{}{
					"type":   "reconnect_failed",
					"reason": "invalid_or_expired",
				})
				log.Printf("client %d: reconnect failed (invalid/expired token)", clientID)
				return
			}
			// Only allow reconnect while a match is in progress
			if gs.Lifecycle.Phase == game.PhaseEnded {
				hub.SendJSON(clientID, map[string]interface{}{
					"type":   "reconnect_failed",
					"reason": "match_ended",
				})
				log.Printf("client %d: reconnect failed (match already ended)", clientID)
				return
			}
			// Re-bind: set the new clientID's playerID and InGame flag.
			hub.SetClientPlayerID(clientID, playerID)
			hub.SetClientInGame(clientID, true)
			log.Printf("client %d reconnected as player %d", clientID, playerID)

			// Re-send match_found + map data so client can re-init
			mw, mh := gs.MapSize()
			hub.SendJSON(clientID, map[string]interface{}{
				"type":            "reconnect_ok",
				"player_id":       playerID,
				"map_w":           mw,
				"map_h":           mh,
				"reconnect_token": token, // refresh token so another reconnect is possible
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
			registry.Clear() // revoke all reconnect tokens — match is over
			result := network.EncodeServerMessage(&network.ServerMessage{
				Type:   network.MsgMatchResult,
				Winner: gs.Lifecycle.WinnerFaction,
				Reason: gs.Lifecycle.WinReason,
			})
			// AAR: send match statistics right after result
			ms := gs.GetMatchStats()
			statsMsg := network.EncodeServerMessage(&network.ServerMessage{
				Type: network.MsgMatchStats,
				Stats: [2]network.MatchStatsEntry{
					{
						Kills:          ms.Factions[0].Kills,
						Deaths:         ms.Factions[0].Deaths,
						CommanderKills: ms.Factions[0].CommanderKills,
						UnitsRecruited: ms.Factions[0].UnitsRecruited,
						GoldEarned:     ms.Factions[0].GoldEarned,
						GoldSpent:      ms.Factions[0].GoldSpent,
					},
					{
						Kills:          ms.Factions[1].Kills,
						Deaths:         ms.Factions[1].Deaths,
						CommanderKills: ms.Factions[1].CommanderKills,
						UnitsRecruited: ms.Factions[1].UnitsRecruited,
						GoldEarned:     ms.Factions[1].GoldEarned,
						GoldSpent:      ms.Factions[1].GoldSpent,
					},
				},
			})
			for _, cid := range hub.ClientIDs() {
				if hub.GetClientInGame(cid) {
					hub.SendToClient(cid, result)
					hub.SendToClient(cid, statsMsg)
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
