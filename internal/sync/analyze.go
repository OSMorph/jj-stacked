package sync

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: AnalyzeSync is the first phase of the three-phase sync architecture.
// It determines what needs to be synced by:
// 1. Getting trunk info and user bookmarks
// 2. Querying GitHub for merged PRs
// 3. Filtering to only contiguous merged bookmarks from trunk
// 4. Validating the local state is ready for sync

// AnalyzeSync determines what needs to be synced.
// This is a pure analysis phase - no mutations are performed.
//
// Parameters:
//   - ctx: context for cancellation
//   - jj: jujutsu functions interface
//   - gh: GitHub client for API calls
//   - owner: repository owner
//   - repo: repository name
//
// Returns a SyncAnalysis with merged bookmarks, remaining bookmarks,
// and any warnings or errors.
func AnalyzeSync(
	ctx context.Context,
	jj jjutils.JJFunctions,
	gh github.GitHubClient,
	owner, repo string,
) (*SyncAnalysis, error) {
	analysis := &SyncAnalysis{}

	// Step 1: Get trunk info
	trunkInfo, err := jj.GetTrunkInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get trunk info: %w", err)
	}
	analysis.TrunkBranch = trunkInfo.BranchName
	analysis.TrunkChangeID = trunkInfo.ChangeID

	// Step 2: Get user bookmarks
	bookmarks, err := jj.ListUserBookmarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list user bookmarks: %w", err)
	}

	if len(bookmarks) == 0 {
		// Nothing to sync
		return analysis, nil
	}

	// Check which bookmarks need to be pushed (ahead of origin)
	for _, bm := range bookmarks {
		if bm.NeedsPush() {
			analysis.BookmarksNeedingPush = append(analysis.BookmarksNeedingPush, bm.Name)
		}
	}

	// Step 3: Build the change graph to understand stack structure
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build change graph: %w", err)
	}

	// Step 4: Detect which bookmarks have merged PRs
	mergedBookmarks, detectErrors := DetectMergedBookmarks(ctx, bookmarks, gh, owner, repo)
	for _, e := range detectErrors {
		analysis.Warnings = append(analysis.Warnings, e.Error())
	}

	if len(mergedBookmarks) == 0 {
		// No merged PRs found
		for _, bm := range bookmarks {
			analysis.RemainingBookmarks = append(analysis.RemainingBookmarks, bm.Name)
		}
		return analysis, nil
	}

	// Step 5: Filter to only contiguous merged bookmarks from bottom of stack
	contiguousMerged, gapWarnings := FilterMergedFromBottom(mergedBookmarks, graph)
	analysis.Warnings = append(analysis.Warnings, gapWarnings...)
	analysis.MergedBookmarks = contiguousMerged

	// Step 6: Determine remaining bookmarks
	analysis.RemainingBookmarks = GetRemainingBookmarks(bookmarks, contiguousMerged)

	// Step 7: Check for working copy conflicts
	hasConflicts, err := jj.HasConflicts(ctx)
	if err != nil {
		analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not check for conflicts: %v", err))
	} else if hasConflicts {
		analysis.Errors = append(analysis.Errors,
			fmt.Errorf("working copy has conflicts - resolve them before syncing"))
	}

	return analysis, nil
}

// ValidateAnalysis checks if the analysis result is valid for proceeding with sync.
// Returns a list of validation errors if the analysis is not valid.
func ValidateAnalysis(analysis *SyncAnalysis) []error {
	var errors []error

	// Check for blocking errors from analysis
	errors = append(errors, analysis.Errors...)

	// Check for out-of-order merges (these are in warnings)
	for _, warning := range analysis.Warnings {
		// Out-of-order merges are blocking errors
		if len(warning) > 0 && warning[0] == 'b' { // "bookmark X was merged out of order"
			errors = append(errors, fmt.Errorf("%s", warning))
		}
	}

	return errors
}
