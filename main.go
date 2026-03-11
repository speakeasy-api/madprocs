package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/process"
	"github.com/speakeasy-api/madprocs/ui"
	"github.com/speakeasy-api/madprocs/web"
)

//go:embed .claude/commands/madprocs.md
var skillContent embed.FS

var (
	version = "dev"
)

func main() {
	// Handle subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "skill":
			handleSkillCommand(os.Args[2:])
			return
		}
	}
	var (
		configPath   string
		webOnly      bool
		webPort      int
		webHost      string
		tlsCert      string
		tlsKey       string
		allowedHosts string
		logDir       string
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
	flag.StringVar(&logDir, "log-dir", "", "Directory to write process log files")
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
	if logDir != "" {
		cfg.LogDir = logDir
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

	// Write port file for CLI tools and Claude Code skill to discover
	portFile := ".madprocs.port"
	protocol := "http"
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		protocol = "https"
	}
	portInfo := fmt.Sprintf("%s://%s:%d", protocol, webServer.Host(), actualPort)
	if err := os.WriteFile(portFile, []byte(portInfo), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write port file: %v\n", err)
	} else {
		defer os.Remove(portFile)
	}

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

func handleSkillCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: madprocs skill <command>")
		fmt.Println("")
		fmt.Println("Commands:")
		fmt.Println("  install    Install Claude Code skill to current project")
		fmt.Println("  uninstall  Remove Claude Code skill from current project")
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		installSkill()
	case "uninstall":
		uninstallSkill()
	default:
		fmt.Fprintf(os.Stderr, "Unknown skill command: %s\n", args[0])
		os.Exit(1)
	}
}

func installSkill() {
	// Read the embedded skill content
	content, err := skillContent.ReadFile(".claude/commands/madprocs.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading skill content: %v\n", err)
		os.Exit(1)
	}

	// Create .claude/commands directory
	skillDir := filepath.Join(".claude", "commands")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	// Write the skill file
	skillPath := filepath.Join(skillDir, "madprocs.md")
	if err := os.WriteFile(skillPath, content, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing skill file: %v\n", err)
		os.Exit(1)
	}

	// Add to .git/info/exclude if git repo exists
	gitExclude := filepath.Join(".git", "info", "exclude")
	if _, err := os.Stat(".git"); err == nil {
		// Ensure .git/info exists
		os.MkdirAll(filepath.Join(".git", "info"), 0755)

		// Read existing excludes
		excludeContent, _ := os.ReadFile(gitExclude)
		excludeLine := ".claude/commands/madprocs.md"

		// Add if not already present
		if !strings.Contains(string(excludeContent), excludeLine) {
			f, err := os.OpenFile(gitExclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				if len(excludeContent) > 0 && !strings.HasSuffix(string(excludeContent), "\n") {
					f.WriteString("\n")
				}
				f.WriteString(excludeLine + "\n")
				f.Close()
			}
		}
	}

	fmt.Printf("Installed madprocs skill to %s\n", skillPath)
	fmt.Println("")
	fmt.Println("Use /madprocs in Claude Code to get help controlling madprocs.")
}

func uninstallSkill() {
	skillPath := filepath.Join(".claude", "commands", "madprocs.md")

	if err := os.Remove(skillPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Skill not installed.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error removing skill file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Uninstalled madprocs skill.")
}
