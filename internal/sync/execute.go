package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// ExecuteSync executes a plan without durable recovery state. Commands should
// use ExecuteSyncWithState; this wrapper is kept for library callers and tests.
func ExecuteSync(
	ctx context.Context,
	plan *SyncPlan,
	jj jjutils.JJFunctions,
	callbacks *SyncCallbacks,
) *SyncResult {
	state := CreateInitialState(plan, "", "", true)
	return ExecuteSyncWithState(ctx, plan, state, jj, callbacks)
}

// ExecuteSyncWithState executes only incomplete durable steps in state.
func ExecuteSyncWithState(ctx context.Context, plan *SyncPlan, state *SyncState, jj jjutils.JJFunctions, callbacks *SyncCallbacks) *SyncResult {
	result := &SyncResult{Success: true}
	checkpoint := func(phase string) bool {
		state.SetPhase(phase)
		if callbacks != nil && callbacks.OnCheckpoint != nil {
			if err := callbacks.OnCheckpoint(state); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("save sync recovery state: %w", err))
				result.Success = false
				return false
			}
		}
		return true
	}
	checkConflicts := func() bool {
		hasConflicts, err := jj.HasConflicts(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("check conflicts: %w", err))
			result.Success = false
			return true
		}
		if !hasConflicts {
			return false
		}
		result.HasConflicts = true
		result.Success = false
		files, err := jj.GetConflictFiles(ctx)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not list conflict files: %v", err))
		}
		result.ConflictFiles = files
		state.SetConflictFiles(files)
		checkpoint("conflict")
		return true
	}

	for _, bookmark := range plan.ToDelete {
		step := "delete:" + bookmark
		if state.StepComplete(step) {
			continue
		}
		existing := getExistingBookmarks(ctx, jj)
		if !existing[bookmark] {
			state.MarkStepComplete(step)
			if !checkpoint("delete") {
				return result
			}
			continue
		}
		if callbacks != nil && callbacks.OnDelete != nil {
			callbacks.OnDelete(bookmark)
		}
		err := jj.DeleteBookmark(ctx, bookmark)
		if callbacks != nil && callbacks.OnDeleteComplete != nil {
			callbacks.OnDeleteComplete(bookmark, err)
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("delete merged bookmark %s: %w", bookmark, err))
			result.Success = false
			checkpoint("delete-failed")
			return result
		}
		result.Deleted = append(result.Deleted, bookmark)
		state.MarkStepComplete(step)
		if !checkpoint("delete") {
			return result
		}
	}

	for _, bookmark := range plan.ToAbandon {
		step := "abandon:" + bookmark
		if state.StepComplete(step) {
			continue
		}
		existing := getExistingBookmarks(ctx, jj)
		if !existing[bookmark] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("bookmark %s no longer exists; skipping abandon", bookmark))
			state.MarkStepComplete(step)
			if !checkpoint("abandon") {
				return result
			}
			continue
		}
		if callbacks != nil && callbacks.OnAbandon != nil {
			callbacks.OnAbandon(bookmark)
		}
		err := jj.Abandon(ctx, bookmark)
		if callbacks != nil && callbacks.OnAbandonComplete != nil {
			callbacks.OnAbandonComplete(bookmark, err)
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("abandon %s: %w", bookmark, err))
			result.Success = false
			checkpoint("abandon-failed")
			return result
		}
		result.Abandoned = append(result.Abandoned, bookmark)
		state.MarkStepComplete(step)
		if !checkpoint("abandon") || checkConflicts() {
			return result
		}
	}

	if callbacks != nil && callbacks.OnRebaseStart != nil && len(plan.RebaseRoots) > 0 {
		callbacks.OnRebaseStart(plan.RebaseRoots)
	}
	var rebaseErr error
	for _, root := range plan.RebaseRoots {
		step := "rebase:" + root
		if state.StepComplete(step) {
			continue
		}
		existing := getExistingBookmarks(ctx, jj)
		if !existing[root] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("stack root %s no longer exists; skipping rebase", root))
			state.MarkStepComplete(step)
			if !checkpoint("rebase") {
				return result
			}
			continue
		}
		based, err := jj.IsAncestor(ctx, plan.RebaseTarget, root)
		if err != nil {
			rebaseErr = fmt.Errorf("check rebase target for %s: %w", root, err)
		} else if !based {
			rebaseErr = jj.Rebase(ctx, root, plan.RebaseTarget)
		}
		if rebaseErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("rebase %s onto %s failed: %w", root, plan.RebaseTarget, rebaseErr))
			result.Success = false
			checkpoint("rebase-failed")
			break
		}
		state.MarkStepComplete(step)
		if !checkpoint("rebase") || checkConflicts() {
			return result
		}
	}
	if callbacks != nil && callbacks.OnRebaseComplete != nil {
		callbacks.OnRebaseComplete(rebaseErr)
	}
	if rebaseErr != nil {
		return result
	}
	if len(plan.RebaseRoots) > 0 {
		result.Rebased = append(result.Rebased, plan.ToRebase...)
	}

	existing := getExistingBookmarks(ctx, jj)
	for _, bookmark := range plan.ToPush {
		step := "push:" + bookmark
		if state.StepComplete(step) {
			continue
		}
		if !existing[bookmark] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipping push for %s: bookmark no longer exists", bookmark))
			state.MarkStepComplete(step)
			if !checkpoint("push") {
				return result
			}
			continue
		}
		if callbacks != nil && callbacks.OnPushStart != nil {
			callbacks.OnPushStart(bookmark)
		}
		err := jj.Push(ctx, plan.Remote, bookmark)
		if callbacks != nil && callbacks.OnPushComplete != nil {
			callbacks.OnPushComplete(bookmark, err)
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("push %s failed: %w", bookmark, err))
			result.Success = false
			checkpoint("push-failed")
			continue
		}
		result.Pushed = append(result.Pushed, bookmark)
		state.MarkStepComplete(step)
		if !checkpoint("push") {
			return result
		}
	}
	if result.Success {
		checkpoint("local-complete")
	}
	return result
}

// getExistingBookmarks returns a set of bookmark names that currently exist.
func getExistingBookmarks(ctx context.Context, jj jjutils.JJFunctions) map[string]bool {
	// Use all local bookmarks, not just "user" bookmarks. Some commands (notably push)
	// require the bookmark to exist regardless of whether it points at trunk() or not.
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		// On error, return empty map (will cause all bookmarks to be filtered out)
		return make(map[string]bool)
	}

	existing := make(map[string]bool)
	for _, bm := range bookmarks {
		existing[bm.Name] = true
	}
	return existing
}

// FormatResult returns a human-readable summary of the sync result.
func FormatResult(result *SyncResult) string {
	var sb strings.Builder

	switch {
	case result.Success:
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
		if len(result.Deleted) > 0 {
			parts = append(parts, fmt.Sprintf("deleted %d merged bookmark%s",
				len(result.Deleted), pluralize(len(result.Deleted))))
		}
		if len(parts) == 0 {
			sb.WriteString("Sync complete - nothing to do")
		} else {
			sb.WriteString(fmt.Sprintf("Sync complete: %s", joinParts(parts)))
		}
	case result.HasConflicts:
		sb.WriteString("Sync paused: rebase resulted in conflicts. Resolve conflicts and run 'jj-stacked sync --continue'")
	case len(result.Errors) > 0:
		sb.WriteString(fmt.Sprintf("Sync failed: %v", result.Errors[0]))
	default:
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
