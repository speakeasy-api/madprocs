package main

import (
	"flag"
	"fmt"
	"os"

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
		configPath  string
		webOnly     bool
		webPort     int
		showVersion bool
	)

	flag.StringVar(&configPath, "config", "", "Path to config file (default: mprocs.yaml)")
	flag.StringVar(&configPath, "c", "", "Path to config file (shorthand)")
	flag.BoolVar(&webOnly, "web-only", false, "Run in headless mode with web UI only")
	flag.IntVar(&webPort, "port", 0, "Web server port (default: random available port)")
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

	// Override web port if specified
	if webPort > 0 {
		cfg.WebPort = webPort
	}

	// Create process manager
	manager, err := process.NewManager(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating process manager: %v\n", err)
		os.Exit(1)
	}

	// Start web server
	webServer := web.NewServer(manager, cfg.WebPort)
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
		fmt.Printf("madprocs running in headless mode\n")
		fmt.Printf("Web UI: http://localhost:%d\n", actualPort)
		fmt.Printf("Press Ctrl+C to stop\n")

		// Wait for interrupt
		select {}
	} else {
		// TUI mode
		model := ui.NewModel(manager, actualPort)
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
