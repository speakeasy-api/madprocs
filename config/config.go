package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level mprocs-compatible configuration
type Config struct {
	Procs            map[string]ProcConfig `yaml:"procs"`
	HideKeymapWindow bool                  `yaml:"hide_keymap_window"`
	MouseScrollSpeed int                   `yaml:"mouse_scroll_speed"`
	Scrollback       int                   `yaml:"scrollback"`
	ProcListWidth    int                   `yaml:"proc_list_width"`
	LogDir           string                `yaml:"log_dir"`
	WebPort          int                   `yaml:"web_port"`
}

// StopConfig represents how to stop a process
type StopConfig struct {
	Signal   string   // SIGINT, SIGTERM, SIGKILL, hard-kill
	SendKeys []string // key sequences to send
}

// ProcConfig represents a single process configuration
type ProcConfig struct {
	Shell       string             `yaml:"shell"`
	Cmd         StringOrSlice      `yaml:"cmd"`
	Cwd         string             `yaml:"cwd"`
	Env         map[string]*string `yaml:"env"`
	AddPath     StringOrSlice      `yaml:"add_path"`
	Autostart   *bool              `yaml:"autostart"`
	Autorestart bool               `yaml:"autorestart"`
	Stop        StopConfig         `yaml:"-"` // custom unmarshaling
	StopRaw     interface{}        `yaml:"stop"`
	LogDir      string             `yaml:"log_dir"`
}

// StringOrSlice handles YAML fields that can be either a string or []string
type StringOrSlice []string

func (s *StringOrSlice) UnmarshalYAML(node *yaml.Node) error {
	var single string
	if err := node.Decode(&single); err == nil {
		*s = []string{single}
		return nil
	}

	var slice []string
	if err := node.Decode(&slice); err == nil {
		*s = slice
		return nil
	}

	return nil
}

// GetAutostart returns true if the process should autostart (default: true)
func (p *ProcConfig) GetAutostart() bool {
	if p.Autostart == nil {
		return true
	}
	return *p.Autostart
}

// GetCommand returns the command to execute
func (p *ProcConfig) GetCommand() (string, []string, bool) {
	if p.Shell != "" {
		return p.Shell, nil, true // shell mode
	}
	if len(p.Cmd) > 0 {
		return p.Cmd[0], p.Cmd[1:], false // direct mode
	}
	return "", nil, false
}

// Load reads and parses the config file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Scrollback:    1000,
		ProcListWidth: 20,
		WebPort:       0,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Resolve <CONFIG_DIR> in paths and parse stop config
	configDir := filepath.Dir(path)
	for name, proc := range cfg.Procs {
		if strings.HasPrefix(proc.Cwd, "<CONFIG_DIR>") {
			proc.Cwd = strings.Replace(proc.Cwd, "<CONFIG_DIR>", configDir, 1)
		}
		if strings.HasPrefix(proc.LogDir, "<CONFIG_DIR>") {
			proc.LogDir = strings.Replace(proc.LogDir, "<CONFIG_DIR>", configDir, 1)
		}

		// Parse stop config
		proc.Stop = parseStopConfig(proc.StopRaw)
		cfg.Procs[name] = proc
	}

	if strings.HasPrefix(cfg.LogDir, "<CONFIG_DIR>") {
		cfg.LogDir = strings.Replace(cfg.LogDir, "<CONFIG_DIR>", configDir, 1)
	}

	return cfg, nil
}

// parseStopConfig parses the stop configuration which can be:
// - a string like "SIGTERM"
// - a map with send-keys
func parseStopConfig(raw interface{}) StopConfig {
	if raw == nil {
		return StopConfig{Signal: "SIGINT"}
	}

	// String signal
	if s, ok := raw.(string); ok {
		return StopConfig{Signal: s}
	}

	// Map with send-keys
	if m, ok := raw.(map[string]interface{}); ok {
		if keys, ok := m["send-keys"]; ok {
			if keySlice, ok := keys.([]interface{}); ok {
				sendKeys := make([]string, len(keySlice))
				for i, k := range keySlice {
					if s, ok := k.(string); ok {
						sendKeys[i] = s
					}
				}
				return StopConfig{SendKeys: sendKeys}
			}
		}
	}

	return StopConfig{Signal: "SIGINT"}
}

// LoadOrDefault tries to load config from path, or looks for mprocs.yaml in cwd
func LoadOrDefault(path string) (*Config, error) {
	if path != "" {
		return Load(path)
	}

	// Try mprocs.yaml in current directory
	if _, err := os.Stat("mprocs.yaml"); err == nil {
		return Load("mprocs.yaml")
	}

	// Try mprocs.yml
	if _, err := os.Stat("mprocs.yml"); err == nil {
		return Load("mprocs.yml")
	}

	// Return empty config
	return &Config{
		Procs:         make(map[string]ProcConfig),
		Scrollback:    1000,
		ProcListWidth: 20,
		WebPort:       0,
	}, nil
}
