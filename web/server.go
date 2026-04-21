package web

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/madprocs/process"
)

//go:embed static/*
var staticFiles embed.FS

// Config holds web server configuration
type Config struct {
	Host         string
	Port         int
	TLSCert      string
	TLSKey       string
	AllowedHosts []string
	Version      string
}

// Server is the embedded web server
type Server struct {
	manager *process.Manager
	config  Config
	port    int
	server  *http.Server
	hub     *Hub
}

// NewServer creates a new web server
func NewServer(manager *process.Manager, cfg Config) *Server {
	s := &Server{
		manager: manager,
		config:  cfg,
		port:    cfg.Port,
		hub:     NewHub(),
	}
	return s
}

// Start starts the web server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes with explicit HTTP methods (Go 1.22+)
	mux.HandleFunc("GET /api/processes", s.handleProcesses)
	mux.HandleFunc("GET /api/logs/{name}", s.handleLogs)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("POST /api/process/{name}/{action}", s.handleProcessAction)
	mux.Handle("GET /ws/logs/{process}", http.HandlerFunc(s.handleWebSocket))

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux.Handle("GET /", http.FileServer(http.FS(staticFS)))

	// Wrap with allowed hosts middleware
	var handler http.Handler = mux
	if len(s.config.AllowedHosts) > 0 {
		handler = s.allowedHostsMiddleware(handler)
	}

	// Default to localhost if no host specified
	host := s.config.Host
	if host == "" {
		host = "localhost"
	}

	// Get a listener on the specified host:port (port 0 = random available port)
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, s.config.Port))
	if err != nil && s.config.Port != 0 {
		// Port might be in use (e.g., previous port from .madprocs.port), fall back to random
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:0", host))
	}
	if err != nil {
		return err
	}

	// Update port to the actual port assigned
	s.port = listener.Addr().(*net.TCPAddr).Port

	s.server = &http.Server{
		Handler:  handler,
		ErrorLog: log.New(io.Discard, "", 0), // Suppress TLS handshake errors
	}

	// Start WebSocket hub
	go s.hub.Run()

	// Start log subscriptions for all processes
	go s.subscribeToAllLogs()

	// Start server with or without TLS
	go func() {
		var err error
		if s.config.TLSCert != "" && s.config.TLSKey != "" {
			// TLS enabled
			s.server.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			err = s.server.ServeTLS(listener, s.config.TLSCert, s.config.TLSKey)
		} else {
			err = s.server.Serve(listener)
		}
		if err != http.ErrServerClosed {
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	return nil
}

// allowedHostsMiddleware restricts requests to allowed hosts
func (s *Server) allowedHostsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		// Strip port from host if present
		if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
			host = host[:colonIdx]
		}

		allowed := false
		for _, h := range s.config.AllowedHosts {
			if strings.EqualFold(host, strings.TrimSpace(h)) {
				allowed = true
				break
			}
		}

		if !allowed {
			http.Error(w, "Forbidden: Host not allowed", http.StatusForbidden)
			return
		}

		// Set CORS headers for allowed hosts
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, h := range s.config.AllowedHosts {
				// Check if origin matches any allowed host
				if strings.Contains(origin, strings.TrimSpace(h)) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
					break
				}
			}
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
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

// Host returns the server host
func (s *Server) Host() string {
	if s.config.Host == "" {
		return "localhost"
	}
	return s.config.Host
}

// IsTLS returns whether TLS is enabled
func (s *Server) IsTLS() bool {
	return s.config.TLSCert != "" && s.config.TLSKey != ""
}

func (s *Server) subscribeToAllLogs() {
	procs := s.manager.List()
	for _, proc := range procs {
		go func(p *process.Process) {
			ch := p.Buffer.Subscribe()
			for line := range ch {
				if line.Stream == "clear" {
					s.hub.BroadcastEvent(p.Name, "clear")
					continue
				}
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
