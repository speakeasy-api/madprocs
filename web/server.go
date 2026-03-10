package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/speakeasy-api/madprocs/process"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the embedded web server
type Server struct {
	manager *process.Manager
	port    int
	server  *http.Server
	hub     *Hub
}

// NewServer creates a new web server
func NewServer(manager *process.Manager, port int) *Server {
	s := &Server{
		manager: manager,
		port:    port,
		hub:     NewHub(),
	}
	return s
}

// Start starts the web server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/processes", s.handleProcesses)
	mux.HandleFunc("/api/logs/", s.handleLogs)
	mux.HandleFunc("/api/process/", s.handleProcessAction)
	mux.HandleFunc("/ws/logs/", s.handleWebSocket)

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// Get a listener on the specified port (0 = random available port)
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return err
	}

	// Update port to the actual port assigned
	s.port = listener.Addr().(*net.TCPAddr).Port

	s.server = &http.Server{
		Handler: mux,
	}

	// Start WebSocket hub
	go s.hub.Run()

	// Start log subscriptions for all processes
	go s.subscribeToAllLogs()

	go func() {
		if err := s.server.Serve(listener); err != http.ErrServerClosed {
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// Port returns the server port
func (s *Server) Port() int {
	return s.port
}

func (s *Server) subscribeToAllLogs() {
	procs := s.manager.List()
	for _, proc := range procs {
		go func(p *process.Process) {
			ch := p.Buffer.Subscribe()
			for line := range ch {
				s.hub.Broadcast(p.Name, LogMessage{
					Timestamp: line.Timestamp.Format("15:04:05"),
					Content:   line.Content,
					Stream:    line.Stream,
					Process:   line.Process,
				})
			}
		}(proc)
	}
}
