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
		canFinish, err := canFinishPendingRebases(ctx, jj, state)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("inspect pending rebase conflicts: %w", err))
			result.Success = false
			return true
		}
		if canFinish {
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

	if checkConflicts() {
		return result
	}
	if err := validateCleanup(ctx, plan, state, jj); err != nil {
		result.Errors = append(result.Errors, err)
		result.Success = false
		checkpoint("cleanup-blocked")
		return result
	}

	for _, bookmark := range plan.ToDelete {
		step := "delete:" + bookmark
		if state.StepComplete(step) {
			continue
		}
		existing, err := getExistingBookmarks(ctx, jj)
		if err != nil {
			result.Errors = append(result.Errors, err)
			result.Success = false
			checkpoint("delete-failed")
			return result
		}
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
		if err := validateCleanup(ctx, plan, state, jj); err != nil {
			result.Errors = append(result.Errors, err)
			result.Success = false
			checkpoint("cleanup-blocked")
			return result
		}
		err = jj.ForgetBookmark(ctx, bookmark)
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

	if pendingAbandon(plan, state) {
		// Validate again immediately before the single abandonment operation.
		if err := validateCleanup(ctx, plan, state, jj); err != nil {
			result.Errors = append(result.Errors, err)
			result.Success = false
			checkpoint("cleanup-blocked")
			return result
		}
		if callbacks != nil && callbacks.OnAbandon != nil {
			for _, bookmark := range plan.ToAbandon {
				callbacks.OnAbandon(bookmark)
			}
		}
		err := jj.Abandon(ctx, strings.Join(plan.AbandonCommits, " | "))
		if callbacks != nil && callbacks.OnAbandonComplete != nil {
			for _, bookmark := range plan.ToAbandon {
				callbacks.OnAbandonComplete(bookmark, err)
			}
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("abandon reviewed merged segments: %w", err))
			result.Success = false
			checkpoint("abandon-failed")
			return result
		}
		for _, bookmark := range plan.ToAbandon {
			result.Abandoned = append(result.Abandoned, bookmark)
			state.MarkStepComplete("abandon:" + bookmark)
		}
		if !checkpoint("abandon") {
			return result
		}
	}

	// Abandon while the reviewed heads are still tracked and mutable, then
	// forget their local references before any rebase or push. Colocated Git
	// imports may retain the exact old head; a moved/reused name is protected. Forgetting
	// first can make their untracked remote commits immutable in jj.
	for _, bookmark := range plan.ToAbandon {
		step := "forget:" + bookmark
		if state.StepComplete(step) || !contains(state.PendingSteps, step) {
			continue
		}
		bookmarks, err := jj.ListLocalBookmarks(ctx)
		if err != nil {
			result.Errors = append(result.Errors, err)
			result.Success = false
			checkpoint("forget-failed")
			return result
		}
		for _, current := range bookmarks {
			if current.Name == bookmark && current.CommitID != plan.CleanupHeads[bookmark] {
				result.Errors = append(result.Errors, fmt.Errorf("bookmark %s was recreated after abandonment at %s (reviewed %s); preserving it; abort and replan", bookmark, current.CommitID, plan.CleanupHeads[bookmark]))
				result.Success = false
				checkpoint("forget-blocked")
				return result
			}
		}
		if err := jj.ForgetBookmark(ctx, bookmark); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("forget deleted merged bookmark %s before pushing: %w", bookmark, err))
			result.Success = false
			checkpoint("forget-failed")
			return result
		}
		state.MarkStepComplete(step)
		if !checkpoint("forget") {
			return result
		}
	}
	if len(plan.ToAbandon) > 0 && checkConflicts() {
		return result
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
		existing, err := getExistingBookmarks(ctx, jj)
		if err != nil {
			rebaseErr = err
		} else if !existing[root] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("stack root %s no longer exists; skipping rebase", root))
			state.MarkStepComplete(step)
			if !checkpoint("rebase") {
				return result
			}
			continue
		}
		source := jjutils.BookmarkRevset(root)
		if changeID := plan.RebaseSources[root]; changeID != "" {
			entries, sourceErr := jj.GetLog(ctx, changeID, 2)
			if sourceErr != nil || len(entries) != 1 {
				rebaseErr = fmt.Errorf("rebase source for %s is missing or divergent; resolve it before continuing", root)
			} else {
				source = entries[0].CommitID
				ancestor, ancestorErr := jj.IsAncestor(ctx, source, jjutils.BookmarkRevset(root))
				if ancestorErr != nil || !ancestor {
					rebaseErr = fmt.Errorf("bookmark %s moved outside its reviewed segment; abort and replan sync", root)
				}
			}
		}
		if rebaseErr != nil {
			result.Errors = append(result.Errors, rebaseErr)
			result.Success = false
			checkpoint("rebase-failed")
			return result
		}
		based, err := jj.IsAncestor(ctx, plan.RebaseTarget, source)
		if err != nil {
			rebaseErr = fmt.Errorf("check rebase target for %s: %w", root, err)
		} else if !based {
			rebaseErr = jj.Rebase(ctx, source, plan.RebaseTarget)
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

	if checkConflicts() {
		return result
	}

	existing, err := getExistingBookmarks(ctx, jj)
	if err != nil {
		result.Errors = append(result.Errors, err)
		result.Success = false
		checkpoint("push-failed")
		return result
	}
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
func getExistingBookmarks(ctx context.Context, jj jjutils.JJFunctions) (map[string]bool, error) {
	// Use all local bookmarks, not just "user" bookmarks. Some commands (notably push)
	// require the bookmark to exist regardless of whether it points at trunk() or not.
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks before sync step: %w", err)
	}

	existing := make(map[string]bool)
	for _, bm := range bookmarks {
		existing[bm.Name] = true
	}
	return existing, nil
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
