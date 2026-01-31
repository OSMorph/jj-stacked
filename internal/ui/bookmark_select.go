package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: BookmarkSelectModel provides selection when multiple bookmarks point to the same change.
// This is needed when a user needs to choose which bookmark to use for a PR.

// BookmarkSelectModel is a Bubble Tea model for selecting a bookmark.
type BookmarkSelectModel struct {
	bookmarks []jjutils.Bookmark
	changeID  string
	cursor    int
	selected  *jjutils.Bookmark
	done      bool
	cancelled bool
}

// NewBookmarkSelect creates a new bookmark selection model.
func NewBookmarkSelect(bookmarks []jjutils.Bookmark, changeID string) BookmarkSelectModel {
	return BookmarkSelectModel{
		bookmarks: bookmarks,
		changeID:  changeID,
		cursor:    0,
	}
}

// Init implements tea.Model.
func (m BookmarkSelectModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m BookmarkSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.bookmarks)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.bookmarks) > 0 {
				m.selected = &m.bookmarks[m.cursor]
				m.done = true
				return m, tea.Quit
			}
		case "q", "esc", "ctrl+c":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m BookmarkSelectModel) View() string {
	if m.done {
		return ""
	}

	// Show shortened change ID
	shortID := m.changeID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	s := TitleStyle.Render(fmt.Sprintf("Multiple bookmarks point to change %s:", shortID)) + "\n\n"

	for i, bookmark := range m.bookmarks {
		cursor := "  "
		style := ListItemStyle

		if i == m.cursor {
			cursor = CursorStyle.Render("> ")
			style = SelectedListItemStyle
		}

		// Show sync status
		syncStatus := UnsyncedIndicator
		if bookmark.IsSynced {
			syncStatus = SyncedIndicator
		}

		// Format: name (sync status) [remote info]
		line := fmt.Sprintf("%s%s %s",
			cursor,
			style.Render(bookmark.Name),
			syncStatus,
		)

		// Add remote info if available
		if bookmark.HasRemote {
			if bookmark.IsSynced {
				line += MutedStyle.Render(" (synced)")
			} else {
				line += WarningStyle.Render(" (needs push)")
			}
		} else {
			line += MutedStyle.Render(" (local only)")
		}

		s += line + "\n"
	}

	s += "\n" + HelpKeyStyle.Render("[↑↓]") + " " + HelpDescStyle.Render("navigate") + "  " +
		HelpKeyStyle.Render("[Enter]") + " " + HelpDescStyle.Render("select") + "  " +
		HelpKeyStyle.Render("[Esc]") + " " + HelpDescStyle.Render("cancel")

	return s
}

// Selected returns the selected bookmark, or nil if cancelled.
func (m BookmarkSelectModel) Selected() *jjutils.Bookmark {
	return m.selected
}

// IsCancelled returns true if the user cancelled selection.
func (m BookmarkSelectModel) IsCancelled() bool {
	return m.cancelled
}

// IsDone returns true if selection is complete.
func (m BookmarkSelectModel) IsDone() bool {
	return m.done
}

// RunBookmarkSelect runs the bookmark selection UI and returns the selected bookmark.
// Returns nil if cancelled. Auto-selects if only one bookmark.
func RunBookmarkSelect(bookmarks []jjutils.Bookmark, changeID string) (*jjutils.Bookmark, error) {
	if len(bookmarks) == 0 {
		return nil, nil
	}

	// Auto-select if only one bookmark
	if len(bookmarks) == 1 {
		return &bookmarks[0], nil
	}

	m := NewBookmarkSelect(bookmarks, changeID)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	if selectModel, ok := finalModel.(BookmarkSelectModel); ok {
		return selectModel.Selected(), nil
	}

	return nil, nil
}
