package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/OSMorph/jj-stacked/internal/submit"
)

// AIDEV-NOTE: SubmitProgressModel shows real-time progress during submission execution.
// It displays each action with its status and provides a progress bar.

// SubmitProgressModel is a Bubble Tea model for showing submission progress.
type SubmitProgressModel struct {
	actions     []submit.SubmissionAction
	results     []submit.ActionResult
	current     int
	done        bool
	spinner     spinner.Model
	finalResult *submit.ExecutionResult
	plan        *submit.SubmissionPlan
	stackName   string
	quitting    bool
}

// ActionUpdateMsg is sent when an action's status changes.
type ActionUpdateMsg struct {
	Index  int
	Result submit.ActionResult
}

// ActionStartMsg is sent when an action starts.
type ActionStartMsg struct {
	Index int
}

// SubmitDoneMsg is sent when all actions are complete.
type SubmitDoneMsg struct {
	Result *submit.ExecutionResult
}

// NewSubmitProgress creates a new submit progress model.
func NewSubmitProgress(plan *submit.SubmissionPlan, stackName string) SubmitProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return SubmitProgressModel{
		actions:   plan.Actions,
		results:   make([]submit.ActionResult, len(plan.Actions)),
		current:   -1,
		spinner:   s,
		plan:      plan,
		stackName: stackName,
	}
}

// Init implements tea.Model.
func (m SubmitProgressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update implements tea.Model.
func (m SubmitProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case ActionStartMsg:
		m.current = msg.Index
		return m, nil

	case ActionUpdateMsg:
		if msg.Index >= 0 && msg.Index < len(m.results) {
			m.results[msg.Index] = msg.Result
		}
		return m, nil

	case SubmitDoneMsg:
		m.done = true
		m.finalResult = msg.Result
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model.
func (m SubmitProgressModel) View() string {
	if m.quitting {
		return ""
	}

	var sb strings.Builder

	// Header
	sb.WriteString(TitleStyle.Render("Submitting stack: "))
	sb.WriteString(m.stackName)
	sb.WriteString("\n\n")

	// Action list
	for i, action := range m.actions {
		var icon string
		var style lipgloss.Style

		switch {
		case i < len(m.results) && m.results[i].Action != nil && m.results[i].Success:
			// Action succeeded
			icon = SuccessIndicator
			style = SuccessStyle
		case i < len(m.results) && m.results[i].Action != nil:
			// Action failed
			icon = ErrorIndicator
			style = ErrorStyle
		case i == m.current:
			// Currently executing
			icon = m.spinner.View()
			style = HighlightStyle
		default:
			// Pending
			icon = PendingIndicator
			style = MutedStyle
		}

		sb.WriteString(fmt.Sprintf("  %s %s\n", icon, style.Render(action.Description())))

		// Show details for completed actions
		if i < len(m.results) && m.results[i].Action != nil {
			if m.results[i].Success {
				// Show PR URL for create actions
				if action.Type() == submit.ActionCreatePR {
					if url, ok := m.results[i].Details["pr_url"].(string); ok {
						sb.WriteString(fmt.Sprintf("    %s %s\n", CurrentIndicator, MutedStyle.Render(url)))
					}
				}
				// Show reason for closed PRs
				if action.Type() == submit.ActionClosePR {
					if reason, ok := m.results[i].Details["reason"].(string); ok {
						sb.WriteString(fmt.Sprintf("    %s %s\n", CurrentIndicator, MutedStyle.Render(reason)))
					}
				}
			} else if m.results[i].Error != nil {
				// Show error
				sb.WriteString(fmt.Sprintf("    %s\n", ErrorStyle.Render(m.results[i].Error.Error())))
			}
		}
	}

	// Progress bar
	if !m.done && len(m.actions) > 0 {
		completed := 0
		for _, r := range m.results {
			if r.Action != nil {
				completed++
			}
		}

		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  Progress: %s %d/%d\n",
			RenderProgressBar(completed, len(m.actions), 20),
			completed,
			len(m.actions),
		))
	}

	// Final summary
	if m.done && m.finalResult != nil {
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("─", 40))
		sb.WriteString("\n")

		if m.finalResult.Summary.Failed > 0 {
			sb.WriteString(ErrorStyle.Render(fmt.Sprintf("Completed with %d failure(s)\n", m.finalResult.Summary.Failed)))
		} else {
			sb.WriteString(SuccessStyle.Render("All actions completed successfully!\n"))
		}

		sb.WriteString(fmt.Sprintf("  Succeeded: %d\n", m.finalResult.Summary.Succeeded))
		if m.finalResult.Summary.Failed > 0 {
			sb.WriteString(fmt.Sprintf("  Failed: %d\n", m.finalResult.Summary.Failed))
		}
		if m.finalResult.Summary.Skipped > 0 {
			sb.WriteString(fmt.Sprintf("  Skipped: %d\n", m.finalResult.Summary.Skipped))
		}

		// Show all PR URLs (created and existing)
		urls := submit.GetAllPRURLs(m.finalResult, m.plan)
		if len(urls) > 0 {
			sb.WriteString("\nPull Requests:\n")
			for _, url := range urls {
				sb.WriteString(fmt.Sprintf("  %s %s\n", SuccessIndicator, url))
			}
		}
	}

	return sb.String()
}

// IsDone returns true if submission is complete.
func (m SubmitProgressModel) IsDone() bool {
	return m.done
}

// IsQuitting returns true if the user cancelled.
func (m SubmitProgressModel) IsQuitting() bool {
	return m.quitting
}

// FinalResult returns the final execution result.
func (m SubmitProgressModel) FinalResult() *submit.ExecutionResult {
	return m.finalResult
}

// CreateProgressCallbacks creates ExecutionCallbacks that send messages to the program.
func CreateProgressCallbacks(p *tea.Program) *submit.ExecutionCallbacks {
	currentIndex := 0

	return &submit.ExecutionCallbacks{
		OnActionStart: func(action submit.SubmissionAction) {
			p.Send(ActionStartMsg{Index: currentIndex})
		},
		OnActionComplete: func(action submit.SubmissionAction, result submit.ActionResult) {
			p.Send(ActionUpdateMsg{Index: currentIndex, Result: result})
			currentIndex++
		},
		OnProgress: func(completed, total int) {
			// Progress is tracked via action callbacks
		},
	}
}

// SendDone sends the completion message to the program.
func SendDone(p *tea.Program, result *submit.ExecutionResult) {
	p.Send(SubmitDoneMsg{Result: result})
}
