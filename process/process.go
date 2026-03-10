package process

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/log"
	"github.com/creack/pty"
)

// Regex to match cursor movement and screen control ANSI sequences
// This keeps color codes but removes cursor positioning, clearing, etc.
var ansiCursorControlRegex = regexp.MustCompile(`\x1b\[[0-9;]*[HJKfABCDEFGsuhl]|\x1b\[\?[0-9;]*[hl]|\x1b7|\x1b8|\x1b\[2J|\x1b\[K`)

// State represents the current state of a process
type State int

const (
	StateStopped State = iota
	StateRunning
	StateExited
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	case StateExited:
		return "exited"
	default:
		return "unknown"
	}
}

// Process wraps an OS process with lifecycle management
type Process struct {
	Name   string
	Config config.ProcConfig
	Buffer *log.Buffer

	mu         sync.RWMutex
	cmd        *exec.Cmd
	ptyFile    *os.File
	state      State
	exitCode   int
	startTime  time.Time
	cancelFunc context.CancelFunc
}

// NewProcess creates a new Process instance
func NewProcess(name string, cfg config.ProcConfig, scrollback int, globalLogDir string) (*Process, error) {
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = globalLogDir
	}

	buf, err := log.NewBuffer(scrollback, logDir, name)
	if err != nil {
		return nil, err
	}

	return &Process{
		Name:   name,
		Config: cfg,
		Buffer: buf,
		state:  StateStopped,
	}, nil
}

// State returns the current process state
func (p *Process) State() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// ExitCode returns the exit code (only valid after exit)
func (p *Process) ExitCode() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitCode
}

// Uptime returns how long the process has been running
func (p *Process) Uptime() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state != StateRunning {
		return 0
	}
	return time.Since(p.startTime)
}

// Start launches the process with PTY support
func (p *Process) Start() error {
	p.mu.Lock()
	if p.state == StateRunning {
		p.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancelFunc = cancel

	cmdStr, args, isShell := p.Config.GetCommand()
	if cmdStr == "" {
		p.mu.Unlock()
		return nil
	}

	var cmd *exec.Cmd
	if isShell {
		shell := os.Getenv("SHELL")
		if shell == "" {
			if runtime.GOOS == "windows" {
				shell = "cmd"
				cmd = exec.CommandContext(ctx, shell, "/C", cmdStr)
			} else {
				shell = "/bin/sh"
				cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
			}
		} else {
			cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
		}
	} else {
		cmd = exec.CommandContext(ctx, cmdStr, args...)
	}

	// Set working directory
	if p.Config.Cwd != "" {
		cmd.Dir = p.Config.Cwd
	}

	// Set environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	for k, v := range p.Config.Env {
		if v == nil {
			// Remove variable
			for i, e := range cmd.Env {
				if strings.HasPrefix(e, k+"=") {
					cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
					break
				}
			}
		} else {
			cmd.Env = append(cmd.Env, k+"="+*v)
		}
	}

	// Add to PATH
	if len(p.Config.AddPath) > 0 {
		currentPath := os.Getenv("PATH")
		newPath := strings.Join(p.Config.AddPath, string(os.PathListSeparator))
		cmd.Env = append(cmd.Env, "PATH="+newPath+string(os.PathListSeparator)+currentPath)
	}

	// Note: Don't set SysProcAttr with Setpgid when using PTY - they conflict
	// The PTY library handles process setup itself

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		p.mu.Unlock()
		return err
	}

	// Set PTY size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 120})

	p.cmd = cmd
	p.ptyFile = ptmx
	p.state = StateRunning
	p.startTime = time.Now()
	p.mu.Unlock()

	// Stream PTY output (combined stdout/stderr)
	go p.streamPtyOutput(ptmx)

	// Wait for process to exit
	go p.wait()

	return nil
}

func (p *Process) streamOutput(r io.ReadCloser, stream string) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		p.Buffer.Write(p.Name, stream, scanner.Text())
	}
}

func (p *Process) streamPtyOutput(ptmx *os.File) {
	reader := bufio.NewReader(ptmx)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				// PTY closed or error
			}
			return
		}
		// Strip trailing newline and carriage return
		line = strings.TrimRight(line, "\r\n")
		// Strip cursor movement and screen control sequences (keep colors)
		line = ansiCursorControlRegex.ReplaceAllString(line, "")
		// Skip empty lines that were only control sequences
		if strings.TrimSpace(line) == "" && line != "" {
			continue
		}
		p.Buffer.Write(p.Name, "stdout", line)
	}
}

func (p *Process) wait() {
	if p.cmd == nil {
		return
	}

	err := p.cmd.Wait()

	p.mu.Lock()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
		} else {
			p.exitCode = -1
		}
	} else {
		p.exitCode = 0
	}
	p.state = StateExited
	p.mu.Unlock()

	// Handle autorestart
	if p.Config.Autorestart {
		// Don't restart if exited too quickly (within 1 second)
		if time.Since(p.startTime) > time.Second {
			time.Sleep(100 * time.Millisecond)
			p.Start()
		}
	}
}

// Stop terminates the process
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != StateRunning || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	// Cancel context
	if p.cancelFunc != nil {
		p.cancelFunc()
	}

	// Handle send-keys mode (like Ctrl+C) via PTY
	if len(p.Config.Stop.SendKeys) > 0 && p.ptyFile != nil {
		// Send Ctrl+C (0x03) via PTY
		for _, key := range p.Config.Stop.SendKeys {
			if key == "<C-c>" || key == "C-c" {
				p.ptyFile.Write([]byte{0x03}) // Ctrl+C
			} else if key == "<C-d>" || key == "C-d" {
				p.ptyFile.Write([]byte{0x04}) // Ctrl+D
			}
		}
	} else {
		// Determine signal based on config
		var sig syscall.Signal
		switch p.Config.Stop.Signal {
		case "SIGKILL", "hard-kill":
			sig = syscall.SIGKILL
		case "SIGTERM":
			sig = syscall.SIGTERM
		default:
			sig = syscall.SIGINT
		}

		// Send signal directly to the process
		// PTY-spawned processes will propagate signals to children
		p.cmd.Process.Signal(sig)
	}

	// Close PTY after a short delay to allow graceful shutdown
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.mu.RLock()
		if p.ptyFile != nil {
			p.ptyFile.Close()
		}
		p.mu.RUnlock()
	}()

	// Force kill after timeout if still running
	go func() {
		time.Sleep(5 * time.Second)
		p.mu.RLock()
		if p.state == StateRunning && p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		p.mu.RUnlock()
	}()

	return nil
}

// Restart stops and starts the process
func (p *Process) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}

	// Wait for process to actually stop
	for i := 0; i < 50; i++ {
		if p.State() != StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return p.Start()
}

// Close cleans up resources
func (p *Process) Close() error {
	p.Stop()
	return p.Buffer.Close()
}
