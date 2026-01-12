package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: GraphModel is the main interactive view showing all bookmark stacks.
// It's displayed when running jj-stacked with no arguments.

// GraphMode represents the current mode of the graph view.
type GraphMode int

const (
	ModeNormal GraphMode = iota
	ModeSelecting
	ModeHelp
)

// RefreshFunc is a function that refreshes the change graph.
type RefreshFunc func() (*jjutils.ChangeGraph, error)

// GraphModel is a Bubble Tea model for the interactive graph view.
type GraphModel struct {
	// Data
	graph  *jjutils.ChangeGraph
	stacks []jjutils.BranchStack

	// UI state
	cursor    int               // Currently selected item (flattened index)
	expanded  map[string]bool   // Expanded bookmarks (show changes)
	viewport  viewport.Model    // For scrolling
	width     int               // Terminal width
	height    int               // Terminal height
	ready     bool              // Viewport initialized

	// Mode
	mode             GraphMode
	selectedBookmark string
	statusMessage    string
	errorMessage     string

	// Flattened list for navigation
	items []graphItem

	// Refresh function (optional)
	refreshFn RefreshFunc
}

// graphItem represents a navigable item in the graph.
type graphItem struct {
	bookmark    string
	stack       int  // Stack index
	segment     int  // Segment index within stack
	isExpanded  bool
	isSynced    bool
	changeCount int
}

// GraphRefreshMsg is sent to refresh the graph data.
type GraphRefreshMsg struct {
	Graph *jjutils.ChangeGraph
	Error error
}

// GraphSelectMsg is sent when a bookmark is selected for submission.
type GraphSelectMsg struct {
	Bookmark string
}

// NewGraphModel creates a new graph view model.
func NewGraphModel(graph *jjutils.ChangeGraph) GraphModel {
	m := GraphModel{
		graph:    graph,
		expanded: make(map[string]bool),
	}

	if graph != nil {
		m.stacks = graph.Stacks
		m.buildItems()
	}

	return m
}

// buildItems creates the flattened list of navigable items.
func (m *GraphModel) buildItems() {
	m.items = nil

	for stackIdx, stack := range m.stacks {
		// Show bookmarks from top to bottom (reverse order)
		for segIdx := len(stack.Segments) - 1; segIdx >= 0; segIdx-- {
			seg := stack.Segments[segIdx]
			item := graphItem{
				bookmark:    seg.Bookmark.Name,
				stack:       stackIdx,
				segment:     segIdx,
				isExpanded:  m.expanded[seg.Bookmark.Name],
				isSynced:    seg.Bookmark.IsSynced,
				changeCount: len(seg.Changes),
			}
			m.items = append(m.items, item)
		}
	}
}

// Init implements tea.Model.
func (m GraphModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m GraphModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		footerHeight := 3
		verticalMargin := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargin)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargin
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.ensureVisible()
			}
		case "enter":
			if len(m.items) > 0 && m.cursor < len(m.items) {
				m.selectedBookmark = m.items[m.cursor].bookmark
				return m, func() tea.Msg {
					return GraphSelectMsg{Bookmark: m.selectedBookmark}
				}
			}
		case " ":
			// Toggle expand/collapse
			if len(m.items) > 0 && m.cursor < len(m.items) {
				bookmark := m.items[m.cursor].bookmark
				m.expanded[bookmark] = !m.expanded[bookmark]
				m.buildItems()
			}
		case "?":
			if m.mode == ModeHelp {
				m.mode = ModeNormal
			} else {
				m.mode = ModeHelp
			}
		case "r":
			if m.refreshFn != nil {
				m.statusMessage = "Refreshing..."
				return m, m.doRefresh()
			}
		case "q", "esc", "ctrl+c":
			if m.mode == ModeHelp {
				m.mode = ModeNormal
			} else {
				return m, tea.Quit
			}
		}

	case GraphRefreshMsg:
		if msg.Error != nil {
			m.errorMessage = msg.Error.Error()
		} else {
			m.graph = msg.Graph
			m.stacks = msg.Graph.Stacks
			m.buildItems()
			m.statusMessage = ""
			m.errorMessage = ""
		}
	}

	// Update viewport
	m.viewport.SetContent(m.renderContent())
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// ensureVisible scrolls to keep cursor visible.
func (m *GraphModel) ensureVisible() {
	// Simple implementation - just rebuild content
	// More sophisticated would calculate line position
}

// doRefresh returns a command that refreshes the graph data.
func (m *GraphModel) doRefresh() tea.Cmd {
	return func() tea.Msg {
		graph, err := m.refreshFn()
		return GraphRefreshMsg{Graph: graph, Error: err}
	}
}

// View implements tea.Model.
func (m GraphModel) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.mode == ModeHelp {
		return m.renderHelp()
	}

	var sb strings.Builder

	// Header
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Content in viewport
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")

	// Footer
	sb.WriteString(m.renderFooter())

	return sb.String()
}

func (m GraphModel) renderHeader() string {
	title := TitleStyle.Render("Bookmark Stacks")

	if m.graph != nil && m.graph.ExcludedCount > 0 {
		title += MutedStyle.Render(fmt.Sprintf(" (%d excluded)", m.graph.ExcludedCount))
	}

	return BoxStyle.Width(m.width - 2).Render(title)
}

func (m GraphModel) renderContent() string {
	if m.graph == nil || len(m.stacks) == 0 {
		return MutedStyle.Render("\n  No bookmark stacks found.\n\n  Create bookmarks with: jj bookmark create <name>\n")
	}

	var sb strings.Builder

	itemIdx := 0
	for stackIdx, stack := range m.stacks {
		if stackIdx > 0 {
			sb.WriteString("\n")
		}

		// Render stack from top to bottom
		for segIdx := len(stack.Segments) - 1; segIdx >= 0; segIdx-- {
			seg := stack.Segments[segIdx]

			// Determine indentation based on position
			indent := strings.Repeat("  ", len(stack.Segments)-1-segIdx)
			connector := ""
			if segIdx < len(stack.Segments)-1 {
				connector = "└─ "
			}

			// Cursor and selection
			cursor := "  "
			style := lipgloss.NewStyle()
			if itemIdx == m.cursor {
				cursor = CursorStyle.Render("> ")
				style = HighlightStyle
			}

			// Sync indicator
			syncIndicator := UnsyncedIndicator
			if seg.Bookmark.IsSynced {
				syncIndicator = SyncedIndicator
			}

			// Change count
			changeInfo := ""
			if len(seg.Changes) > 0 {
				changeInfo = MutedStyle.Render(fmt.Sprintf(" (%d changes)", len(seg.Changes)))
			}

			line := fmt.Sprintf("%s%s%s%s %s%s",
				cursor,
				indent,
				connector,
				style.Render(seg.Bookmark.Name),
				syncIndicator,
				changeInfo,
			)

			sb.WriteString(line)
			sb.WriteString("\n")

			// Show changes if expanded
			if m.expanded[seg.Bookmark.Name] && len(seg.Changes) > 0 {
				for _, change := range seg.Changes {
					changeIndent := strings.Repeat("  ", len(stack.Segments)-segIdx+1)
					desc := change.DescriptionFirstLine
					if desc == "" {
						desc = "(no description)"
					}
					if len(desc) > 50 {
						desc = desc[:47] + "..."
					}
					sb.WriteString(fmt.Sprintf("   %s%s %s\n",
						changeIndent,
						MutedStyle.Render("•"),
						DimStyle.Render(desc),
					))
				}
			}

			itemIdx++
		}

		// Show base
		baseIndent := strings.Repeat("  ", len(stack.Segments))
		sb.WriteString(fmt.Sprintf("   %s└─ %s\n", baseIndent, MutedStyle.Render("main")))
	}

	return sb.String()
}

func (m GraphModel) renderFooter() string {
	if m.errorMessage != "" {
		return ErrorStyle.Render("Error: " + m.errorMessage)
	}

	if m.statusMessage != "" {
		return MutedStyle.Render(m.statusMessage)
	}

	help := []string{
		RenderKeyHelp("↑↓", "navigate"),
		RenderKeyHelp("Enter", "submit"),
		RenderKeyHelp("Space", "expand"),
		RenderKeyHelp("r", "refresh"),
		RenderKeyHelp("?", "help"),
		RenderKeyHelp("q", "quit"),
	}

	legend := SyncedIndicator + " synced  " + UnsyncedIndicator + " needs push"

	return MutedStyle.Render(legend) + "\n" + strings.Join(help, "  ")
}

func (m GraphModel) renderHelp() string {
	help := `
` + TitleStyle.Render("Keyboard Shortcuts") + `

  Navigation
    ↑/k        Move up
    ↓/j        Move down
    Space      Expand/collapse bookmark details

  Actions
    Enter      Select bookmark for submission
    r          Refresh graph from repository

  Other
    ?          Toggle this help
    q/Esc      Quit

` + MutedStyle.Render("Press ? or Esc to close help")

	return BoxStyle.Width(m.width - 4).Render(help)
}

// SelectedBookmark returns the currently selected bookmark name.
func (m GraphModel) SelectedBookmark() string {
	return m.selectedBookmark
}

// SetError sets an error message to display.
func (m *GraphModel) SetError(err string) {
	m.errorMessage = err
}

// SetStatus sets a status message to display.
func (m *GraphModel) SetStatus(status string) {
	m.statusMessage = status
}

// RunGraphView runs the interactive graph view and returns the selected bookmark.
// Returns empty string if user quits without selecting.
func RunGraphView(graph *jjutils.ChangeGraph) (string, error) {
	return RunGraphViewWithRefresh(graph, nil)
}

// RunGraphViewWithRefresh runs the interactive graph view with refresh support.
func RunGraphViewWithRefresh(graph *jjutils.ChangeGraph, refreshFn RefreshFunc) (string, error) {
	m := NewGraphModel(graph)
	m.refreshFn = refreshFn
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if gm, ok := finalModel.(GraphModel); ok {
		return gm.SelectedBookmark(), nil
	}

	return "", nil
}
