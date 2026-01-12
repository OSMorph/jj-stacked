package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AIDEV-NOTE: SpinnerModel provides an animated spinner for long-running operations.
// It can run a background task and show completion state.

// SpinnerModel is a Bubble Tea model for showing a progress spinner.
type SpinnerModel struct {
	spinner  spinner.Model
	message  string
	done     bool
	result   string
	err      error
	quitting bool
}

// SpinnerDoneMsg is sent when the spinner's task is complete.
type SpinnerDoneMsg struct {
	Result string
	Err    error
}

// NewSpinner creates a new spinner with a message.
func NewSpinner(message string) SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return SpinnerModel{
		spinner: s,
		message: message,
	}
}

// NewSpinnerWithStyle creates a new spinner with a custom style.
func NewSpinnerWithStyle(message string, style spinner.Spinner) SpinnerModel {
	s := spinner.New()
	s.Spinner = style
	s.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return SpinnerModel{
		spinner: s,
		message: message,
	}
}

// Init implements tea.Model.
func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update implements tea.Model.
func (m SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case SpinnerDoneMsg:
		m.done = true
		m.result = msg.Result
		m.err = msg.Err
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model.
func (m SpinnerModel) View() string {
	if m.quitting {
		return ""
	}

	if m.done {
		if m.err != nil {
			return ErrorIndicator + " " + m.message + ": " + ErrorStyle.Render(m.err.Error()) + "\n"
		}
		if m.result != "" {
			return SuccessIndicator + " " + m.result + "\n"
		}
		return SuccessIndicator + " " + m.message + "\n"
	}

	return m.spinner.View() + " " + m.message + "\n"
}

// IsQuitting returns true if the user cancelled the spinner.
func (m SpinnerModel) IsQuitting() bool {
	return m.quitting
}

// Error returns any error that occurred.
func (m SpinnerModel) Error() error {
	return m.err
}

// Result returns the result message.
func (m SpinnerModel) Result() string {
	return m.result
}

// SetMessage updates the spinner message.
func (m *SpinnerModel) SetMessage(message string) {
	m.message = message
}

// RunWithSpinner runs a function while showing a spinner.
// This is a convenience function for simple use cases.
func RunWithSpinner(message string, fn func() (string, error)) error {
	m := NewSpinner(message)

	p := tea.NewProgram(m)

	// Run the task in a goroutine
	go func() {
		result, err := fn()
		p.Send(SpinnerDoneMsg{Result: result, Err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	if spinnerModel, ok := finalModel.(SpinnerModel); ok {
		if spinnerModel.IsQuitting() {
			return nil // User cancelled
		}
		return spinnerModel.Error()
	}

	return nil
}

// WithTask returns a tea.Cmd that runs the given function.
func WithTask(fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		result, err := fn()
		return SpinnerDoneMsg{Result: result, Err: err}
	}
}

// Common spinner styles
var (
	SpinnerDot     = spinner.Dot
	SpinnerLine    = spinner.Line
	SpinnerMiniDot = spinner.MiniDot
	SpinnerJump    = spinner.Jump
	SpinnerPulse   = spinner.Pulse
	SpinnerPoints  = spinner.Points
	SpinnerGlobe   = spinner.Globe
	SpinnerMoon    = spinner.Moon
	SpinnerMonkey  = spinner.Monkey
)
