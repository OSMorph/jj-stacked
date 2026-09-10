package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: Conflict handling is critical for user experience during sync.
// When rebase results in conflicts, we need to:
// 1. Detect the conflict state
// 2. Provide clear instructions for resolution
// 3. Support --continue to resume after resolution
// 4. Support --abort to cancel the sync

// ConflictInfo contains information about detected conflicts.
type ConflictInfo struct {
	// HasConflicts indicates whether conflicts are present
	HasConflicts bool

	// Files lists the files with conflicts
	Files []string

	// CurrentBookmark is the bookmark being rebased when conflict occurred
	CurrentBookmark string
}

// CheckForConflicts detects if the working copy has conflicts.
func CheckForConflicts(ctx context.Context, jj jjutils.JJFunctions) (*ConflictInfo, error) {
	hasConflicts, err := jj.HasConflicts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check for conflicts: %w", err)
	}

	info := &ConflictInfo{
		HasConflicts: hasConflicts,
	}

	if hasConflicts {
		info.Files, _ = jj.GetConflictFiles(ctx)
	}

	return info, nil
}

// FormatConflictInstructions returns user instructions for resolving conflicts.
func FormatConflictInstructions(info *ConflictInfo) string {
	var sb strings.Builder

	sb.WriteString("Sync paused: rebase conflicts detected\n\n")

	if len(info.Files) > 0 {
		sb.WriteString("Conflicts in:\n")
		for _, file := range info.Files {
			sb.WriteString(fmt.Sprintf("  - %s\n", file))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("To resolve:\n")
	sb.WriteString("  1. Run: jj resolve --list\n")
	sb.WriteString("  2. Resolve each file with 'jj resolve <path>' or edit it directly\n")
	sb.WriteString("  3. Confirm 'jj status' reports no conflicts\n")
	sb.WriteString("  4. Run: jjk sync --continue\n")
	sb.WriteString("\n")
	sb.WriteString("To abort:\n")
	sb.WriteString("  jjk sync --abort\n")

	return sb.String()
}

// FormatAbortInstructions returns instructions for aborting a sync.
func FormatAbortInstructions() string {
	return "The repository was restored to its pre-sync jj operation. Remote pushes, if any, cannot be undone automatically.\n"
}

// FormatContinueInstructions returns instructions for continuing a sync.
func FormatContinueInstructions() string {
	return `To continue the sync after resolving conflicts:

  1. Run: jj resolve --list
  2. Resolve each listed file and confirm jj status is conflict-free
  3. Run: jjk sync --continue
     This resumes at the first incomplete push or PR refresh step

If conflicts remain, jj-stacked will pause again for resolution.
`
}

// ValidateCanContinue checks if sync can be continued.
// Returns an error if conflicts are still present or if there's no sync in progress.
func ValidateCanContinue(ctx context.Context, jj jjutils.JJFunctions, state *SyncState) error {
	if state == nil {
		return fmt.Errorf("no sync in progress - nothing to continue")
	}

	if state.Plan == nil {
		return fmt.Errorf("saved sync state has no plan; run sync --abort")
	}
	if !state.NoResubmit {
		if _, err := state.RefreshSelection(); err != nil {
			return err
		}
	}

	// Check for remaining conflicts
	hasConflicts, err := jj.HasConflicts(ctx)
	if err != nil {
		return fmt.Errorf("failed to check conflicts: %w", err)
	}

	if hasConflicts {
		canFinish, err := canFinishPendingRebases(ctx, jj, state)
		if err != nil {
			return err
		}
		if canFinish {
			return nil
		}
		return fmt.Errorf("conflicts still present - resolve them before continuing")
	}

	return nil
}

// ValidateCanAbort checks if sync can be aborted.
func ValidateCanAbort(state *SyncState) error {
	if state == nil {
		return fmt.Errorf("no sync in progress - nothing to abort")
	}

	return nil
}

// canFinishPendingRebases permits only intermediate conflicts underneath the
// surviving segments which cleanup is still scheduled to rebase. Conflicts in
// unrelated work or a completed rebase remain blocking.
func canFinishPendingRebases(ctx context.Context, jj jjutils.JJFunctions, state *SyncState) (bool, error) {
	if state.Plan == nil || len(state.Plan.ToAbandon) == 0 {
		return false, nil
	}
	for _, name := range state.Plan.ToAbandon {
		if !state.StepComplete("abandon:" + name) {
			return false, nil
		}
	}
	var subtrees []string
	for _, root := range state.Plan.RebaseRoots {
		if state.StepComplete("rebase:" + root) {
			continue
		}
		changeID := state.Plan.RebaseSources[root]
		if changeID == "" {
			return false, nil
		}
		entries, err := jj.GetLog(ctx, changeID, 2)
		if err != nil {
			return false, err
		}
		if len(entries) != 1 {
			return false, fmt.Errorf("pending rebase source for %s is missing or divergent", root)
		}
		inside, err := jj.IsAncestor(ctx, entries[0].CommitID, jjutils.BookmarkRevset(root))
		if err != nil {
			return false, err
		}
		if !inside {
			return false, fmt.Errorf("pending bookmark %s moved outside its reviewed segment", root)
		}
		subtrees = append(subtrees, "("+entries[0].CommitID+")::")
	}
	if len(subtrees) == 0 {
		return false, nil
	}
	outside, err := jj.GetLog(ctx, "conflicts() ~ ("+strings.Join(subtrees, " | ")+")", 1)
	return len(outside) == 0, err
}
