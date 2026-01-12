package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// AIDEV-NOTE: RemoteSelectModel provides interactive selection for multiple GitHub remotes.
// Supports both GitHub.com and GitHub Enterprise instances.

// RemoteOption represents a GitHub remote for selection.
type RemoteOption struct {
	Name  string // e.g., "origin"
	URL   string // Full URL
	Host  string // "github.com" or "git.mycompany.com"
	Owner string // Extracted owner
	Repo  string // Extracted repo name
}

// RemoteSelectModel is a Bubble Tea model for selecting a remote.
type RemoteSelectModel struct {
	remotes   []RemoteOption
	cursor    int
	selected  *RemoteOption
	done      bool
	cancelled bool
}

// NewRemoteSelect creates a new remote selection model.
func NewRemoteSelect(remotes []RemoteOption) RemoteSelectModel {
	return RemoteSelectModel{
		remotes: remotes,
		cursor:  0,
	}
}

// Init implements tea.Model.
func (m RemoteSelectModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m RemoteSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.remotes)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.remotes) > 0 {
				m.selected = &m.remotes[m.cursor]
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
func (m RemoteSelectModel) View() string {
	if m.done {
		return ""
	}

	s := TitleStyle.Render("Multiple GitHub remotes found. Select one:") + "\n\n"

	for i, remote := range m.remotes {
		cursor := "  "
		style := ListItemStyle

		if i == m.cursor {
			cursor = CursorStyle.Render("> ")
			style = SelectedListItemStyle
		}

		// Format: name [host] owner/repo
		var hostLabel string
		if remote.Host != "github.com" {
			hostLabel = WarningStyle.Render(remote.Host) // Highlight GHE hosts
		} else {
			hostLabel = MutedStyle.Render(remote.Host)
		}

		line := fmt.Sprintf("%s%s [%s] %s/%s",
			cursor,
			style.Render(remote.Name),
			hostLabel,
			MutedStyle.Render(remote.Owner),
			MutedStyle.Render(remote.Repo),
		)

		s += line + "\n"
	}

	s += "\n" + HelpKeyStyle.Render("[↑↓]") + " " + HelpDescStyle.Render("navigate") + "  " +
		HelpKeyStyle.Render("[Enter]") + " " + HelpDescStyle.Render("select") + "  " +
		HelpKeyStyle.Render("[Esc]") + " " + HelpDescStyle.Render("cancel")

	return s
}

// Selected returns the selected remote, or nil if cancelled.
func (m RemoteSelectModel) Selected() *RemoteOption {
	return m.selected
}

// IsCancelled returns true if the user cancelled selection.
func (m RemoteSelectModel) IsCancelled() bool {
	return m.cancelled
}

// IsDone returns true if selection is complete.
func (m RemoteSelectModel) IsDone() bool {
	return m.done
}

// RunRemoteSelect runs the remote selection UI and returns the selected remote.
// Returns nil if cancelled or no remotes available.
func RunRemoteSelect(remotes []RemoteOption) (*RemoteOption, error) {
	if len(remotes) == 0 {
		return nil, nil
	}

	// Auto-select if only one remote
	if len(remotes) == 1 {
		return &remotes[0], nil
	}

	m := NewRemoteSelect(remotes)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	if selectModel, ok := finalModel.(RemoteSelectModel); ok {
		return selectModel.Selected(), nil
	}

	return nil, nil
}

// FilterGitHubRemotes filters remotes to only include GitHub remotes.
// This includes both github.com and known GHE hosts.
func FilterGitHubRemotes(remotes []RemoteOption) []RemoteOption {
	var filtered []RemoteOption
	for _, r := range remotes {
		// Include all remotes that have a valid host, owner, and repo
		if r.Host != "" && r.Owner != "" && r.Repo != "" {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
