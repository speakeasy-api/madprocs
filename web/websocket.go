package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10
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

	// Create upgrader - always allow all origins since we're a local dev tool
	// The allowed hosts middleware already handles security for HTTP requests
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Log upgrade errors for debugging
		fmt.Printf("WebSocket upgrade error: %v\n", err)
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
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// We don't process incoming messages for now
	}
}
