// Package ui provides terminal UI components using Bubble Tea and Lipgloss.
package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// AIDEV-NOTE: This file defines the visual theme for all UI components.
// Colors are chosen for readability on both dark and light terminals.

// Colors using adaptive colors for light/dark terminal support
var (
	// Primary colors
	ColorPrimary   = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"} // Purple
	ColorSuccess   = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#10B981"} // Green
	ColorWarning   = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#F59E0B"} // Yellow/Orange
	ColorError     = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#EF4444"} // Red
	ColorMuted     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"} // Gray
	ColorHighlight = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#3B82F6"} // Blue
	ColorWhite     = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}
	ColorBlack     = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#000000"}
)

// Text styles
var (
	// TitleStyle for main headers
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	// SubtitleStyle for secondary headers
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// SelectedStyle for highlighted/selected items
	SelectedStyle = lipgloss.NewStyle().
			Bold(true).
			Background(ColorPrimary).
			Foreground(ColorWhite).
			Padding(0, 1)

	// CursorStyle for the selection cursor
	CursorStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	// ErrorStyle for error messages
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError)

	// SuccessStyle for success messages
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	// WarningStyle for warning messages
	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	// MutedStyle for less important text
	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// HighlightStyle for emphasized text
	HighlightStyle = lipgloss.NewStyle().
			Foreground(ColorHighlight).
			Bold(true)

	// BoldStyle for bold text
	BoldStyle = lipgloss.NewStyle().
			Bold(true)

	// DimStyle for dimmed text
	DimStyle = lipgloss.NewStyle().
			Faint(true)
)

// Status indicators
var (
	// SyncedIndicator shows a bookmark is synced with remote
	SyncedIndicator = SuccessStyle.Render("●")

	// UnsyncedIndicator shows a bookmark needs to be pushed
	UnsyncedIndicator = WarningStyle.Render("○")

	// ErrorIndicator shows an error state
	ErrorIndicator = ErrorStyle.Render("✗")

	// SuccessIndicator shows success
	SuccessIndicator = SuccessStyle.Render("✓")

	// PendingIndicator shows pending/in-progress state
	PendingIndicator = MutedStyle.Render("◌")

	// CurrentIndicator shows the current item
	CurrentIndicator = HighlightStyle.Render("→")
)

// Box styles
var (
	// BoxStyle for bordered containers
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	// FocusedBoxStyle for focused bordered containers
	FocusedBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1)

	// HeaderBoxStyle for header sections
	HeaderBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorMuted).
			BorderBottom(true).
			BorderTop(false).
			BorderLeft(false).
			BorderRight(false).
			Padding(0, 0, 1, 0)
)

// List item styles
var (
	// ListItemStyle for normal list items
	ListItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	// SelectedListItemStyle for selected list items
	SelectedListItemStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(ColorPrimary).
				Bold(true)
)

// Help styles
var (
	// HelpKeyStyle for help key bindings
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(true)

	// HelpDescStyle for help descriptions
	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// HelpSepStyle for help separators
	HelpSepStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			SetString("  ")
)

// Progress styles
var (
	// ProgressBarFilledStyle for filled portion of progress bar
	ProgressBarFilledStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				SetString("█")

	// ProgressBarEmptyStyle for empty portion of progress bar
	ProgressBarEmptyStyle = lipgloss.NewStyle().
				Foreground(ColorMuted).
				SetString("░")
)

// noColorEnabled tracks if colors are disabled
var noColorEnabled bool

// init checks for NO_COLOR environment variable
func init() {
	if os.Getenv("NO_COLOR") != "" {
		DisableColors()
	}
}

// DisableColors removes all colors from styles (for --no-color flag)
func DisableColors() {
	noColorEnabled = true

	// Reset all styles to remove colors
	TitleStyle = lipgloss.NewStyle().Bold(true)
	SubtitleStyle = lipgloss.NewStyle()
	SelectedStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	CursorStyle = lipgloss.NewStyle().Bold(true)
	ErrorStyle = lipgloss.NewStyle()
	SuccessStyle = lipgloss.NewStyle()
	WarningStyle = lipgloss.NewStyle()
	MutedStyle = lipgloss.NewStyle()
	HighlightStyle = lipgloss.NewStyle().Bold(true)
	BoldStyle = lipgloss.NewStyle().Bold(true)
	DimStyle = lipgloss.NewStyle().Faint(true)

	// Reset indicators to plain text
	SyncedIndicator = "●"
	UnsyncedIndicator = "○"
	ErrorIndicator = "✗"
	SuccessIndicator = "✓"
	PendingIndicator = "◌"
	CurrentIndicator = "→"

	// Reset box styles
	BoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	FocusedBoxStyle = BoxStyle.Bold(true)
	HeaderBoxStyle = lipgloss.NewStyle().BorderBottom(true)

	// Reset list styles
	ListItemStyle = lipgloss.NewStyle().PaddingLeft(2)
	SelectedListItemStyle = lipgloss.NewStyle().PaddingLeft(0).Bold(true)

	// Reset help styles
	HelpKeyStyle = lipgloss.NewStyle().Bold(true)
	HelpDescStyle = lipgloss.NewStyle()
	HelpSepStyle = lipgloss.NewStyle().SetString("  ")

	// Reset progress styles
	ProgressBarFilledStyle = lipgloss.NewStyle().SetString("█")
	ProgressBarEmptyStyle = lipgloss.NewStyle().SetString("░")
}

// IsNoColor returns true if colors are disabled
func IsNoColor() bool {
	return noColorEnabled
}

// RenderStack formats a stack of bookmarks for display
func RenderStack(bookmarks []string, baseBranch string) string {
	if len(bookmarks) == 0 {
		return MutedStyle.Render("(empty stack)")
	}

	result := ""
	for i := len(bookmarks) - 1; i >= 0; i-- {
		if i < len(bookmarks)-1 {
			result += MutedStyle.Render(" → ")
		}
		result += BoldStyle.Render(bookmarks[i])
	}
	result += MutedStyle.Render(" → ") + baseBranch

	return result
}

// RenderBookmarkStatus renders a bookmark name with its sync status indicator
func RenderBookmarkStatus(name string, synced bool) string {
	indicator := UnsyncedIndicator
	if synced {
		indicator = SyncedIndicator
	}
	return indicator + " " + name
}

// RenderKeyHelp renders a key binding help item
func RenderKeyHelp(key, desc string) string {
	return HelpKeyStyle.Render("["+key+"]") + " " + HelpDescStyle.Render(desc)
}

// RenderProgressBar renders a progress bar of the given width
func RenderProgressBar(current, total, width int) string {
	if total == 0 {
		return ""
	}

	filled := (current * width) / total
	empty := width - filled

	result := ""
	for i := 0; i < filled; i++ {
		result += ProgressBarFilledStyle.String()
	}
	for i := 0; i < empty; i++ {
		result += ProgressBarEmptyStyle.String()
	}

	return "[" + result + "]"
}
