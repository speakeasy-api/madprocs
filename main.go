package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/process"
	"github.com/speakeasy-api/madprocs/ui"
	"github.com/speakeasy-api/madprocs/web"
)

var (
	version = "dev"
)

func main() {
	var (
		configPath   string
		webOnly      bool
		webPort      int
		webHost      string
		tlsCert      string
		tlsKey       string
		allowedHosts string
		showVersion  bool
	)

	flag.StringVar(&configPath, "config", "", "Path to config file (default: mprocs.yaml)")
	flag.StringVar(&configPath, "c", "", "Path to config file (shorthand)")
	flag.BoolVar(&webOnly, "web-only", false, "Run in headless mode with web UI only")
	flag.IntVar(&webPort, "port", 0, "Web server port (default: random available port)")
	flag.StringVar(&webHost, "host", "", "Web server host (default: localhost)")
	flag.StringVar(&tlsCert, "tls-cert", "", "Path to TLS certificate file")
	flag.StringVar(&tlsKey, "tls-key", "", "Path to TLS key file")
	flag.StringVar(&allowedHosts, "allowed-hosts", "", "Comma-separated list of allowed hosts for CORS")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.BoolVar(&showVersion, "v", false, "Show version (shorthand)")
	flag.Parse()

	if showVersion {
		fmt.Printf("madprocs %s\n", version)
		os.Exit(0)
	}

	// Load config
	cfg, err := config.LoadOrDefault(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Procs) == 0 {
		fmt.Fprintln(os.Stderr, "No processes configured. Create an mprocs.yaml file or specify --config")
		os.Exit(1)
	}

	// Override web settings if specified via CLI
	if webPort > 0 {
		cfg.WebPort = webPort
	}
	if webHost != "" {
		cfg.WebHost = webHost
	}
	if tlsCert != "" {
		cfg.TLSCert = tlsCert
	}
	if tlsKey != "" {
		cfg.TLSKey = tlsKey
	}
	if allowedHosts != "" {
		cfg.AllowedHosts = strings.Split(allowedHosts, ",")
	}

	// Create process manager
	manager, err := process.NewManager(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating process manager: %v\n", err)
		os.Exit(1)
	}

	// Start web server
	webCfg := web.Config{
		Host:         cfg.WebHost,
		Port:         cfg.WebPort,
		TLSCert:      cfg.TLSCert,
		TLSKey:       cfg.TLSKey,
		AllowedHosts: cfg.AllowedHosts,
	}
	webServer := web.NewServer(manager, webCfg)
	if err := webServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting web server: %v\n", err)
		os.Exit(1)
	}
	defer webServer.Stop()

	// Get the actual port (may be different if random port was used)
	actualPort := webServer.Port()

	// Start all autostart processes
	manager.StartAll()

	if webOnly {
		// Headless mode - just run web server
		protocol := "http"
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			protocol = "https"
		}
		fmt.Printf("madprocs running in headless mode\n")
		fmt.Printf("Web UI: %s://%s:%d\n", protocol, cfg.WebHost, actualPort)
		fmt.Printf("Press Ctrl+C to stop\n")

		// Wait for interrupt
		select {}
	} else {
		// TUI mode
		model := ui.NewModel(manager, actualPort, webServer.Host(), webServer.IsTLS())
		p := tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
			tea.WithInputTTY(),
		)

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			manager.Close()
			os.Exit(1)
		}
	}

	manager.Close()
}
