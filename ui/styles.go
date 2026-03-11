package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#10B981") // Green
	errorColor     = lipgloss.Color("#EF4444") // Red
	warningColor   = lipgloss.Color("#F59E0B") // Yellow
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	bgColor        = lipgloss.Color("#1F2937") // Dark gray
	borderColor    = lipgloss.Color("#374151") // Border gray

	// Process states
	runningStyle = lipgloss.NewStyle().Foreground(secondaryColor)
	stoppedStyle = lipgloss.NewStyle().Foreground(mutedColor)
	exitedStyle  = lipgloss.NewStyle().Foreground(errorColor)

	// List styles
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(primaryColor)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB"))

	// Pane styles
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor)

	focusedPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor)

	// Title styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			PaddingLeft(1)

	// Log styles
	timestampStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	stderrStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	// Search style - orange bg for active match
	searchActiveMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#F59E0B")).
				Foreground(lipgloss.Color("#000000")).
				Bold(true)

	searchInputStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				Padding(0, 1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(bgColor).
			Foreground(lipgloss.Color("#E5E7EB")).
			Padding(0, 1)

	statusKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor)

	// Help text
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
)
