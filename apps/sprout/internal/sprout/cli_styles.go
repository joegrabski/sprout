package sprout

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/gdamore/tcell/v2"
)

var (
	// Colors (Muted/Nord-inspired)
	ColorLime    = lipgloss.Color("#b4be82")
	ColorGreen   = lipgloss.Color("#a3be8c")
	ColorEmerald = lipgloss.Color("#8fbcbb")
	ColorCyan    = lipgloss.Color("#88c0d0")
	ColorBlue    = lipgloss.Color("#81a1c1")
	ColorPurple  = lipgloss.Color("#b48ead")
	ColorRed     = lipgloss.Color("#bf616a")
	ColorGray    = lipgloss.Color("#4c566a")

	// Unified Theme Colors (Exported for TUI)
	ThemeColorPrimary   = ColorGreen
	ThemeColorSecondary = ColorCyan
	ThemeColorAccent    = ColorLime
	ThemeColorMuted     = ColorGray

	// Styles
	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorPurple).
			Bold(true)

	StyleInfo = lipgloss.NewStyle().
			Foreground(ColorCyan)

	StyleBold = lipgloss.NewStyle().Bold(true)

	StyleFaint = lipgloss.NewStyle().Foreground(ColorGray)

	// Table/List Styles
	StyleHeader = lipgloss.NewStyle().
			Foreground(ThemeColorPrimary).
			Bold(true).
			MarginBottom(1)

	StyleTableHead = lipgloss.NewStyle().
			Foreground(ThemeColorPrimary).
			Bold(true).
			Underline(true)

	StyleCurrentWorktree = lipgloss.NewStyle().
				Foreground(ThemeColorAccent).
				Bold(true)

	StyleBranch = lipgloss.NewStyle().
			Foreground(ThemeColorSecondary)

	StyleDirty = lipgloss.NewStyle().
			Foreground(ColorRed)

	StyleClean = lipgloss.NewStyle().
			Foreground(ColorGreen)

	StyleDim = lipgloss.NewStyle().
			Foreground(ThemeColorMuted)

	StylePath = lipgloss.NewStyle().
			Foreground(ColorBlue)

	// Box styles for larger announcements
	StyleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ThemeColorPrimary).
			Padding(0, 1)
)

func SuccessMsg(msg string) string {
	return StyleSuccess.Render("✓ ") + msg
}

func ErrorMsg(msg string) string {
	return StyleError.Render("✗ ") + msg
}

func WarnMsg(msg string) string {
	return StyleWarning.Render("! ") + msg
}

func InfoMsg(msg string) string {
	return StyleInfo.Render("• ") + msg
}

// ColorToTcell converts a lipgloss Color to a tcell Color
func ColorToTcell(c lipgloss.Color) tcell.Color {
	// Simple conversion for basic hex colors
	return tcell.GetColor(string(c))
}
