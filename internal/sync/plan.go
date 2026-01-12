package sync

import (
	"fmt"
	"strings"
	"time"
)

// AIDEV-NOTE: CreateSyncPlan is the second phase of the three-phase sync architecture.
// It takes the analysis result and creates a concrete plan of operations to execute.
// This separation allows for dry-run mode and user confirmation before execution.

// CreateSyncPlan builds a plan from the analysis.
// The plan specifies exactly what operations will be performed during execution.
func CreateSyncPlan(analysis *SyncAnalysis) (*SyncPlan, error) {
	// Validate analysis first
	if errs := ValidateAnalysis(analysis); len(errs) > 0 {
		return nil, fmt.Errorf("analysis has blocking errors: %v", errs[0])
	}

	plan := &SyncPlan{
		Analysis: analysis,
	}

	// Order bookmarks to abandon (should already be in bottom-up order from analysis)
	for _, m := range analysis.MergedBookmarks {
		plan.ToAbandon = append(plan.ToAbandon, m.Name)
	}

	// Determine if rebase is needed
	plan.NeedsRebase = len(analysis.RemainingBookmarks) > 0 && len(analysis.MergedBookmarks) > 0
	plan.RebaseTarget = analysis.TrunkBranch
	plan.ToRebase = analysis.RemainingBookmarks

	// Build summary
	plan.Summary = SyncSummary{
		MergedCount:  len(analysis.MergedBookmarks),
		AbandonCount: len(plan.ToAbandon),
		RebaseCount:  len(plan.ToRebase),
		TrunkBranch:  analysis.TrunkBranch,
	}

	return plan, nil
}

// FormatPlan returns a human-readable description of the sync plan.
func FormatPlan(plan *SyncPlan) string {
	var sb strings.Builder

	sb.WriteString("Sync Plan:\n")
	sb.WriteString(fmt.Sprintf("  Trunk: %s\n\n", plan.Summary.TrunkBranch))

	if plan.IsEmpty() {
		sb.WriteString("  Nothing to sync - all bookmarks are up to date.\n")
		return sb.String()
	}

	// Show merged bookmarks to abandon
	if len(plan.ToAbandon) > 0 {
		sb.WriteString(fmt.Sprintf("  Abandon (%d merged bookmark%s):\n",
			len(plan.ToAbandon), pluralize(len(plan.ToAbandon))))

		for i, name := range plan.ToAbandon {
			// Find the corresponding merged bookmark for details
			for _, m := range plan.Analysis.MergedBookmarks {
				if m.Name == name {
					sb.WriteString(fmt.Sprintf("    %d. %s (PR #%d", i+1, name, m.PRNumber))
					if !m.MergedAt.IsZero() {
						sb.WriteString(fmt.Sprintf(", merged %s", formatTimeAgo(m.MergedAt)))
					}
					sb.WriteString(")\n")
					break
				}
			}
		}
		sb.WriteString("\n")
	}

	// Show bookmarks to rebase
	if plan.NeedsRebase && len(plan.ToRebase) > 0 {
		sb.WriteString(fmt.Sprintf("  Rebase onto %s:\n", plan.RebaseTarget))
		for _, name := range plan.ToRebase {
			sb.WriteString(fmt.Sprintf("    - %s\n", name))
		}
	}

	return sb.String()
}

// FormatPlanCompact returns a one-line summary of the plan.
func FormatPlanCompact(plan *SyncPlan) string {
	if plan.IsEmpty() {
		return "Nothing to sync"
	}

	var parts []string

	if plan.Summary.AbandonCount > 0 {
		parts = append(parts, fmt.Sprintf("abandon %d", plan.Summary.AbandonCount))
	}

	if plan.Summary.RebaseCount > 0 {
		parts = append(parts, fmt.Sprintf("rebase %d onto %s", plan.Summary.RebaseCount, plan.Summary.TrunkBranch))
	}

	return strings.Join(parts, ", ")
}

// pluralize returns "s" if count != 1
func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// formatTimeAgo formats a time as a human-readable "X ago" string.
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		return fmt.Sprintf("%d minute%s ago", mins, pluralize(mins))
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		return fmt.Sprintf("%d hour%s ago", hours, pluralize(hours))
	default:
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%d day%s ago", days, pluralize(days))
	}
}
