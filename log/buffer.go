package log

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Line represents a single log line
type Line struct {
	Timestamp time.Time
	Content   string
	Process   string
	Stream    string // "stdout" or "stderr"
}

// Buffer is a thread-safe ring buffer for log lines with search capability
type Buffer struct {
	mu          sync.RWMutex
	lines       []Line
	capacity    int
	head        int
	count       int
	logFile     *os.File
	logWriter   *bufio.Writer
	subscribers []chan Line
	subMu       sync.RWMutex
}

// NewBuffer creates a new log buffer with the given capacity
func NewBuffer(capacity int, logDir string, processName string) (*Buffer, error) {
	b := &Buffer{
		lines:    make([]Line, capacity),
		capacity: capacity,
	}

	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, err
		}
		logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", processName))
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		b.logFile = f
		b.logWriter = bufio.NewWriter(f)
	}

	return b, nil
}

// Write adds a log line to the buffer
func (b *Buffer) Write(process, stream, content string) {
	line := Line{
		Timestamp: time.Now(),
		Content:   content,
		Process:   process,
		Stream:    stream,
	}

	b.mu.Lock()
	idx := (b.head + b.count) % b.capacity
	b.lines[idx] = line
	if b.count < b.capacity {
		b.count++
	} else {
		b.head = (b.head + 1) % b.capacity
	}

	// Write to file if configured
	if b.logWriter != nil {
		fmt.Fprintf(b.logWriter, "[%s] %s\n", line.Timestamp.Format("15:04:05"), content)
		b.logWriter.Flush()
	}
	b.mu.Unlock()

	// Notify subscribers
	b.subMu.RLock()
	for _, ch := range b.subscribers {
		select {
		case ch <- line:
		default:
			// Drop if channel is full
		}
	}
	b.subMu.RUnlock()
}

// Subscribe returns a channel that receives new log lines
func (b *Buffer) Subscribe() chan Line {
	ch := make(chan Line, 100)
	b.subMu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel
func (b *Buffer) Unsubscribe(ch chan Line) {
	b.subMu.Lock()
	defer b.subMu.Unlock()
	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Lines returns all lines in order (oldest to newest)
func (b *Buffer) Lines() []Line {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]Line, b.count)
	for i := 0; i < b.count; i++ {
		idx := (b.head + i) % b.capacity
		result[i] = b.lines[idx]
	}
	return result
}

// Count returns the number of lines in the buffer
func (b *Buffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Search performs case-insensitive substring search and returns matching line indices
func (b *Buffer) Search(query string) []int {
	if query == "" {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	query = strings.ToLower(query)
	var matches []int

	for i := 0; i < b.count; i++ {
		idx := (b.head + i) % b.capacity
		if strings.Contains(strings.ToLower(b.lines[idx].Content), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

// SearchRegex performs regex search and returns matching line indices
func (b *Buffer) SearchRegex(pattern string) ([]int, error) {
	if pattern == "" {
		return nil, nil
	}

	re, err := regexp.Compile("(?i)" + pattern) // case-insensitive
	if err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	var matches []int
	for i := 0; i < b.count; i++ {
		idx := (b.head + i) % b.capacity
		if re.MatchString(b.lines[idx].Content) {
			matches = append(matches, i)
		}
	}
	return matches, nil
}

// GetLine returns a specific line by index
func (b *Buffer) GetLine(i int) (Line, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if i < 0 || i >= b.count {
		return Line{}, false
	}

	idx := (b.head + i) % b.capacity
	return b.lines[idx], true
}

// Clear removes all lines from the buffer and notifies subscribers
func (b *Buffer) Clear() {
	b.mu.Lock()
	b.head = 0
	b.count = 0
	for i := range b.lines {
		b.lines[i] = Line{}
	}
	b.mu.Unlock()

	// Notify subscribers about the clear
	clearLine := Line{Stream: "clear"}
	b.subMu.RLock()
	for _, ch := range b.subscribers {
		select {
		case ch <- clearLine:
		default:
		}
	}
	b.subMu.RUnlock()
}

// Close closes the log file if open
func (b *Buffer) Close() error {
	if b.logWriter != nil {
		b.logWriter.Flush()
	}
	if b.logFile != nil {
		return b.logFile.Close()
	}
	return nil
}
