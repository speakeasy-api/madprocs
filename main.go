package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
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
		case "status":
			handleStatusCommand()
			return
		case "logs":
			handleLogsCommand(os.Args[2:])
			return
		case "start":
			handleProcessCommand("start", os.Args[2:])
			return
		case "stop":
			handleProcessCommand("stop", os.Args[2:])
			return
		case "restart":
			handleProcessCommand("restart", os.Args[2:])
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

func getBaseURL() (string, error) {
	data, err := os.ReadFile(".madprocs.port")
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("madprocs is not running (no .madprocs.port file found)")
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// httpGetWithRetry performs an HTTP GET with automatic retry on connection failure
func httpGetWithRetry(url string) (*http.Response, error) {
	resp, err := http.Get(url)
	if err != nil {
		// Retry once on connection error (handles sandbox warmup)
		resp, err = http.Get(url)
	}
	return resp, err
}

// httpPostWithRetry performs an HTTP POST with automatic retry on connection failure
func httpPostWithRetry(url string) (*http.Response, error) {
	resp, err := http.Post(url, "", nil)
	if err != nil {
		// Retry once on connection error (handles sandbox warmup)
		resp, err = http.Post(url, "", nil)
	}
	return resp, err
}

func handleStatusCommand() {
	baseURL, err := getBaseURL()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	resp, err := httpGetWithRetry(baseURL + "/api/processes")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to madprocs: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var processes []struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Uptime string `json:"uptime"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&processes); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("madprocs running at %s\n\n", baseURL)
	for _, p := range processes {
		icon := "○"
		if p.State == "running" {
			icon = "●"
		} else if p.State == "exited" {
			icon = "✗"
		}
		uptime := ""
		if p.Uptime != "" {
			uptime = fmt.Sprintf(" (%s)", p.Uptime)
		}
		fmt.Printf("  %s %s: %s%s\n", icon, p.Name, p.State, uptime)
	}
}

func handleLogsCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: madprocs logs <process-name>")
		os.Exit(1)
	}

	baseURL, err := getBaseURL()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	processName := args[0]
	resp, err := httpGetWithRetry(baseURL + "/api/logs/" + processName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to madprocs: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		fmt.Fprintf(os.Stderr, "Process not found: %s\n", processName)
		os.Exit(1)
	}

	var lines []struct {
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&lines); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	for _, line := range lines {
		fmt.Println(line.Content)
	}
}

func handleProcessCommand(action string, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: madprocs %s <process-name>\n", action)
		os.Exit(1)
	}

	baseURL, err := getBaseURL()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	processName := args[0]
	resp, err := httpPostWithRetry(baseURL + "/api/process/" + processName + "/" + action)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to madprocs: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		fmt.Fprintf(os.Stderr, "Process not found: %s\n", processName)
		os.Exit(1)
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("%s: %s\n", processName, action)
}
