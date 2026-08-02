package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/user/paper-war/server/pkg/component"
	"github.com/user/paper-war/server/pkg/ecs"
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
	// Seed the global RNG so each match plays out differently.
	// Without this, Go defaults to seed=1 and every clash match is
	// a byte-for-byte identical replay.
	//
	// Issue #47 Fix A: in test mode (PAPER_WAR_TEST_SEED set), pin the
	// global RNG to the same seed so AI behavior is also deterministic.
	// Production leaves the env var unset and gets time-based seeding.
	if v := os.Getenv("PAPER_WAR_TEST_SEED"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			log.Printf("test mode: pinning global RNG seed to %d (PAPER_WAR_TEST_SEED)", parsed)
			rand.Seed(parsed)
		} else {
			log.Printf("PAPER_WAR_TEST_SEED=%q invalid (%v) — using time-based seed", v, err)
			rand.Seed(time.Now().UnixNano())
		}
	} else {
		rand.Seed(time.Now().UnixNano())
	}

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
			// Translate clientID → playerID using the Hub's mapping
			// (SetClientPlayerID is called on join/match-start).  Without
			// this, HandleCommand received the raw clientID (a connection
			// counter) and treated it as a playerID — which mismatched
			// the OwnerComponent.PlayerID on units (set via SpawnTeam
			// using the playerID) and the PlayerSpawns map.  Result:
			// recruit/build commands looked up the wrong player's data
			// (often the AI's).  Issue found in QA pass.
			playerID := hub.GetClientPlayerID(clientID)
			if playerID == 0 {
				// Client hasn't joined a match yet — drop the command.
				return
			}
			gs.HandleCommand(playerID, cmd)
		},
		func(clientID uint32, msg map[string]interface{}) {
			msgType, _ := msg["type"].(string)
			switch msgType {
			case "login":
				name, _ := msg["name"].(string)
				token, _ := msg["token"].(string)
				hub.SetClientName(clientID, name)

				// v1.1: resolve real DB playerID from token. Without this
				// every client shares playerID=1 in solo/clash and the
				// roster is shared. The client generates and persists the
				// token in localStorage on first login (opaque hex string).
				if token != "" && gs.Store != nil {
					loginCtx := context.Background()
					player, err := gs.Store.FindOrCreatePlayer(loginCtx, token, name)
					if err != nil {
						log.Printf("client %d: FindOrCreatePlayer error: %v", clientID, err)
					} else if player != nil {
						hub.SetClientToken(clientID, token)
						// Persist the DB-resolved playerID on the session
						// so spawnFromStore can load the right roster. Note
						// this is the *account* playerID; the *match*
						// playerID is assigned separately at match start
						// (1 or 2) by SetClientPlayerID.
						hub.SetClientPlayerID(clientID, player.ID)
						log.Printf("client %d logged in as %q (db player_id=%d)",
							clientID, name, player.ID)
					}
				} else {
					log.Printf("client %d logged in as %q (no token — using ephemeral ID)",
						clientID, name)
				}
				hub.SendJSON(clientID, map[string]interface{}{
					"type":       "login_ok",
					"player_id":  hub.GetClientPlayerID(clientID),
				})

				// v1.1: Send career stats so the client can render the
				// career screen / lobby totals immediately. For new players
				// this is all zeros.
				if gs.Store != nil {
					pid := hub.GetClientPlayerID(clientID)
					if pid > 0 {
						careerCtx := context.Background()
						if totals, err := gs.Store.GetCareerStats(careerCtx, pid); err == nil && totals != nil {
							hub.SendJSON(clientID, map[string]interface{}{
								"type":              "career_stats",
								"matches_played":    totals.MatchesPlayed,
								"matches_won":       totals.MatchesWon,
								"matches_lost":      totals.MatchesLost,
								"total_kills":       totals.TotalKills,
								"total_deaths":      totals.TotalDeaths,
								"commander_kills":   totals.CommanderKills,
								"commanders_lost":   totals.CommandersLost,
								"total_gold_earned": totals.TotalGoldEarned,
								"total_gold_spent":  totals.TotalGoldSpent,
								"total_recruits":    totals.TotalRecruits,
							})
						}
					}
				}
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
			case "get_leaderboard":
				// v1.2: client-requested leaderboard. Triggered on Career screen
				// open or manual refresh. Returns top N players by total kills.
				if gs.Store == nil {
					hub.SendJSON(clientID, map[string]interface{}{
						"type":   "leaderboard",
						"error":  "leaderboard unavailable (no persistence)",
						"entries": []interface{}{},
					})
					break
				}
				limit := persist.LeaderboardLimit
				if l, ok := msg["limit"].(float64); ok && l > 0 {
					limit = int(l)
				}
				lbCtx := context.Background()
				entries, err := gs.Store.GetLeaderboard(lbCtx, limit)
				if err != nil {
					log.Printf("client %d: GetLeaderboard error: %v", clientID, err)
					hub.SendJSON(clientID, map[string]interface{}{
						"type":    "leaderboard",
						"error":   "leaderboard query failed",
						"entries": []interface{}{},
					})
					break
				}
				// Marshal entries as array of objects — Go's encoding/json
				// handles this automatically via the struct tags.
				out := make([]map[string]interface{}, 0, len(entries))
				for _, e := range entries {
					out = append(out, map[string]interface{}{
						"rank":           e.Rank,
						"player_id":      e.PlayerID,
						"name":           e.Name,
						"matches_played": e.MatchesPlayed,
						"matches_won":    e.MatchesWon,
						"matches_lost":   e.MatchesLost,
						"total_kills":    e.TotalKills,
						"total_deaths":   e.TotalDeaths,
					})
				}
				hub.SendJSON(clientID, map[string]interface{}{
					"type":    "leaderboard",
					"entries": out,
				})
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
			// Include spawn positions so the client doesn't have to guess.
			// Without this, the client fabricates spawns at fixed y=10/mh-10
			// which disagrees with both the map generator (used for build
			// range checks) and the actual unit positions. (QA finding.)
			spawnsPayload := [][]int32{}
			if gs.Map != nil {
				for _, sp := range gs.Map.Spawns {
					spawnsPayload = append(spawnsPayload, []int32{sp[0], sp[1]})
				}
			}
			hub.SendJSON(clientID, map[string]interface{}{
				"type":            "match_found",
				"player_id":       uint32(1),
				"players":         1,
				"map_w":           mw,
				"map_h":           mh,
				"reconnect_token": token,
				"spawns":          spawnsPayload,
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
			// v1.4 clash redesign: two bases on the long axis, connected by
			// a single road. Init distance tuned to 8 tiles (was 4 at v1.0,
			// 16, then 84 in earlier drafts — 8 plays best: close enough for
			// immediate engagement, enough room for a formation phase).
			//
			// Team 1: (mw/2, mh/2 - 4)
			// Team 2: (mw/2, mh/2 + 4)
			// Init distance: 8 tiles.
			halfDist := int32(4)
			cx := fixed.FromFloat(float64(mw) / 2)
			cy1 := fixed.FromFloat(float64(mh/2 - halfDist))
			cy2 := fixed.FromFloat(float64(mh/2 + halfDist))

			// Record base positions on the map so the client (minimap
			// flags) and matchmaker (match_found payload) know where
			// the spawns are. Procedural maps set this in GenerateMap;
			// clash maps previously left it empty.
			gs.Map.Spawns = [][2]int32{
				{int32(mw) / 2, mh/2 - halfDist},
				{int32(mw) / 2, mh/2 + halfDist},
			}

			// Spawn teams, splitting into squads of max 10
			squadID := uint32(1)
			for t1Units > 0 {
				n := t1Units
				if n > 10 {
					n = 10
				}
				gs.SpawnSquadWithType(1, squadID, cx, cy1, n, t1Cmd)
				t1Units -= n
				squadID++
			}
			for t2Units > 0 {
				n := t2Units
				if n > 10 {
					n = 10
				}
				gs.SpawnSquadWithType(2, squadID, cx, cy2, n, t2Cmd)
				t2Units -= n
				squadID++
			}

			// Per-unit position jitter: a small random offset on each combat
			// unit so deterministic entity-processing-order bias doesn't
			// produce identical match outcomes every time. The bias-break
			// only needs continuous entropy, so the magnitude is kept at
			// ±0.3 tile (matching the spawn jitter in spawnCombatUnitsWithType,
			// not 3× it) — a larger value just spreads the formation.
			// Commanders are excluded: they are the formation anchor, and
			// jittering them scatters the whole squad's reference frame.
			posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
			boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
			boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
				if bc.Role == component.RoleCommander {
					return
				}
				if pos, ok := posPool.GetPtr(e); ok {
					pos.X += fixed.FromFloat(rand.Float64()*0.6 - 0.3)
					pos.Y += fixed.FromFloat(rand.Float64()*0.6 - 0.3)
				}
			})

			// March orders: point each army at the enemy spawn so a clash match
			// plays out as an advancing battle, not a static close-range shootout.
			// MoveDisabled (set by EnableClashMode) only blocks AI-ISSUED moves;
			// direct path targets still drive movement — same technique as
			// runRealisticMatchup. Team 1 (player) → cy2, team 2 (enemy) → cy1.
			pathPool := gs.World.Pool(component.PathfindingComponent{}).(*ecs.ComponentPool[component.PathfindingComponent])
			ownerPool := gs.World.Pool(component.OwnerComponent{}).(*ecs.ComponentPool[component.OwnerComponent])
			boidPool.Each(func(e ecs.Entity, bc *component.BoidComponent) {
				path, ok := pathPool.GetPtr(e)
				if !ok {
					return
				}
				path.TargetX = cx
				if owner, hasOwner := ownerPool.Get(e); hasOwner && owner.Faction == component.FactionEnemy {
					path.TargetY = cy1 // enemy marches up toward team 1
				} else {
					path.TargetY = cy2 // team 1 marches down toward team 2
				}
			})

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

			// Broadcast stronghold state to in-game clients when it changes
		// (positions/ownership/levels) — for client rendering. #54 1B.
		if states, changed := gs.StrongholdStateIfChanged(); changed {
			payload := map[string]interface{}{
				"type":        "stronghold_state",
				"strongholds": states,
			}
			for _, cid := range hub.ClientIDs() {
				if hub.GetClientInGame(cid) {
					hub.SendJSON(cid, payload)
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

			// v1.1: Persist career stats. For each in-game client:
			//   1. Resolve DB playerID via token (cheap lookup, indexed).
			//   2. Map their match-local playerID (1/2) → faction (0/1).
			//   3. Build CareerStats delta from that faction's MatchStats.
			//   4. Atomically accumulate into player_career.
			// Spectators (matchPlayerID=0) and unauthenticated clients are
			// skipped — they have no career to update.
			if gs.Store != nil {
				careerCtx := context.Background()
				for _, cid := range hub.ClientIDs() {
					if !hub.GetClientInGame(cid) {
						continue
					}
					matchPlayerID := hub.GetClientPlayerID(cid)
					if matchPlayerID == 0 {
						continue // spectator (clash/crash test)
					}
					token := hub.GetClientToken(cid)
					if token == "" {
						continue // unauthenticated — no career to update
					}
					faction := gs.FactionOfPlayer(matchPlayerID)
					if faction > 1 {
						continue // unknown faction — shouldn't happen
					}

					// Resolve DB playerID from token.
					player, err := gs.Store.FindOrCreatePlayer(careerCtx, token, hub.GetClientName(cid))
					if err != nil || player == nil {
						log.Printf("career-stats: client %d token lookup failed: %v", cid, err)
						continue
					}

					// Build delta from this faction's slice of MatchStats.
					// MatchStats uses uint16/int32 (per-match magnitudes are
					// small); CareerStats uses uint32 (cumulative across many
					// matches). Cast at the boundary.
					delta := persist.CareerStats{
						PlayerID:        player.ID,
						MatchesPlayed:   1,
						TotalKills:      uint32(ms.Factions[faction].Kills),
						TotalDeaths:     uint32(ms.Factions[faction].Deaths),
						CommanderKills:  uint32(ms.Factions[faction].CommanderKills),
						TotalGoldEarned: uint32(ms.Factions[faction].GoldEarned),
						TotalGoldSpent:  uint32(ms.Factions[faction].GoldSpent),
						TotalRecruits:   uint32(ms.Factions[faction].UnitsRecruited),
					}
					if gs.Lifecycle.WinnerFaction == faction {
						delta.MatchesWon = 1
						// Commander death = loss of that commander in roster.
						// Track as commanders_lost when this player lost their
						// commander (defensive — the death is also handled by
						// FlushRoster's DeleteCommander path).
						delta.CommandersLost = 0
					} else {
						delta.MatchesLost = 1
						// Losing faction's commander is dead (permadeath).
						// This is conservative — assumes the losing player's
						// commander always dies. FlushRoster's existing path
						// will actually delete the commander row.
						delta.CommandersLost = 1
					}

					if err := gs.Store.AddCareerStats(careerCtx, player.ID, delta); err != nil {
						log.Printf("career-stats: AddCareerStats for player %d failed: %v",
							player.ID, err)
					} else {
						// Fetch updated totals and push to client so the UI
						// refreshes immediately after the match.
						if totals, err := gs.Store.GetCareerStats(careerCtx, player.ID); err == nil && totals != nil {
							hub.SendJSON(cid, map[string]interface{}{
								"type":             "career_stats",
								"matches_played":   totals.MatchesPlayed,
								"matches_won":      totals.MatchesWon,
								"matches_lost":     totals.MatchesLost,
								"total_kills":      totals.TotalKills,
								"total_deaths":     totals.TotalDeaths,
								"commander_kills":  totals.CommanderKills,
								"commanders_lost":  totals.CommandersLost,
								"total_gold_earned": totals.TotalGoldEarned,
								"total_gold_spent": totals.TotalGoldSpent,
								"total_recruits":   totals.TotalRecruits,
							})
						}
						log.Printf("career-stats: player %d +%dw/%dl",
							player.ID, delta.MatchesWon, delta.MatchesLost)
					}
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

	// 6b. AI proxy for the combat-unit editor (client/editor/units.html).
	// The editor POSTs a balance instruction + current stats here; we forward
	// to the GLM chat-completions API and return its JSON-delta suggestion.
	// Registered before "/" so it wins the DefaultServeMux precedence.
	// Key resolution: GLM_API_KEY env (preferred, keeps the key off the
	// client) → client-supplied apiKey. Base URL + model are configurable
	// via env (GLM_BASE_URL, GLM_MODEL) and overridable per request.
	http.HandleFunc("/editor/ai", aiProxy)

	// 6c. Snapshot data for the Clash Map editor (client/editor/map.html).
	// Returns the six hand-authored clash maps as JSON so the editor can
	// load them as editable starting points. Serializes straight from
	// clash_maps.go, so it stays in sync with the Go source — no
	// hand-copied snapshot to drift (unlike the units editor's SOURCE_STATS).
	http.HandleFunc("/editor/clash-maps", clashMapsJSON)

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
	// Spawn units at the map generator's spawn point for this player.
	// Issue found in QA: previously hardcoded y=10 (+offset for player
	// index), which disagreed with both the map generator's Spawns
	// array (used by BuildSystem for placement-range checks) and the
	// client's mapData.spawns (used to project flags/minimap markers).
	// Result: building near your own units was rejected as "out of
	// range" because the server thought your spawn was somewhere else.
	mw, _ := gs.MapSize()
	var cx, cy float64
	if gs.Map != nil && playerIndex < len(gs.Map.Spawns) {
		// Use the authoritative spawn position from the map generator.
		cx = float64(gs.Map.Spawns[playerIndex][0])
		cy = float64(gs.Map.Spawns[playerIndex][1])
	} else {
		// Fallback: mirror the previous hardcoded layout.
		cx = float64(mw) * 0.5
		cy = 10.0
		if playerCount > 1 {
			cy = 10.0 + float64(86) * float64(playerIndex) / float64(playerCount-1)
		}
	}
	// Two squads side-by-side, slightly offset from the spawn center so
	// they don't overlap.
	const squadOffsetX = 2.0
	x1 := cx - squadOffsetX
	x2 := cx + squadOffsetX

	baseSquadID := uint32(playerIndex*2 + 1)
	ct := component.CombatUnitType(cmdType)
	gs.SpawnTeamWithType(playerID, baseSquadID, fixed.FromFloat(x1), fixed.FromFloat(cy), 1, ct)
	gs.SpawnTeamWithType(playerID, baseSquadID+1, fixed.FromFloat(x2), fixed.FromFloat(cy), 1, ct)
}

// clashMapSnapshot is the JSON shape the Clash Map editor consumes for one
// hand-authored clash map. Terrain and Elevation are row-major (y*W+x),
// matching GameMap.Tiles layout. []int (not []uint8) so encoding/json emits a
// number array — []byte aliases serialize as base64 strings.
type clashMapSnapshot struct {
	W         int32 `json:"w"`
	H         int32 `json:"h"`
	Terrain   []int `json:"terrain"`
	Elevation []int `json:"elevation"`
}

// clashMapsJSON serializes the six hand-authored clash maps (clash_maps.go)
// as JSON so client/editor/map.html can load them as editable snapshots.
// Reads straight from LoadClashMap, so the editor never holds a stale copy.
func clashMapsJSON(w http.ResponseWriter, r *http.Request) {
	names := []string{"plains", "forest", "road", "river", "stronghold", "hills"}
	out := make(map[string]clashMapSnapshot, len(names))
	for _, name := range names {
		gm := tilemap.LoadClashMap(name)
		if gm == nil {
			continue
		}
		n := int(gm.Width) * int(gm.Height)
		snap := clashMapSnapshot{
			W:         gm.Width,
			H:         gm.Height,
			Terrain:   make([]int, n),
			Elevation: make([]int, n),
		}
		for i, t := range gm.Tiles {
			snap.Terrain[i] = int(t.TerrainType)
			snap.Elevation[i] = int(t.Elevation)
		}
		out[name] = snap
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// combatPrompt / animationPrompt instruct the model to return a strict JSON
// delta the editor can merge. Shared across both backends.
var combatPrompt = `You are a game-balance designer for "Paper War", a real-time strategy game.
You receive the current CombatUnitTypeTable (JSON, keyed by unit) and a 4x3
damage matrix ([weapon][armor] percent, 100 = 1.0x), plus a user instruction.
Respond with ONLY a JSON object describing the changes. Schema:
{
  "stats": { "<UnitKey>": { "field": value }, ... },
  "damageMatrix": [[w0a0,w0a1,w0a2], ...]
}
Valid unit keys: LightInfantry, HeavyInfantry, Sniper, AntiArmorInfantry,
MotorGun, MotorArtillery, MotorMissile.
Valid fields: Weapon(Gun|Cannon|Sniper|Missile), Armor(Light|Heavy|Building),
Cost(int>=1), HP(int>0), Damage(int>0), Range(int 1..32), Cooldown(int>=1),
RecruitCost(int>=0 gold), KillBounty(int ~= 80% of RecruitCost).
Include only the units/fields you change; damageMatrix is optional and must
be a full 4x3 if present. No prose, no markdown fences, no comments.`

var animationPrompt = `You are a pixel-art animation timing designer for "Paper War", a real-time strategy game.
You receive the current per-state animation parameters and a user instruction.
There are 5 states, indexed 0..4: idle, idle2, move, attack, die.
  - framesPerState: array of 5 ints, the frame count per state. Each 1..4
    (MAX_FRAMES_PER_SPRITE — the atlas reserves 4 cells per sprite slot).
  - animFps: array of 5 ints, playback rate in frames-per-second. Each 1..30.
Respond with ONLY a JSON object describing the changes. Schema:
{
  "framesPerState": [n0,n1,n2,n3,n4],
  "animFps":        [n0,n1,n2,n3,n4]
}
Arrays are optional — include only the ones you change, but each must be the
full length-5 array. No prose, no markdown fences, no comments.`

// sysPromptFor returns the system prompt for the given editor kind.
func sysPromptFor(kind string) string {
	if kind == "animation" {
		return animationPrompt
	}
	return combatPrompt
}

// runClaudeCLI drives the locally-installed `claude` CLI in print mode to
// satisfy an editor prompt, then writes an OpenAI-shaped envelope so the
// client's response parsing is identical to the GLM path.
//
// `claude` authenticates itself (no key needed here). We pass
// --dangerously-skip-permissions because the call is a local one-shot text
// generation with no tool use; this keeps the CLI non-interactive. The
// command runs from CLAUDE_CLI_DIR (env, default "/tmp") so it doesn't pull
// in an arbitrary project's context.
func runClaudeCLI(w http.ResponseWriter, instruction, model, sysPrompt string,
	stats, matrix, frames, fps json.RawMessage) {
	// Build the same user-message JSON the GLM path sends.
	userObj := map[string]interface{}{"instruction": instruction}
	if len(stats) > 0 && string(stats) != "null" {
		userObj["stats"] = stats
	}
	if len(matrix) > 0 && string(matrix) != "null" {
		userObj["damageMatrix"] = matrix
	}
	if len(frames) > 0 && string(frames) != "null" {
		userObj["framesPerState"] = frames
	}
	if len(fps) > 0 && string(fps) != "null" {
		userObj["animFps"] = fps
	}
	userMsg, _ := json.Marshal(userObj)

	args := []string{
		"-p", string(userMsg),
		"--append-system-prompt", sysPrompt,
		"--output-format", "json",
		"--dangerously-skip-permissions",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	dir := os.Getenv("CLAUDE_CLI_DIR")
	if dir == "" {
		dir = "/tmp"
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		// `claude` not on PATH is the most likely failure — surface it plainly.
		http.Error(w, "claude CLI failed: "+err.Error()+" "+stderr, http.StatusBadGateway)
		return
	}

	var cr struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &cr); err != nil {
		http.Error(w, "could not parse claude output: "+err.Error(), http.StatusBadGateway)
		return
	}
	if cr.IsError {
		http.Error(w, "claude returned error: "+cr.Result, http.StatusBadGateway)
		return
	}

	// Wrap into the OpenAI chat-completion shape the client expects.
	envelope := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"role": "assistant", "content": cr.Result}},
		},
		"model": "claude-cli",
	}
	body, _ := json.Marshal(envelope)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// aiProxy forwards an editor prompt to an LLM and returns the model's JSON
// delta. Two backends:
//   - "glm" (default): OpenAI-compatible HTTP chat-completions (Zhipu GLM).
//   - "claude": shells out to the locally-installed `claude` CLI in print
//     mode, then wraps its result into the same OpenAI-shaped envelope the
//     client already parses — so the editor code is backend-agnostic.
//
// kind selects the system prompt + delta schema:
//   "combat" (default) — {stats, damageMatrix}
//   "animation"        — {framesPerState, animFps}
//
// GLM auth precedence: GLM_API_KEY env → request apiKey. The claude backend
// needs no key (the CLI authenticates itself) but requires the `claude`
// binary on PATH.
//
// Request body:
//   {kind?, backend?, prompt, ..., model?, apiKey?, baseUrl?}
func aiProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind    string          `json:"kind"`
		Backend string          `json:"backend"`
		Prompt  string          `json:"prompt"`
		Stats   json.RawMessage `json:"stats"`
		Matrix  json.RawMessage `json:"damageMatrix"`
		Frames  json.RawMessage `json:"framesPerState"`
		Fps     json.RawMessage `json:"animFps"`
		Model   string          `json:"model"`
		APIKey  string          `json:"apiKey"`
		BaseURL string          `json:"baseUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Kind == "" {
		req.Kind = "combat"
	}
	if req.Backend == "" {
		req.Backend = "glm"
	}

	// Claude CLI backend doesn't need a GLM key/base; it shells out to the
	// locally-installed `claude` command. Resolve GLM auth only for the HTTP
	// path so the editor works without any GLM config when backend=claude.
	if req.Backend == "claude" {
		runClaudeCLI(w, req.Prompt, req.Model, sysPromptFor(req.Kind), req.Stats, req.Matrix, req.Frames, req.Fps)
		return
	}

	key := os.Getenv("GLM_API_KEY")
	if key == "" {
		key = req.APIKey
	}
	if key == "" {
		http.Error(w, "no GLM API key — set GLM_API_KEY env or pass apiKey", http.StatusUnauthorized)
		return
	}
	base := os.Getenv("GLM_BASE_URL")
	if base == "" {
		base = req.BaseURL
	}
	if base == "" {
		base = "https://open.bigmodel.cn/api/paas/v4"
	}
	model := req.Model
	if model == "" {
		model = os.Getenv("GLM_MODEL")
	}
	if model == "" {
		model = "glm-5.2"
	}

	sysPrompt := sysPromptFor(req.Kind)

	// User message: instruction + whichever context blobs are present for
	// this kind. Empty RawMessages unmarshal to "null"; skip those so the
	// model only sees relevant fields.
	userObj := map[string]interface{}{"instruction": req.Prompt}
	if len(req.Stats) > 0 && string(req.Stats) != "null" {
		userObj["stats"] = req.Stats
	}
	if len(req.Matrix) > 0 && string(req.Matrix) != "null" {
		userObj["damageMatrix"] = req.Matrix
	}
	if len(req.Frames) > 0 && string(req.Frames) != "null" {
		userObj["framesPerState"] = req.Frames
	}
	if len(req.Fps) > 0 && string(req.Fps) != "null" {
		userObj["animFps"] = req.Fps
	}
	userMsg, _ := json.Marshal(userObj)

	body := struct {
		Model          string                    `json:"model"`
		Messages       []map[string]string       `json:"messages"`
		Temperature    float64                   `json:"temperature"`
		ResponseFormat map[string]string         `json:"response_format,omitempty"`
	}{
		Model: model,
		Messages: []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": string(userMsg)},
		},
		Temperature:    0.2,
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq, err := http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "bad upstream url: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		http.Error(w, "GLM request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Stream the upstream body through verbatim. The client parses the
	// OpenAI-shaped {choices:[{message:{content}}]} envelope itself, so the
	// editor keeps working against any OpenAI-compatible endpoint.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
