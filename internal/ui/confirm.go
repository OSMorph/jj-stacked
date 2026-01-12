package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// AIDEV-NOTE: ConfirmModel provides a yes/no confirmation dialog.
// Used for destructive or important operations.

// ConfirmModel is a Bubble Tea model for yes/no confirmation.
type ConfirmModel struct {
	message   string
	confirmed bool
	done      bool
	cursor    int // 0 = No, 1 = Yes
	quitting  bool
}

// NewConfirm creates a new confirmation dialog.
func NewConfirm(message string) ConfirmModel {
	return ConfirmModel{
		message: message,
		cursor:  1, // Default to Yes
	}
}

// NewConfirmDefaultNo creates a new confirmation dialog defaulting to No.
func NewConfirmDefaultNo(message string) ConfirmModel {
	return ConfirmModel{
		message: message,
		cursor:  0, // Default to No
	}
}

// Init implements tea.Model.
func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			m.cursor = 0
		case "right", "l":
			m.cursor = 1
		case "tab":
			m.cursor = (m.cursor + 1) % 2
		case "y", "Y":
			m.confirmed = true
			m.done = true
			return m, tea.Quit
		case "n", "N":
			m.confirmed = false
			m.done = true
			return m, tea.Quit
		case "enter":
			m.confirmed = m.cursor == 1
			m.done = true
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.confirmed = false
			m.done = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m ConfirmModel) View() string {
	if m.done {
		return ""
	}

	noStyle := MutedStyle
	yesStyle := MutedStyle

	if m.cursor == 0 {
		noStyle = SelectedStyle
	} else {
		yesStyle = SelectedStyle
	}

	return fmt.Sprintf(
		"%s\n\n  %s  %s\n\n%s",
		m.message,
		noStyle.Render(" No "),
		yesStyle.Render(" Yes "),
		HelpKeyStyle.Render("[←→]")+" "+HelpDescStyle.Render("select")+"  "+
			HelpKeyStyle.Render("[Enter]")+" "+HelpDescStyle.Render("confirm")+"  "+
			HelpKeyStyle.Render("[y/n]")+" "+HelpDescStyle.Render("quick select"),
	)
}

// Confirmed returns true if the user confirmed.
func (m ConfirmModel) Confirmed() bool {
	return m.confirmed
}

// IsQuitting returns true if the user cancelled.
func (m ConfirmModel) IsQuitting() bool {
	return m.quitting
}

// IsDone returns true if the dialog is complete.
func (m ConfirmModel) IsDone() bool {
	return m.done
}

// RunConfirm runs a confirmation dialog and returns the result.
// Returns true if confirmed, false if declined or cancelled.
func RunConfirm(message string) (bool, error) {
	m := NewConfirm(message)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}

	if confirmModel, ok := finalModel.(ConfirmModel); ok {
		return confirmModel.Confirmed(), nil
	}

	return false, nil
}

// RunConfirmDefaultNo runs a confirmation dialog defaulting to No.
func RunConfirmDefaultNo(message string) (bool, error) {
	m := NewConfirmDefaultNo(message)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}

	if confirmModel, ok := finalModel.(ConfirmModel); ok {
		return confirmModel.Confirmed(), nil
	}

	return false, nil
}
