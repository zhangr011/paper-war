package network

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// outgoing wraps data with its WebSocket message type for the send channel.
type outgoing struct {
	data   []byte
	isText bool
}

type Hub struct {
	mu      sync.RWMutex
	clients map[uint32]*ClientSession
	nextID  uint32
	onCmd   func(clientID uint32, cmd *Command)
	onText  func(clientID uint32, msg map[string]interface{})
}

func NewHub(onCmd func(clientID uint32, cmd *Command), onText func(clientID uint32, msg map[string]interface{})) *Hub {
	return &Hub{
		clients: make(map[uint32]*ClientSession),
		onCmd:   onCmd,
		onText:  onText,
	}
}

type ClientSession struct {
	ID       uint32
	PlayerID uint32
	Name     string
	conn     *websocket.Conn
	mu       sync.Mutex
	sendCh   chan outgoing
	closeCh  chan struct{}
}

func (s *ClientSession) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Name = name
}

func (s *ClientSession) GetName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Name
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	h.mu.Lock()
	h.nextID++
	clientID := h.nextID
	session := &ClientSession{
		ID:     clientID,
		conn:   conn,
		sendCh: make(chan outgoing, 256),
		closeCh: make(chan struct{}),
	}
	h.clients[clientID] = session
	h.mu.Unlock()

	log.Printf("client %d connected", clientID)

	go session.writePump()
	go session.readPump(h)
}

func (s *ClientSession) readPump(h *Hub) {
	defer func() {
		close(s.closeCh)
		s.conn.Close()
		h.mu.Lock()
		delete(h.clients, s.ID)
		h.mu.Unlock()
		log.Printf("client %d disconnected", s.ID)
	}()

	s.conn.SetReadLimit(4096)
	s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		msgType, msg, err := s.conn.ReadMessage()
		if err != nil {
			return
		}

		if msgType == websocket.TextMessage {
			// Parse as JSON
			var data map[string]interface{}
			if err := json.Unmarshal(msg, &data); err == nil {
				if h.onText != nil {
					h.onText(s.ID, data)
				}
			}
			continue
		}

		// Binary: existing decode path
		cmd, err := DecodeCommand(msg)
		if err != nil {
			log.Printf("client %d: decode error: %v", s.ID, err)
			continue
		}
		if h.onCmd != nil {
			h.onCmd(s.ID, cmd)
		}
	}
}

func (s *ClientSession) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		s.conn.Close()
	}()

	for {
		select {
		case out, ok := <-s.sendCh:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			msgType := websocket.BinaryMessage
			if out.isText {
				msgType = websocket.TextMessage
			}
			if err := s.conn.WriteMessage(msgType, out.data); err != nil {
				return
			}
		case <-ticker.C:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-s.closeCh:
			return
		}
	}
}

func (h *Hub) SendToClient(clientID uint32, data []byte) {
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case session.sendCh <- outgoing{data: data, isText: false}:
	default:
		log.Printf("client %d: send buffer full, dropping message", clientID)
	}
}

// SendJSON marshals v as JSON and sends it as a text message to the client.
func (h *Hub) SendJSON(clientID uint32, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case session.sendCh <- outgoing{data: data, isText: true}:
	default:
		log.Printf("client %d: send buffer full, dropping JSON message", clientID)
	}
}

// SetClientName sets the display name for a connected client.
func (h *Hub) SetClientName(clientID uint32, name string) {
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if ok {
		session.SetName(name)
	}
}

// GetClientName returns the display name for a connected client.
func (h *Hub) GetClientName(clientID uint32) string {
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if ok {
		return session.GetName()
	}
	return ""
}

func (h *Hub) SetClientPlayerID(clientID uint32, playerID uint32) {
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if ok {
		session.PlayerID = playerID
	}
}

func (h *Hub) GetClientPlayerID(clientID uint32) uint32 {
	h.mu.RLock()
	session, ok := h.clients[clientID]
	h.mu.RUnlock()
	if ok {
		return session.PlayerID
	}
	return 0
}

func (h *Hub) Broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, session := range h.clients {
		select {
		case session.sendCh <- outgoing{data: data, isText: false}:
		default:
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ClientIDs returns a snapshot of all connected client IDs.
func (h *Hub) ClientIDs() []uint32 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uint32, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// Serve starts the WebSocket server on the given address.
func Serve(addr string, hub *Hub) error {
	http.Handle("/ws", hub)
	log.Printf("WebSocket server listening on %s", addr)
	return http.ListenAndServe(addr, nil)
}
