package process

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/log"
)

// Regex to match cursor movement, screen control, and other non-color ANSI sequences.
// Preserves SGR color codes (ending in 'm') but removes everything else:
// - CSI sequences for cursor movement, erasing, scrolling, mode changes
// - OSC sequences (title setting, hyperlinks, etc.)
// - DEC private modes, save/restore cursor, alternate screen buffer
var ansiCursorControlRegex = regexp.MustCompile(
	`\x1b\[[0-9;]*[HJKfABCDEFGLMPSTXZdhlnqrsu]` + // CSI sequences (cursor, erase, scroll, mode)
		`|\x1b\[\?[0-9;]*[hlsru]` + // DEC private mode set/reset/save/restore
		`|\x1b[78DMEHcn=>]` + // Single-char escapes: save/restore cursor, index, etc.
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC sequences (terminated by BEL or ST)
		`|\x1b\([0-9A-Za-z]` + // Character set designation
		`|\x1b_[^\x1b]*\x1b\\` + // APC sequences
		`|\x1bP[^\x1b]*\x1b\\`, // DCS sequences
)

// Regex to match sequences that rewind the cursor to the start of the line.
// Tools like vite use \x1b[G or \x1b[1G (cursor to column 1) plus \x1b[2K (erase line)
// to overwrite progress output in-place. We treat these like \r.
var lineRewindRegex = regexp.MustCompile(`\x1b\[2K|\x1b\[1?G|\r`)

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

	mu           sync.RWMutex
	cmd          *exec.Cmd
	ptyFile      *os.File
	state        State
	exitCode     int
	startTime    time.Time
	cancelFunc   context.CancelFunc
	term         vt10x.Terminal // non-nil only while in alt-screen TUI mode
	viewportCols int            // last viewport width reported by UI
	viewportRows int            // last viewport height reported by UI
}

// IsInTuiMode returns true when the process is currently in alt-screen TUI mode.
// The UI uses this to enable key passthrough.
func (p *Process) IsInTuiMode() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.term != nil
}

// WriteInput sends raw bytes to the process PTY (for TUI key passthrough).
func (p *Process) WriteInput(data []byte) error {
	p.mu.RLock()
	f := p.ptyFile
	p.mu.RUnlock()
	if f == nil {
		return nil
	}
	_, err := f.Write(data)
	return err
}

// ResizeTui stores the current viewport dimensions and, if the process is
// currently in TUI mode, resizes the virtual terminal and PTY immediately.
// Called by the UI on every WindowSizeMsg for all processes.
func (p *Process) ResizeTui(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	p.mu.Lock()
	p.viewportCols = cols
	p.viewportRows = rows
	f := p.ptyFile
	term := p.term
	p.mu.Unlock()

	if f != nil && term != nil {
		term.Resize(cols, rows)
		pty.Setsize(f, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}) //nolint:errcheck
	}
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

	// Set initial PTY size; will be resized to viewport dimensions if/when TUI mode activates.
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 120})

	p.cmd = cmd
	p.ptyFile = ptmx
	p.state = StateRunning
	p.startTime = time.Now()
	p.mu.Unlock()

	// Stream PTY output with automatic alt-screen TUI detection.
	go p.streamAutoOutput(ptmx)

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
		// Handle line rewrites: split on \r, \x1b[G, \x1b[1G, \x1b[2K (cursor-to-column-1
		// and erase-line sequences). Tools like vite use these to update progress in-place.
		// We keep only the content after the last rewind point.
		parts := lineRewindRegex.Split(line, -1)
		line = parts[len(parts)-1]
		// Strip remaining cursor movement and screen control sequences (keep colors)
		line = ansiCursorControlRegex.ReplaceAllString(line, "")
		p.Buffer.Write(p.Name, "stdout", line)
	}
}

// tuiSize returns the viewport dimensions last reported by the UI, falling back
// to any explicit config overrides, then a sensible default.
func (p *Process) tuiSize() (cols, rows int) {
	p.mu.RLock()
	cols = p.viewportCols
	rows = p.viewportRows
	p.mu.RUnlock()

	if cols <= 0 {
		cols = p.Config.TuiCols
		if cols <= 0 {
			cols = 120
		}
	}
	if rows <= 0 {
		rows = p.Config.TuiRows
		if rows <= 0 {
			rows = 40
		}
	}
	return cols, rows
}

// streamAutoOutput reads raw PTY bytes and auto-detects when a process enters
// the alternate screen buffer (\x1b[?1049h). In normal mode it processes
// output line-by-line with ANSI stripping. In TUI mode it runs a vt10x virtual
// terminal and writes periodic screen snapshots to the log buffer.
// It reverts to normal mode when the process exits the alternate screen (\x1b[?1049l).
func (p *Process) streamAutoOutput(ptmx *os.File) {
	var (
		isTui    bool
		term     vt10x.Terminal
		tuiDone  chan struct{}
		lineFrag []byte // partial line carry-over between reads
	)

	startTUI := func(initialData []byte) {
		cols, rows := p.tuiSize()
		term = vt10x.New(vt10x.WithSize(cols, rows))

		p.mu.Lock()
		p.term = term
		if p.ptyFile != nil {
			pty.Setsize(p.ptyFile, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}) //nolint:errcheck
		}
		p.mu.Unlock()

		isTui = true
		lineFrag = nil

		if len(initialData) > 0 {
			term.Write(initialData) //nolint:errcheck
		}

		var prevLines []string
		tuiDone = make(chan struct{})
		ticker := time.NewTicker(150 * time.Millisecond)

		go func() {
			defer func() {
				ticker.Stop()
				p.mu.Lock()
				if p.term == term {
					p.term = nil
				}
				p.mu.Unlock()
			}()
			for {
				select {
				case <-tuiDone:
					return
				case <-ticker.C:
					term.Lock()
					lines := snapshotTuiScreen(term)
					term.Unlock()
					if tuiLinesEqual(lines, prevLines) {
						continue
					}
					prevLines = lines
					p.Buffer.Clear()
					for _, line := range lines {
						p.Buffer.Write(p.Name, "tui", line)
					}
				}
			}
		}()
	}

	stopTUI := func() {
		if tuiDone != nil {
			close(tuiDone)
			tuiDone = nil
		}
		isTui = false
		term = nil
		p.mu.Lock()
		p.term = nil
		if p.ptyFile != nil {
			pty.Setsize(p.ptyFile, &pty.Winsize{Rows: 24, Cols: 120}) //nolint:errcheck
		}
		p.mu.Unlock()
		p.Buffer.Clear()
	}

	flushLine := func(b []byte) {
		line := strings.TrimRight(string(b), "\r")
		parts := lineRewindRegex.Split(line, -1)
		line = parts[len(parts)-1]
		line = ansiCursorControlRegex.ReplaceAllString(line, "")
		p.Buffer.Write(p.Name, "stdout", line)
	}

	processNormal := func(data []byte) {
		combined := append(lineFrag, data...)
		lineFrag = nil
		for len(combined) > 0 {
			idx := bytes.IndexByte(combined, '\n')
			if idx < 0 {
				lineFrag = append([]byte(nil), combined...)
				return
			}
			flushLine(combined[:idx])
			combined = combined[idx+1:]
		}
	}

	altEnter := []byte("\x1b[?1049h")
	altExit := []byte("\x1b[?1049l")

	rawBuf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(rawBuf)
		if n > 0 {
			chunk := rawBuf[:n]
			if isTui {
				if idx := bytes.Index(chunk, altExit); idx >= 0 {
					term.Write(chunk[:idx+len(altExit)]) //nolint:errcheck
					stopTUI()
					if rest := chunk[idx+len(altExit):]; len(rest) > 0 {
						processNormal(rest)
					}
				} else {
					term.Write(chunk) //nolint:errcheck
				}
			} else {
				if idx := bytes.Index(chunk, altEnter); idx >= 0 {
					processNormal(chunk[:idx])
					startTUI(chunk[idx:])
				} else {
					processNormal(chunk)
				}
			}
		}
		if err != nil {
			if isTui && tuiDone != nil {
				close(tuiDone)
			}
			return
		}
	}
}

// snapshotTuiScreen renders the virtual terminal state as ANSI-colored text lines.
// Every cell in every row is emitted with its explicit color/attribute state so that
// background fills match the TUI's own background exactly — no trimming.
// Must be called with term.Lock() held.
func snapshotTuiScreen(term vt10x.Terminal) []string {
	cols, rows := term.Size()
	const (
		attrReverse   = 1 << 0
		attrUnderline = 1 << 1
		attrBold      = 1 << 2
		attrItalic    = 1 << 4
	)

	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		var prevFG, prevBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG
		var prevMode int16

		for x := 0; x < cols; x++ {
			g := term.Cell(x, y)

			if g.FG != prevFG || g.BG != prevBG || g.Mode != prevMode {
				sb.WriteString("\x1b[0m")
				if g.Mode&attrBold != 0 {
					sb.WriteString("\x1b[1m")
				}
				if g.Mode&attrItalic != 0 {
					sb.WriteString("\x1b[3m")
				}
				if g.Mode&attrUnderline != 0 {
					sb.WriteString("\x1b[4m")
				}
				if g.Mode&attrReverse != 0 {
					sb.WriteString("\x1b[7m")
				}
				if g.FG != vt10x.DefaultFG {
					sb.WriteString(tuiColorANSI(g.FG, true))
				}
				if g.BG != vt10x.DefaultBG {
					sb.WriteString(tuiColorANSI(g.BG, false))
				}
				prevFG = g.FG
				prevBG = g.BG
				prevMode = g.Mode
			}

			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteString("\x1b[0m")
		lines[y] = sb.String()
	}
	return lines
}

// tuiColorANSI converts a vt10x Color to an ANSI SGR escape sequence.
// fg=true for foreground color, fg=false for background.
func tuiColorANSI(c vt10x.Color, fg bool) string {
	if c.ANSI() {
		n := int(c)
		if fg {
			if n < 8 {
				return fmt.Sprintf("\x1b[%dm", 30+n)
			}
			return fmt.Sprintf("\x1b[%dm", 90+(n-8))
		}
		if n < 8 {
			return fmt.Sprintf("\x1b[%dm", 40+n)
		}
		return fmt.Sprintf("\x1b[%dm", 100+(n-8))
	}
	// 256-color palette
	if fg {
		return fmt.Sprintf("\x1b[38;5;%dm", c)
	}
	return fmt.Sprintf("\x1b[48;5;%dm", c)
}

// tuiLinesEqual returns true if two string slices are identical.
func tuiLinesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *Process) wait() {
	p.mu.RLock()
	cmd := p.cmd
	p.mu.RUnlock()

	if cmd == nil {
		return
	}

	err := cmd.Wait()

	p.mu.Lock()
	// Only update state if this is still the current command
	// (avoids race condition when process is restarted)
	if p.cmd == cmd {
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
	}
	p.mu.Unlock()

	// Handle autorestart (only if this was the current command)
	p.mu.RLock()
	shouldRestart := p.Config.Autorestart && p.cmd == cmd && time.Since(p.startTime) > time.Second
	p.mu.RUnlock()

	if shouldRestart {
		time.Sleep(100 * time.Millisecond)
		p.Start()
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
	// Capture the ptyFile reference so we close the OLD pty, not a new one
	ptyToClose := p.ptyFile
	go func() {
		time.Sleep(500 * time.Millisecond)
		if ptyToClose != nil {
			ptyToClose.Close()
		}
	}()

	// Force kill after timeout if still running
	// Capture the cmd reference so we kill the OLD process, not a new one
	cmdToKill := p.cmd
	go func() {
		time.Sleep(5 * time.Second)
		p.mu.RLock()
		// Only kill if this is still the same command
		if p.cmd == cmdToKill && p.state == StateRunning && cmdToKill != nil && cmdToKill.Process != nil {
			cmdToKill.Process.Kill()
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
