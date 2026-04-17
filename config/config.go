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
	WebHost          string                `yaml:"web_host"`
	TLSCert          string                `yaml:"tls_cert"`
	TLSKey           string                `yaml:"tls_key"`
	AllowedHosts     []string              `yaml:"allowed_hosts"`
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
	Tui         bool               `yaml:"tui"`
	TuiCols     int                `yaml:"tui_cols"`
	TuiRows     int                `yaml:"tui_rows"`
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
		WebHost:       "localhost",
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Resolve <CONFIG_DIR> and environment variables in paths
	configDir := filepath.Dir(path)
	for name, proc := range cfg.Procs {
		// Expand <CONFIG_DIR> first, then env vars
		proc.Cwd = expandConfigVars(proc.Cwd, configDir)
		proc.LogDir = expandConfigVars(proc.LogDir, configDir)
		proc.Shell = os.ExpandEnv(proc.Shell)

		// Expand env vars in cmd args
		for i, arg := range proc.Cmd {
			proc.Cmd[i] = os.ExpandEnv(arg)
		}

		// Expand env vars in add_path
		for i, p := range proc.AddPath {
			proc.AddPath[i] = expandConfigVars(p, configDir)
		}

		// Parse stop config
		proc.Stop = parseStopConfig(proc.StopRaw)
		cfg.Procs[name] = proc
	}

	cfg.LogDir = expandConfigVars(cfg.LogDir, configDir)
	cfg.TLSCert = expandConfigVars(cfg.TLSCert, configDir)
	cfg.TLSKey = expandConfigVars(cfg.TLSKey, configDir)

	return cfg, nil
}

// expandConfigVars expands <CONFIG_DIR> and environment variables in a string
func expandConfigVars(s string, configDir string) string {
	if s == "" {
		return s
	}
	// Expand <CONFIG_DIR> first
	s = strings.Replace(s, "<CONFIG_DIR>", configDir, -1)
	// Then expand environment variables ($VAR or ${VAR})
	return os.ExpandEnv(s)
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
		WebHost:       "localhost",
	}, nil
}
