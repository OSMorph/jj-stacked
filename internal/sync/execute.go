package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: ExecuteSync is the third phase of the three-phase sync architecture.
// It performs the actual sync operations: fetch, rebase, and push.
// This phase should have no decision-making - all decisions were made in the planning phase.

// ExecuteSync performs the sync operations defined in the plan.
//
// Execution order:
// 1. Fetch from all remotes (get latest trunk state)
// 2. Abandon merged bookmarks (in order, bottom-up from trunk) - optional, may fail if already deleted
// 3. Rebase remaining bookmarks onto trunk@origin
// 4. Push bookmarks that are ahead of origin (after rebase)
//
// Parameters:
//   - ctx: context for cancellation
//   - plan: the sync plan to execute
//   - jj: jujutsu functions interface
//   - callbacks: optional progress callbacks (can be nil)
//
// Returns a SyncResult with the outcome of each operation.
func ExecuteSync(
	ctx context.Context,
	plan *SyncPlan,
	jj jjutils.JJFunctions,
	callbacks *SyncCallbacks,
) *SyncResult {
	result := &SyncResult{
		Success: true,
	}

	if plan.IsEmpty() {
		return result
	}

	// Step 1: Fetch from all remotes first to get latest trunk state
	if callbacks != nil && callbacks.OnFetchStart != nil {
		callbacks.OnFetchStart()
	}

	err := jj.FetchAllRemotes(ctx)
	if callbacks != nil && callbacks.OnFetchComplete != nil {
		callbacks.OnFetchComplete(err)
	}
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("fetch failed: %w", err))
		result.Success = false
		return result
	}

	// Step 2: Abandon merged bookmarks (best effort - may fail if bookmark was deleted)
	for _, bookmark := range plan.ToAbandon {
		if callbacks != nil && callbacks.OnAbandon != nil {
			callbacks.OnAbandon(bookmark)
		}

		// Abandon by bookmark name (jj will resolve to the change)
		// This may fail if the bookmark was already deleted when the remote branch was deleted
		err := jj.Abandon(ctx, bookmark)
		if callbacks != nil && callbacks.OnAbandonComplete != nil {
			callbacks.OnAbandonComplete(bookmark, err)
		}

		if err != nil {
			// Don't fail the sync if abandon fails - the bookmark may have been deleted
			// when the remote branch was deleted on GitHub
			result.Warnings = append(result.Warnings, fmt.Sprintf("abandon %s: %v (bookmark may have been auto-deleted)", bookmark, err))
		} else {
			result.Abandoned = append(result.Abandoned, bookmark)
		}
	}

	// Step 3: Rebase remaining bookmarks onto trunk@origin
	if plan.NeedsRebase && len(plan.ToRebase) > 0 {
		if callbacks != nil && callbacks.OnRebaseStart != nil {
			callbacks.OnRebaseStart(plan.ToRebase)
		}

		// Find the root(s) of the remaining stack(s) and rebase them onto trunk@origin
		roots := findStackRoots(plan)

		var rebaseErr error
		for _, root := range roots {
			// Use the plan's rebase target (e.g., "main@origin")
			err := jj.Rebase(ctx, root, plan.RebaseTarget)
			if err != nil {
				rebaseErr = fmt.Errorf("rebase %s onto %s failed: %w", root, plan.RebaseTarget, err)
				result.Errors = append(result.Errors, rebaseErr)
				result.Success = false
				break
			}
		}

		if callbacks != nil && callbacks.OnRebaseComplete != nil {
			callbacks.OnRebaseComplete(rebaseErr)
		}

		if rebaseErr == nil {
			result.Rebased = plan.ToRebase
		}

		// Check for conflicts after rebase
		hasConflicts, err := jj.HasConflicts(ctx)
		if err == nil && hasConflicts {
			result.HasConflicts = true
			result.Success = false
			return result
		}
	}

	// Step 4: Push bookmarks that are ahead of origin (after rebase they may have new commits)
	for _, bookmark := range plan.ToPush {
		if callbacks != nil && callbacks.OnPushStart != nil {
			callbacks.OnPushStart(bookmark)
		}

		err := jj.Push(ctx, "origin", bookmark)
		if callbacks != nil && callbacks.OnPushComplete != nil {
			callbacks.OnPushComplete(bookmark, err)
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("push %s failed: %w", bookmark, err))
			result.Success = false
			// Continue trying to push remaining bookmarks
		} else {
			result.Pushed = append(result.Pushed, bookmark)
		}
	}

	return result
}

// findStackRoots finds the root bookmarks of remaining stacks that need rebasing.
// These are bookmarks whose parent was abandoned (merged).
func findStackRoots(plan *SyncPlan) []string {
	if plan.Analysis == nil {
		// Fallback: just return all bookmarks to rebase
		return plan.ToRebase
	}

	// Build set of abandoned bookmark names
	abandonedSet := make(map[string]bool)
	for _, name := range plan.ToAbandon {
		abandonedSet[name] = true
	}

	// For now, return the first remaining bookmark as root
	// In a more sophisticated implementation, we would analyze the graph
	// to find the actual root(s)
	if len(plan.ToRebase) > 0 {
		return []string{plan.ToRebase[0]}
	}

	return nil
}

// FormatResult returns a human-readable summary of the sync result.
func FormatResult(result *SyncResult) string {
	var sb strings.Builder

	if result.Success {
		var parts []string
		if len(result.Rebased) > 0 {
			parts = append(parts, fmt.Sprintf("rebased %d bookmark%s",
				len(result.Rebased), pluralize(len(result.Rebased))))
		}
		if len(result.Pushed) > 0 {
			parts = append(parts, fmt.Sprintf("pushed %d bookmark%s",
				len(result.Pushed), pluralize(len(result.Pushed))))
		}
		if len(result.Abandoned) > 0 {
			parts = append(parts, fmt.Sprintf("abandoned %d bookmark%s",
				len(result.Abandoned), pluralize(len(result.Abandoned))))
		}
		if len(parts) == 0 {
			sb.WriteString("Sync complete - nothing to do")
		} else {
			sb.WriteString(fmt.Sprintf("Sync complete: %s", joinParts(parts)))
		}
	} else if result.HasConflicts {
		sb.WriteString("Sync paused: rebase resulted in conflicts. Resolve conflicts and run 'jj-stacked sync --continue'")
	} else if len(result.Errors) > 0 {
		sb.WriteString(fmt.Sprintf("Sync failed: %v", result.Errors[0]))
	} else {
		sb.WriteString("Sync failed")
	}

	// Add warnings if any
	if len(result.Warnings) > 0 {
		sb.WriteString("\n\nWarnings:")
		for _, w := range result.Warnings {
			sb.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}

	return sb.String()
}

// joinParts joins strings with commas and "and" for the last item.
func joinParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		result := ""
		for i, p := range parts {
			if i == len(parts)-1 {
				result += "and " + p
			} else {
				result += p + ", "
			}
		}
		return result
	}
}
