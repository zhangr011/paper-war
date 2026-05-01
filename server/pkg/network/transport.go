package network

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	mu      sync.RWMutex
	clients map[uint32]*ClientSession
	nextID  uint32
	onCmd   func(clientID uint32, cmd *Command)
}

func NewHub(onCmd func(clientID uint32, cmd *Command)) *Hub {
	return &Hub{
		clients: make(map[uint32]*ClientSession),
		onCmd:   onCmd,
	}
}

type ClientSession struct {
	ID         uint32
	PlayerID   uint32
	conn       *websocket.Conn
	mu         sync.Mutex
	sendCh     chan []byte
	closeCh    chan struct{}
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
		ID:      clientID,
		conn:    conn,
		sendCh:  make(chan []byte, 256),
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
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
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
		case data, ok := <-s.sendCh:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
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
	case session.sendCh <- data:
	default:
		log.Printf("client %d: send buffer full, dropping message", clientID)
	}
}

func (h *Hub) Broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, session := range h.clients {
		select {
		case session.sendCh <- data:
		default:
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Serve starts the WebSocket server on the given address.
func Serve(addr string, hub *Hub) error {
	http.Handle("/ws", hub)
	log.Printf("WebSocket server listening on %s", addr)
	return http.ListenAndServe(addr, nil)
}
