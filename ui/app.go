package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/speakeasy-api/madprocs/log"
	"github.com/speakeasy-api/madprocs/process"
)

// Focus represents which pane has focus
type Focus int

const (
	FocusList Focus = iota
	FocusLogs
	FocusSearch
)

// SearchMode represents the type of search
type SearchMode int

const (
	SearchSubstring SearchMode = iota
	SearchRegex
)

func (m SearchMode) String() string {
	switch m {
	case SearchRegex:
		return "regex"
	default:
		return "text"
	}
}

// LogLineMsg is sent when a new log line arrives
type LogLineMsg struct {
	Line log.Line
}

// Match represents a single search match with line and position
type Match struct {
	LineIdx int
	Start   int
	End     int
}

// Model is the main Bubbletea model
type Model struct {
	manager        *process.Manager
	webPort        int
	webHost        string
	webTLS         bool
	width          int
	height         int
	focus          Focus
	selected       int
	viewport       viewport.Model
	searchInput    string
	searchMode     SearchMode
	searchActive   bool
	matches        []Match
	matchIndex     int
	zoomed         bool
	logLines       []string
	lineToViewport map[int]int // maps original line index to viewport line
	logSub         chan log.Line
	ready          bool
}

// NewModel creates a new UI model
func NewModel(manager *process.Manager, webPort int, webHost string, webTLS bool) Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	return Model{
		manager:  manager,
		webPort:  webPort,
		webHost:  webHost,
		webTLS:   webTLS,
		viewport: vp,
		logLines: []string{},
	}
}

// keyMap defines the key bindings
type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	Start       key.Binding
	Stop        key.Binding
	Restart     key.Binding
	Search      key.Binding
	RegexSearch key.Binding
	NextMatch   key.Binding
	PrevMatch   key.Binding
	Escape      key.Binding
	Tab         key.Binding
	Clear       key.Binding
	Zoom        key.Binding
	Web         key.Binding
	Quit        key.Binding
	Enter       key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Start: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "start"),
	),
	Stop: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "stop"),
	),
	Restart: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "restart"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	RegexSearch: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "regex"),
	),
	NextMatch: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next"),
	),
	PrevMatch: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "prev"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch pane"),
	),
	Clear: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear"),
	),
	Zoom: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "toggle sidebar"),
	),
	Web: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "web"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshLogs(),
		m.tick(),
	)
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m Model) waitForLog() tea.Cmd {
	return func() tea.Msg {
		if m.logSub == nil {
			return nil
		}
		line, ok := <-m.logSub
		if !ok {
			return nil
		}
		return LogLineMsg{Line: line}
	}
}

func (m Model) refreshLogs() tea.Cmd {
	return func() tea.Msg {
		return refreshLogsMsg{}
	}
}

type refreshLogsMsg struct{}
type tickMsg struct{}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewportSize()
		// Resize TUI processes to match the log viewport
		for _, proc := range m.manager.List() {
			if proc.IsTui() {
				proc.ResizeTui(m.viewport.Width, m.viewport.Height)
			}
		}
		// Subscribe on first window size message (initial setup)
		if !m.ready {
			m.ready = true
			procs := m.manager.List()
			if len(procs) > 0 && m.logSub == nil {
				m.logSub = procs[m.selected].Buffer.Subscribe()
				cmds = append(cmds, m.waitForLog())
			}
		}

	case tea.MouseMsg:
		// Handle mouse events
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			// Scroll log viewport up
			if msg.X > 22 { // In log pane area
				m.viewport.SetYOffset(m.viewport.YOffset - 3)
			}
		case tea.MouseButtonWheelDown:
			// Scroll log viewport down
			if msg.X > 22 { // In log pane area
				m.viewport.SetYOffset(m.viewport.YOffset + 3)
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				// Check if click is in the process list area (first ~24 columns)
				// Layout: border(1) + title(1) = processes start at Y=2
				listWidth := 24
				headerRows := 2 // border + title

				if msg.X < listWidth && msg.Y >= headerRows {
					// Calculate which process was clicked
					clickedIdx := msg.Y - headerRows
					procs := m.manager.List()
					if clickedIdx >= 0 && clickedIdx < len(procs) {
						m.selected = clickedIdx
						m.focus = FocusList
						m.onProcessSelected()
						cmds = append(cmds, m.refreshLogs())
					}
				} else if msg.X >= listWidth {
					// Clicked in log area
					m.focus = FocusLogs
				}
			}
		}

	case tickMsg:
		// Periodic refresh for process state updates
		cmds = append(cmds, m.tick())

	case refreshLogsMsg:
		m.updateLogContent()
		m.viewport.GotoBottom() // Start at bottom for initial load
		cmds = append(cmds, m.waitForLog())

	case LogLineMsg:
		wasAtBottom := m.viewport.AtBottom() || m.viewport.TotalLineCount() <= m.viewport.Height
		// Re-run search if active to pick up new matches
		if m.searchActive && m.searchInput != "" {
			m.performSearch()
		} else {
			m.updateLogContent()
		}
		// Auto-scroll to bottom to follow new output (only if not in search mode)
		if wasAtBottom && !m.searchActive {
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, m.waitForLog())

	case tea.KeyMsg:
		// TUI passthrough: when log pane is focused on a TUI process, forward all
		// keys to the PTY. Tab or Esc returns focus to the process list.
		if m.focus == FocusLogs {
			procs := m.manager.List()
			if m.selected < len(procs) && procs[m.selected].IsTui() {
				if key.Matches(msg, keys.Tab) {
					m.focus = FocusList
				} else if data := keyMsgToBytes(msg); len(data) > 0 {
					procs[m.selected].WriteInput(data) //nolint:errcheck
				}
				return m, tea.Batch(cmds...)
			}
		}

		// Handle search input first
		if m.searchActive && m.focus == FocusSearch {
			switch {
			case key.Matches(msg, keys.Escape):
				m.searchActive = false
				m.searchInput = ""
				m.matches = nil
				m.focus = FocusList
				m.updateLogContent()
			case key.Matches(msg, keys.Enter):
				// Go to next match
				if len(m.matches) > 0 {
					m.matchIndex = (m.matchIndex + 1) % len(m.matches)
					m.scrollToMatch()
					m.updateLogContent()
				}
			case key.Matches(msg, keys.Tab):
				// Cycle search mode
				m.searchMode = (m.searchMode + 1) % 3
				m.performSearch()
			case msg.Type == tea.KeyBackspace:
				if len(m.searchInput) > 0 {
					m.searchInput = m.searchInput[:len(m.searchInput)-1]
					m.performSearch()
				}
			case msg.Type == tea.KeySpace:
				m.searchInput += " "
				m.performSearch()
			case msg.Type == tea.KeyRunes:
				m.searchInput += string(msg.Runes)
				m.performSearch()
			}
			return m, tea.Batch(cmds...)
		}

		// Global keys
		switch {
		case key.Matches(msg, keys.Quit):
			m.manager.Close()
			return m, tea.Quit

		case key.Matches(msg, keys.Search):
			m.searchMode = SearchSubstring
			m.searchActive = true
			m.focus = FocusSearch
			m.searchInput = ""

		case key.Matches(msg, keys.RegexSearch):
			m.searchMode = SearchRegex
			m.searchActive = true
			m.focus = FocusSearch
			m.searchInput = ""

		case key.Matches(msg, keys.Tab):
			if m.focus == FocusList {
				m.focus = FocusLogs
			} else {
				m.focus = FocusList
			}

		case key.Matches(msg, keys.Zoom):
			m.zoomed = !m.zoomed
			m.updateViewportSize()

		case key.Matches(msg, keys.Web):
			m.openWebUI()

		case key.Matches(msg, keys.NextMatch):
			if len(m.matches) > 0 {
				m.matchIndex = (m.matchIndex + 1) % len(m.matches)
				m.scrollToMatch()
				m.updateLogContent()
			}

		case key.Matches(msg, keys.PrevMatch):
			if len(m.matches) > 0 {
				m.matchIndex = (m.matchIndex - 1 + len(m.matches)) % len(m.matches)
				m.scrollToMatch()
				m.updateLogContent()
			}

		case key.Matches(msg, keys.Clear):
			procs := m.manager.List()
			if m.selected < len(procs) {
				procs[m.selected].Buffer.Clear()
				m.matches = nil
				m.matchIndex = 0
				m.updateLogContent()
			}

		case key.Matches(msg, keys.Escape):
			if m.searchActive {
				m.searchActive = false
				m.searchInput = ""
				m.matches = nil
				m.updateLogContent()
			}
			m.focus = FocusList
		}

		// List-specific keys
		if m.focus == FocusList {
			procs := m.manager.List()
			switch {
			case key.Matches(msg, keys.Up):
				if m.selected > 0 {
					m.selected--
					m.onProcessSelected()
					cmds = append(cmds, m.refreshLogs())
				}

			case key.Matches(msg, keys.Down):
				if m.selected < len(procs)-1 {
					m.selected++
					m.onProcessSelected()
					cmds = append(cmds, m.refreshLogs())
				}

			case key.Matches(msg, keys.Start):
				if m.selected < len(procs) {
					procs[m.selected].Start()
				}

			case key.Matches(msg, keys.Stop):
				if m.selected < len(procs) {
					procs[m.selected].Stop()
				}

			case key.Matches(msg, keys.Restart):
				if m.selected < len(procs) {
					procs[m.selected].Buffer.Clear()
					procs[m.selected].Restart()
					// Clear search state since logs are cleared
					m.matches = nil
					m.matchIndex = 0
					m.updateLogContent()
				}

			}
		}

		// Logs-specific keys (viewport scrolling)
		if m.focus == FocusLogs {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	// Always pass mouse events to viewport for scrolling
	if _, ok := msg.(tea.MouseMsg); ok {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) onProcessSelected() {
	procs := m.manager.List()
	if m.selected >= len(procs) {
		return
	}

	// Unsubscribe from old process
	if m.logSub != nil {
		// Find the old process to unsubscribe
		for _, p := range procs {
			p.Buffer.Unsubscribe(m.logSub)
		}
	}

	// Subscribe to new process
	proc := procs[m.selected]
	m.logSub = proc.Buffer.Subscribe()
	m.matches = nil
	m.searchInput = ""
	m.searchActive = false
}

func (m *Model) updateViewportSize() {
	listWidth := 20
	if m.zoomed {
		listWidth = 0
	}

	// Pane height is m.height - 4 (status bar + search bar), viewport fills the pane
	paneHeight := m.height - 4

	logWidth := m.width - listWidth - 4 // borders on both panes (2+2)
	logHeight := paneHeight

	if logHeight < 1 {
		logHeight = 1
	}
	if logWidth < 1 {
		logWidth = 1
	}

	m.viewport.Width = logWidth
	m.viewport.Height = logHeight
}

func (m *Model) updateLogContent() {
	procs := m.manager.List()
	if m.selected >= len(procs) {
		return
	}

	proc := procs[m.selected]
	lines := proc.Buffer.Lines()

	// Calculate available width for content (subtract timestamp width)
	contentWidth := m.viewport.Width - 12 // [HH:MM:SS] + space
	if contentWidth < 20 {
		contentWidth = 20
	}

	var wrappedLines []string
	m.lineToViewport = make(map[int]int)

	for i, line := range lines {
		// Track which viewport line this original line starts at
		m.lineToViewport[i] = len(wrappedLines)

		content := line.Content

		// TUI snapshot lines: render as-is without timestamp or word-wrap.
		// The content is already a fixed-width rendered screen cell row.
		if line.Stream == "tui" {
			if m.searchInput != "" {
				if match := m.getActiveMatchForLine(i); match != nil {
					content = m.highlightMatch(content, match)
				}
			}
			wrappedLines = append(wrappedLines, content)
			continue
		}

		ts := timestampStyle.Render(line.Timestamp.Format("[15:04:05]"))

		// Highlight only the active match (orange background)
		if m.searchInput != "" {
			if match := m.getActiveMatchForLine(i); match != nil {
				content = m.highlightMatch(content, match)
			}
		}

		if line.Stream == "stderr" {
			content = stderrStyle.Render(content)
		}

		// Wrap long lines
		wrapped := wrapText(content, contentWidth)
		for j, wline := range wrapped {
			if j == 0 {
				wrappedLines = append(wrappedLines, fmt.Sprintf("%s %s", ts, wline))
			} else {
				// Continuation lines get indented
				wrappedLines = append(wrappedLines, fmt.Sprintf("           %s", wline))
			}
		}
	}

	m.logLines = wrappedLines
	m.viewport.SetContent(strings.Join(m.logLines, "\n"))
}

// wrapText wraps text to the specified width using wordwrap which handles ANSI codes
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	// wordwrap.String handles ANSI escape codes properly
	wrapped := wordwrap.String(text, width)
	return strings.Split(wrapped, "\n")
}

// getActiveMatchForLine returns the active match if it's on this line, nil otherwise
func (m *Model) getActiveMatchForLine(lineIdx int) *Match {
	if m.matchIndex >= 0 && m.matchIndex < len(m.matches) {
		match := m.matches[m.matchIndex]
		if match.LineIdx == lineIdx {
			return &match
		}
	}
	return nil
}

func (m *Model) highlightMatch(content string, match *Match) string {
	if m.searchInput == "" || match == nil {
		return content
	}

	start := match.Start
	end := match.End

	// Safety bounds check
	if start < 0 || end > len(content) || start >= end {
		return content
	}

	before := content[:start]
	matchText := content[start:end]
	after := content[end:]

	return before + searchActiveMatchStyle.Render(matchText) + after
}

func (m *Model) performSearch() {
	procs := m.manager.List()
	if m.selected >= len(procs) {
		return
	}

	proc := procs[m.selected]
	lines := proc.Buffer.Lines()

	// Get line indices that match
	var lineIndices []int
	switch m.searchMode {
	case SearchRegex:
		indices, err := proc.Buffer.SearchRegex(m.searchInput)
		if err == nil {
			lineIndices = indices
		}
	default:
		lineIndices = proc.Buffer.Search(m.searchInput)
	}

	// For each matching line, find all occurrences of the search term
	m.matches = nil
	query := strings.ToLower(m.searchInput)
	queryLen := len(m.searchInput)

	for _, lineIdx := range lineIndices {
		if lineIdx >= len(lines) {
			continue
		}
		content := lines[lineIdx].Content
		lower := strings.ToLower(content)

		// Find all occurrences in this line
		offset := 0
		for {
			idx := strings.Index(lower[offset:], query)
			if idx == -1 {
				break
			}
			start := offset + idx
			m.matches = append(m.matches, Match{
				LineIdx: lineIdx,
				Start:   start,
				End:     start + queryLen,
			})
			offset = start + 1 // Move past this match to find next one
		}
	}

	m.matchIndex = 0
	if len(m.matches) > 0 {
		m.scrollToMatch()
	}
	m.updateLogContent()
}

func (m *Model) scrollToMatch() {
	if m.matchIndex >= 0 && m.matchIndex < len(m.matches) {
		match := m.matches[m.matchIndex]
		// Use the mapping to get the viewport line number
		if viewportLine, ok := m.lineToViewport[match.LineIdx]; ok {
			m.viewport.SetYOffset(viewportLine)
		} else {
			m.viewport.SetYOffset(match.LineIdx)
		}
	}
}

func (m *Model) openWebUI() {
	protocol := "http"
	if m.webTLS {
		protocol = "https"
	}
	host := m.webHost
	if host == "" {
		host = "localhost"
	}
	url := fmt.Sprintf("%s://%s:%d", protocol, host, m.webPort)

	// Pass the currently selected process via URL hash
	procs := m.manager.List()
	if m.selected >= 0 && m.selected < len(procs) {
		url += "#process=" + procs[m.selected].Name
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Process list
	listContent := m.renderProcessList()

	// Log viewer
	logContent := m.renderLogViewer()

	// Combine panes
	var mainContent string
	if m.zoomed {
		mainContent = logContent
	} else {
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, listContent, logContent)
	}

	// Search bar (always present to maintain consistent layout)
	searchBar := m.renderSearchBar()
	mainContent = lipgloss.JoinVertical(lipgloss.Left, mainContent, searchBar)

	// Status bar
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, statusBar)
}

func (m Model) renderProcessList() string {
	procs := m.manager.List()
	listWidth := 20
	innerWidth := listWidth - 2 // account for borders

	var items []string
	items = append(items, titleStyle.Render("Processes"))

	for i, proc := range procs {
		var stateIcon string
		var stateColor lipgloss.Style

		switch proc.State() {
		case process.StateRunning:
			stateIcon = "●"
			stateColor = runningStyle
		case process.StateStopped:
			stateIcon = "○"
			stateColor = stoppedStyle
		case process.StateExited:
			stateIcon = "✗"
			stateColor = exitedStyle
		}

		// Format: icon + space + name, full width
		icon := stateColor.Render(stateIcon)
		if i == m.selected {
			text := fmt.Sprintf(" %s %s", stateIcon, proc.Name)
			// Pad to full width
			padding := innerWidth - lipgloss.Width(text)
			if padding > 0 {
				text += strings.Repeat(" ", padding)
			}
			items = append(items, selectedStyle.Render(text))
		} else {
			name := fmt.Sprintf(" %s %s", icon, proc.Name)
			items = append(items, normalStyle.Render(name))
		}
	}

	content := strings.Join(items, "\n")

	pane := paneStyle
	if m.focus == FocusList {
		pane = focusedPaneStyle
	}

	paneHeight := m.height - 4 // leave room for status bar + search bar

	return pane.Width(listWidth).Height(paneHeight).Render(content)
}

func (m Model) renderLogViewer() string {
	pane := paneStyle
	if m.focus == FocusLogs {
		pane = focusedPaneStyle
	}

	listWidth := 20
	if m.zoomed {
		listWidth = 0
	}

	paneHeight := m.height - 4 // leave room for status bar + search bar
	logWidth := m.width - listWidth - 2

	return pane.Width(logWidth).Height(paneHeight).Render(m.viewport.View())
}

func (m Model) renderSearchBar() string {
	if !m.searchActive {
		// Empty line to maintain layout
		return ""
	}

	modeStr := m.searchMode.String()
	matchInfo := ""
	if len(m.matches) > 0 {
		// Show current match position info for debugging
		currentMatch := m.matches[m.matchIndex]
		matchInfo = fmt.Sprintf(" [%d/%d line:%d pos:%d-%d]", m.matchIndex+1, len(m.matches), currentMatch.LineIdx, currentMatch.Start, currentMatch.End)
	} else if m.searchInput != "" {
		matchInfo = " [no matches]"
	}

	prompt := fmt.Sprintf("Search (%s)%s: %s", modeStr, matchInfo, m.searchInput)
	return searchInputStyle.Width(m.width - 4).Render(prompt + "█")
}

// keyMsgToBytes converts a bubbletea key message to the raw byte sequence
// that should be sent to a PTY for TUI key passthrough.
func keyMsgToBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{127}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEsc:
		return []byte{'\x1b'}
	case tea.KeyUp:
		return []byte{'\x1b', '[', 'A'}
	case tea.KeyDown:
		return []byte{'\x1b', '[', 'B'}
	case tea.KeyRight:
		return []byte{'\x1b', '[', 'C'}
	case tea.KeyLeft:
		return []byte{'\x1b', '[', 'D'}
	case tea.KeyPgUp:
		return []byte{'\x1b', '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{'\x1b', '[', '6', '~'}
	case tea.KeyHome:
		return []byte{'\x1b', '[', 'H'}
	case tea.KeyEnd:
		return []byte{'\x1b', '[', 'F'}
	case tea.KeyDelete:
		return []byte{'\x1b', '[', '3', '~'}
	case tea.KeyCtrlA:
		return []byte{1}
	case tea.KeyCtrlB:
		return []byte{2}
	case tea.KeyCtrlC:
		return []byte{3}
	case tea.KeyCtrlD:
		return []byte{4}
	case tea.KeyCtrlE:
		return []byte{5}
	case tea.KeyCtrlF:
		return []byte{6}
	case tea.KeyCtrlG:
		return []byte{7}
	case tea.KeyCtrlH:
		return []byte{8}
	case tea.KeyCtrlJ:
		return []byte{'\n'}
	case tea.KeyCtrlK:
		return []byte{11}
	case tea.KeyCtrlL:
		return []byte{12}
	case tea.KeyCtrlN:
		return []byte{14}
	case tea.KeyCtrlO:
		return []byte{15}
	case tea.KeyCtrlP:
		return []byte{16}
	case tea.KeyCtrlQ:
		return []byte{17}
	case tea.KeyCtrlR:
		return []byte{18}
	case tea.KeyCtrlS:
		return []byte{19}
	case tea.KeyCtrlT:
		return []byte{20}
	case tea.KeyCtrlU:
		return []byte{21}
	case tea.KeyCtrlV:
		return []byte{22}
	case tea.KeyCtrlW:
		return []byte{23}
	case tea.KeyCtrlX:
		return []byte{24}
	case tea.KeyCtrlY:
		return []byte{25}
	case tea.KeyCtrlZ:
		return []byte{26}
	}
	return nil
}

func (m Model) renderStatusBar() string {
	running := m.manager.RunningCount()
	total := m.manager.Count()

	left := fmt.Sprintf(" %d/%d running", running, total)

	// Show TUI passthrough hint when log pane focused on a TUI process
	procs := m.manager.List()
	inTuiPassthrough := m.focus == FocusLogs && m.selected < len(procs) && procs[m.selected].IsTui()

	var help []string
	if inTuiPassthrough {
		help = []string{
			statusKeyStyle.Render("tab") + ":exit tui mode",
		}
	} else {
		help = []string{
			statusKeyStyle.Render("q") + ":quit",
			statusKeyStyle.Render("s") + ":start",
			statusKeyStyle.Render("x") + ":stop",
			statusKeyStyle.Render("r") + ":restart",
			statusKeyStyle.Render("c") + ":clear",
			statusKeyStyle.Render("/") + ":search",
			statusKeyStyle.Render("?") + ":regex",
			statusKeyStyle.Render("t") + ":sidebar",
			statusKeyStyle.Render("w") + ":open web ui",
			statusKeyStyle.Render("⇧") + ":select",
		}
	}
	right := strings.Join(help, " ")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 0 {
		gap = 0
	}

	return statusBarStyle.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}
