package network

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// connectHub spins up a Hub behind an httptest server and dials a client.
func connectHub(t *testing.T, onCmd func(uint32, *Command), onText func(uint32, map[string]interface{})) (*Hub, *websocket.Conn, func()) {
	t.Helper()
	hub := NewHub(onCmd, onText)
	server := httptest.NewServer(hub)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial failed: %v", err)
	}
	cleanup := func() {
		conn.Close()
		server.Close()
		time.Sleep(50 * time.Millisecond) // let goroutines finish
	}
	return hub, conn, cleanup
}

func TestNewHubHasNoClients(t *testing.T) {
	hub := NewHub(nil, nil)
	if hub.ClientCount() != 0 {
		t.Errorf("new hub has %d clients, want 0", hub.ClientCount())
	}
}

func TestHubClientConnect(t *testing.T) {
	hub, conn, cleanup := connectHub(t, nil, nil)
	defer cleanup()

	// Give the server goroutine time to register
	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Errorf("after connect: %d clients, want 1", hub.ClientCount())
	}

	ids := hub.ClientIDs()
	if len(ids) != 1 || ids[0] != 1 {
		t.Errorf("client IDs = %v, want [1]", ids)
	}
	conn.Close()
}

func TestHubSetGetClientName(t *testing.T) {
	hub, _, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	hub.SetClientName(1, "Player1")
	got := hub.GetClientName(1)
	if got != "Player1" {
		t.Errorf("GetClientName(1) = %q, want %q", got, "Player1")
	}

	// Nonexistent client
	got = hub.GetClientName(999)
	if got != "" {
		t.Errorf("GetClientName(999) = %q, want empty", got)
	}

	// Set name on nonexistent client — should be a no-op
	hub.SetClientName(999, "Ghost")
}

func TestHubSetGetClientPlayerID(t *testing.T) {
	hub, _, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	hub.SetClientPlayerID(1, 2)
	got := hub.GetClientPlayerID(1)
	if got != 2 {
		t.Errorf("GetClientPlayerID(1) = %d, want 2", got)
	}

	// Nonexistent client
	got = hub.GetClientPlayerID(999)
	if got != 0 {
		t.Errorf("GetClientPlayerID(999) = %d, want 0", got)
	}

	hub.SetClientPlayerID(999, 5) // no-op
}

func TestHubSetGetClientInGame(t *testing.T) {
	hub, _, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	if hub.GetClientInGame(1) {
		t.Error("new client should not be in game")
	}

	hub.SetClientInGame(1, true)
	if !hub.GetClientInGame(1) {
		t.Error("client should be in game after Set")
	}

	// Nonexistent client
	if hub.GetClientInGame(999) {
		t.Error("nonexistent client should report not in game")
	}

	hub.SetClientInGame(999, true) // no-op
}

func TestHubSendToClient(t *testing.T) {
	hub, conn, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	data := []byte{0xFF, 0xFE, 0x01, 0x02}
	hub.SendToClient(1, data)

	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if len(msg) != len(data) {
		t.Errorf("received %d bytes, want %d", len(msg), len(data))
	}

	// Send to nonexistent client — should not crash
	hub.SendToClient(999, data)
}

func TestHubSendJSON(t *testing.T) {
	hub, conn, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	payload := map[string]interface{}{"type": "gold", "amount": 500}
	hub.SendJSON(1, payload)

	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "gold" {
		t.Errorf("type = %v, want gold", got["type"])
	}

	// Send JSON to nonexistent client — should not crash
	hub.SendJSON(999, payload)
}

func TestHubBroadcast(t *testing.T) {
	hub, conn, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	data := []byte{0xAA, 0xBB}
	hub.Broadcast(data)

	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if len(msg) != 2 || msg[0] != 0xAA || msg[1] != 0xBB {
		t.Errorf("received %v, want [0xAA 0xBB]", msg)
	}
}

func TestHubCommandDispatch(t *testing.T) {
	var receivedCmd *Command
	var cmdClientID uint32
	hub, conn, cleanup := connectHub(t, func(cid uint32, cmd *Command) {
		receivedCmd = cmd
		cmdClientID = cid
	}, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	// Send a move command: type(1) + seq(4) + tick(4) + squad(4) + x(8) + y(8) = 29 bytes
	cmd := &Command{
		Type:          CmdMoveSquad,
		ClientSeq:     42,
		PredictedTick: 100,
		SquadID:       7,
		TargetX:       32768, // 0.5 in fixed-point
		TargetY:       65536, // 1.0
	}
	data := EncodeCommand(cmd)
	_ = hub // hub is used in connectHub setup
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Wait for callback
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && receivedCmd == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if receivedCmd == nil {
		t.Fatal("command not received within timeout")
	}
	if receivedCmd.Type != CmdMoveSquad {
		t.Errorf("cmd type = %d, want %d", receivedCmd.Type, CmdMoveSquad)
	}
	if receivedCmd.ClientSeq != 42 {
		t.Errorf("cmd seq = %d, want 42", receivedCmd.ClientSeq)
	}
	if cmdClientID != 1 {
		t.Errorf("client ID = %d, want 1", cmdClientID)
	}
}

func TestHubTextMessageDispatch(t *testing.T) {
	var receivedText map[string]interface{}
	var textClientID uint32
	hub, conn, cleanup := connectHub(t, nil, func(cid uint32, msg map[string]interface{}) {
		receivedText = msg
		textClientID = cid
	})
	defer cleanup()
	_ = hub
	time.Sleep(50 * time.Millisecond)

	payload, _ := json.Marshal(map[string]string{"hello": "world"})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && receivedText == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if receivedText == nil {
		t.Fatal("text message not received within timeout")
	}
	if receivedText["hello"] != "world" {
		t.Errorf("text hello = %v, want world", receivedText["hello"])
	}
	if textClientID != 1 {
		t.Errorf("client ID = %d, want 1", textClientID)
	}
}

func TestHubClientDisconnect(t *testing.T) {
	hub, conn, cleanup := connectHub(t, nil, nil)
	defer cleanup()
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("before disconnect: %d clients, want 1", hub.ClientCount())
	}

	// Close client connection
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("after disconnect: %d clients, want 0", hub.ClientCount())
	}
}
