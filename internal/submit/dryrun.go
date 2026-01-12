package submit

import (
	"fmt"
	"strings"
)

// AIDEV-NOTE: Dry run mode shows what would happen without making changes.
// Output is designed to be clear and informative.

// FormatDryRunOutput formats the submission plan as dry run output.
func FormatDryRunOutput(analysis *AnalysisResult, plan *SubmissionPlan) string {
	var sb strings.Builder

	// Header
	sb.WriteString("Dry Run - No changes will be made\n")
	sb.WriteString(strings.Repeat("─", 40))
	sb.WriteString("\n\n")

	// Stack visualization
	sb.WriteString("Stack: ")
	sb.WriteString(GetStackSummary(analysis))
	sb.WriteString("\n\n")

	// Actions list
	if len(plan.Actions) == 0 {
		sb.WriteString("No actions required - everything is up to date.\n")
	} else {
		sb.WriteString(fmt.Sprintf("Actions (%d):\n", len(plan.Actions)))

		for i, action := range plan.Actions {
			sb.WriteString(fmt.Sprintf("  %d. ", i+1))
			sb.WriteString(formatAction(action))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")

	// Summary
	sb.WriteString("Summary:\n")
	if plan.Summary.BookmarksToPush > 0 {
		sb.WriteString(fmt.Sprintf("  • %d bookmark(s) to push\n", plan.Summary.BookmarksToPush))
	}
	if plan.Summary.PRsToCreate > 0 {
		sb.WriteString(fmt.Sprintf("  • %d PR(s) to create\n", plan.Summary.PRsToCreate))
	}
	if plan.Summary.PRsToUpdate > 0 {
		sb.WriteString(fmt.Sprintf("  • %d PR(s) to update\n", plan.Summary.PRsToUpdate))
	}
	if plan.Summary.CommentsToSync > 0 {
		sb.WriteString(fmt.Sprintf("  • %d comment(s) to sync\n", plan.Summary.CommentsToSync))
	}

	if plan.Summary.BookmarksToPush == 0 && plan.Summary.PRsToCreate == 0 &&
		plan.Summary.PRsToUpdate == 0 && plan.Summary.CommentsToSync == 0 {
		sb.WriteString("  • Nothing to do\n")
	}

	return sb.String()
}

// formatAction formats a single action for display.
func formatAction(action SubmissionAction) string {
	switch a := action.(type) {
	case *PushAction:
		return fmt.Sprintf("[PUSH] Push bookmark '%s' to %s", a.Bookmark, a.Remote)

	case *CreatePRAction:
		var parts []string
		parts = append(parts, fmt.Sprintf("[CREATE PR] Create PR for '%s'", a.Bookmark))
		parts = append(parts, fmt.Sprintf("     Title: %q", truncate(a.Title, 50)))
		parts = append(parts, fmt.Sprintf("     Base: %s", a.BaseBranch))
		if a.Draft {
			parts = append(parts, "     Draft: yes")
		}
		return strings.Join(parts, "\n")

	case *UpdateBaseAction:
		return fmt.Sprintf("[UPDATE BASE] Update PR #%d: %s → %s",
			a.PRNumber, a.OldBase, a.NewBase)

	case *SyncCommentAction:
		return fmt.Sprintf("[SYNC COMMENT] Update stack comment on PR #%d ('%s')",
			a.PRNumber, a.Bookmark)

	default:
		return action.Description()
	}
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// FormatExecutionResult formats the execution result as user-friendly output.
func FormatExecutionResult(result *ExecutionResult) string {
	var sb strings.Builder

	sb.WriteString("Execution Complete\n")
	sb.WriteString(strings.Repeat("─", 40))
	sb.WriteString("\n\n")

	// Results by action
	if len(result.Executed) > 0 {
		sb.WriteString("Results:\n")
		for i, ar := range result.Executed {
			status := "✓"
			if !ar.Success {
				status = "✗"
			}
			sb.WriteString(fmt.Sprintf("  %d. %s %s\n", i+1, status, ar.Action.Description()))

			// Show details for successful PR creations
			if ar.Success && ar.Action.Type() == ActionCreatePR {
				if url, ok := ar.Details["pr_url"].(string); ok {
					sb.WriteString(fmt.Sprintf("     → %s\n", url))
				}
			}

			// Show error for failures
			if !ar.Success && ar.Error != nil {
				sb.WriteString(fmt.Sprintf("     Error: %s\n", ar.Error.Error()))
			}
		}
		sb.WriteString("\n")
	}

	// Summary
	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("  • Succeeded: %d\n", result.Summary.Succeeded))
	if result.Summary.Failed > 0 {
		sb.WriteString(fmt.Sprintf("  • Failed: %d\n", result.Summary.Failed))
	}
	if result.Summary.Skipped > 0 {
		sb.WriteString(fmt.Sprintf("  • Skipped: %d\n", result.Summary.Skipped))
	}

	// PR URLs
	urls := GetCreatedPRURLs(result)
	if len(urls) > 0 {
		sb.WriteString("\nCreated PRs:\n")
		for _, url := range urls {
			sb.WriteString(fmt.Sprintf("  • %s\n", url))
		}
	}

	return sb.String()
}

// FormatAnalysisErrors formats analysis errors for display.
func FormatAnalysisErrors(result *AnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("Analysis Errors\n")
	sb.WriteString(strings.Repeat("─", 40))
	sb.WriteString("\n\n")

	for _, err := range result.Errors {
		sb.WriteString(fmt.Sprintf("  • %s\n", err.Error()))
	}

	if len(result.Warnings) > 0 {
		sb.WriteString("\nWarnings:\n")
		for _, warn := range result.Warnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", warn))
		}
	}

	return sb.String()
}

// FormatWarnings formats warnings for display.
func FormatWarnings(result *AnalysisResult) string {
	if len(result.Warnings) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Warnings:\n")
	for _, warn := range result.Warnings {
		sb.WriteString(fmt.Sprintf("  ⚠ %s\n", warn))
	}
	return sb.String()
}
