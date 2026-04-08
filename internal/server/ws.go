package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local tool
	},
}

// Hub maintains the set of active WebSocket clients and broadcasts messages.
type Hub struct {
	clients    map[*wsClient]bool
	mu         sync.RWMutex
	broadcast  chan []byte
	done       chan struct{}
	onMessage  func(msg map[string]interface{}) // optional handler for client messages
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*wsClient]bool),
		broadcast: make(chan []byte, 64),
		done:      make(chan struct{}),
	}
}

// OnMessage sets a handler for client→server messages.
func (h *Hub) OnMessage(fn func(msg map[string]interface{})) {
	h.onMessage = fn
}

// Run starts the hub's broadcast loop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return
		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					// Client too slow — close it
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Stop shuts down the hub.
func (h *Hub) Stop() {
	close(h.done)
	h.mu.Lock()
	for client := range h.clients {
		close(client.send)
		client.conn.Close()
	}
	h.mu.Unlock()
}

// Broadcast sends a JSON message to all connected clients.
func (h *Hub) Broadcast(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("ws broadcast marshal error: %v", err)
		return
	}
	select {
	case h.broadcast <- data:
	default:
		// Broadcast channel full
	}
}

// HandleWebSocket handles WebSocket upgrade requests.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 32),
	}

	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()

	go client.writePump()
	go client.readPump(h)
}

func (c *wsClient) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *wsClient) readPump(h *Hub) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if h.onMessage != nil {
			var msg map[string]interface{}
			if json.Unmarshal(data, &msg) == nil {
				h.onMessage(msg)
			}
		}
	}
}
