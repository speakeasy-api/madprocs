package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a WebSocket client
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	process string // which process this client is subscribed to
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan broadcastMsg
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

type broadcastMsg struct {
	process string
	data    []byte
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan broadcastMsg, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Only send to clients subscribed to this process or "all"
				if client.process == msg.process || client.process == "all" {
					select {
					case client.send <- msg.data:
					default:
						// Client buffer full, skip
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all clients subscribed to a process
func (h *Hub) Broadcast(process string, msg LogMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcast <- broadcastMsg{process: process, data: data}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Get process name from path parameter (Go 1.22+)
	processName := r.PathValue("process")
	if processName == "" {
		processName = "all"
	}

	// Create upgrader with origin check based on allowed hosts
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if len(s.config.AllowedHosts) == 0 {
				return true // Allow all if no hosts configured
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // Allow no-origin requests (same-origin)
			}
			for _, h := range s.config.AllowedHosts {
				if strings.Contains(origin, strings.TrimSpace(h)) {
					return true
				}
			}
			return false
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{
		hub:     s.hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		process: processName,
	}

	s.hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// We don't process incoming messages for now
	}
}
