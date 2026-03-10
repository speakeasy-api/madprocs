package process

import (
	"sort"
	"sync"

	"github.com/speakeasy-api/madprocs/config"
)

// Manager manages multiple processes
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process
	order     []string // maintains insertion order for display
	cfg       *config.Config
}

// NewManager creates a new process manager from config
func NewManager(cfg *config.Config) (*Manager, error) {
	m := &Manager{
		processes: make(map[string]*Process),
		order:     make([]string, 0, len(cfg.Procs)),
		cfg:       cfg,
	}

	// Get sorted keys for consistent ordering
	names := make([]string, 0, len(cfg.Procs))
	for name := range cfg.Procs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		procCfg := cfg.Procs[name]
		proc, err := NewProcess(name, procCfg, cfg.Scrollback, cfg.LogDir)
		if err != nil {
			return nil, err
		}
		m.processes[name] = proc
		m.order = append(m.order, name)
	}

	return m, nil
}

// StartAll starts all processes with autostart enabled
func (m *Manager) StartAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, name := range m.order {
		proc := m.processes[name]
		if proc.Config.GetAutostart() {
			proc.Start()
		}
	}
}

// StopAll stops all running processes
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, proc := range m.processes {
		wg.Add(1)
		go func(p *Process) {
			defer wg.Done()
			p.Stop()
		}(proc)
	}
	wg.Wait()
}

// Get returns a process by name
func (m *Manager) Get(name string) *Process {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processes[name]
}

// List returns all processes in order
func (m *Manager) List() []*Process {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Process, len(m.order))
	for i, name := range m.order {
		result[i] = m.processes[name]
	}
	return result
}

// Names returns process names in order
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.order))
	copy(result, m.order)
	return result
}

// Count returns the number of processes
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.processes)
}

// Start starts a specific process
func (m *Manager) Start(name string) error {
	proc := m.Get(name)
	if proc == nil {
		return nil
	}
	return proc.Start()
}

// Stop stops a specific process
func (m *Manager) Stop(name string) error {
	proc := m.Get(name)
	if proc == nil {
		return nil
	}
	return proc.Stop()
}

// Restart restarts a specific process
func (m *Manager) Restart(name string) error {
	proc := m.Get(name)
	if proc == nil {
		return nil
	}
	return proc.Restart()
}

// Close closes all processes and cleans up resources
func (m *Manager) Close() {
	m.StopAll()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, proc := range m.processes {
		proc.Close()
	}
}

// RunningCount returns the number of running processes
func (m *Manager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, proc := range m.processes {
		if proc.State() == StateRunning {
			count++
		}
	}
	return count
}
