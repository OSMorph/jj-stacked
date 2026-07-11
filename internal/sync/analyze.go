package sync

import (
	"context"
	"fmt"
	"sort"

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
	return AnalyzeSyncWithOptions(ctx, jj, gh, owner, repo, AnalyzeOptions{})
}

// AnalyzeSyncWithOptions determines what needs to be synced with configurable options.
// If opts.Bookmark is specified, only that bookmark's stack will be analyzed.
func AnalyzeSyncWithOptions(
	ctx context.Context,
	jj jjutils.JJFunctions,
	gh github.GitHubClient,
	owner, repo string,
	opts AnalyzeOptions,
) (*SyncAnalysis, error) {
	analysis := &SyncAnalysis{Remote: opts.Remote}
	if analysis.Remote == "" {
		analysis.Remote = "origin"
	}

	// Step 1: Get trunk info
	if opts.TrunkBranch == "" {
		trunkInfo, err := jj.GetTrunkInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get trunk info: %w", err)
		}
		analysis.TrunkBranch = trunkInfo.BranchName
	} else {
		analysis.TrunkBranch = opts.TrunkBranch
	}
	target := fmt.Sprintf("%s@%s", analysis.TrunkBranch, analysis.Remote)
	entries, err := jj.GetLog(ctx, target, 1)
	if err != nil {
		return nil, fmt.Errorf("resolve selected remote trunk %s: %w", target, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("selected remote trunk %s returned no commits", target)
	}
	analysis.TrunkChangeID = entries[0].ChangeID

	// Step 2: Get user bookmarks
	allBookmarks, err := jj.ListUserBookmarksForBase(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to list user bookmarks: %w", err)
	}

	remoteBookmarks, err := jj.ListBookmarksForRemote(ctx, analysis.Remote)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect bookmarks for remote %s: %w", analysis.Remote, err)
	}
	remoteStatus := make(map[string]jjutils.Bookmark, len(remoteBookmarks))
	for _, bm := range remoteBookmarks {
		remoteStatus[bm.Name] = bm
	}
	for i := range allBookmarks {
		if status, ok := remoteStatus[allBookmarks[i].Name]; ok {
			allBookmarks[i].HasRemote = status.HasRemote
			allBookmarks[i].RemoteName = status.RemoteName
			allBookmarks[i].IsSynced = status.IsSynced
		}
	}

	// Step 3: Build the change graph to understand stack structure
	graph, err := jj.BuildChangeGraphForBase(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to build change graph: %w", err)
	}

	// Step 3.5: Select either the whole connected stack or every stack.
	var bookmarkNames []string
	if opts.Bookmark != "" {
		bookmarkNames = graph.GetConnectedBookmarks(opts.Bookmark)
		if len(bookmarkNames) == 0 {
			if _, exists := remoteStatus[opts.Bookmark]; !exists {
				return nil, fmt.Errorf("bookmark %q not found in any stack", opts.Bookmark)
			}
		}
	} else {
		roots := append([]string(nil), graph.Roots...)
		sort.Strings(roots)
		seen := make(map[string]bool)
		for _, root := range roots {
			for _, name := range graph.GetConnectedBookmarks(root) {
				if !seen[name] {
					seen[name] = true
					bookmarkNames = append(bookmarkNames, name)
				}
			}
		}
	}

	byName := make(map[string]jjutils.Bookmark, len(allBookmarks))
	for _, bm := range allBookmarks {
		byName[bm.Name] = bm
	}
	bookmarks := make([]jjutils.Bookmark, 0, len(bookmarkNames))
	for _, name := range bookmarkNames {
		if bm, ok := byName[name]; ok {
			bookmarks = append(bookmarks, bm)
		}
	}

	// Merged bookmarks whose commits are already in trunk are intentionally
	// excluded by ListUserBookmarks. Include them for cleanup, while preserving
	// selected-stack scope by requiring ancestry with the selected bookmark.
	detectionBookmarks := append([]jjutils.Bookmark(nil), bookmarks...)
	detected := make(map[string]bool, len(detectionBookmarks))
	for _, bm := range detectionBookmarks {
		detected[bm.Name] = true
	}
	var anchor jjutils.Bookmark
	if opts.Bookmark != "" {
		anchor = remoteStatus[opts.Bookmark]
	}
	for _, candidate := range remoteBookmarks {
		if detected[candidate.Name] || candidate.Name == "main" || candidate.Name == "master" || candidate.Name == "trunk" {
			continue
		}
		include := opts.Bookmark == ""
		if !include && anchor.ChangeID != "" {
			ancestor, ancestorErr := jj.IsAncestor(ctx, candidate.ChangeID, anchor.ChangeID)
			descendant, descendantErr := jj.IsAncestor(ctx, anchor.ChangeID, candidate.ChangeID)
			if ancestorErr != nil || descendantErr != nil {
				analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not determine whether bookmark %s belongs to selected stack", candidate.Name))
				continue
			}
			include = ancestor || descendant
		}
		if include {
			detectionBookmarks = append(detectionBookmarks, candidate)
			detected[candidate.Name] = true
		}
	}

	// Check which bookmarks need to be pushed (ahead of origin)
	for _, bm := range bookmarks {
		if bm.NeedsPushTo(analysis.Remote) {
			analysis.BookmarksNeedingPush = append(analysis.BookmarksNeedingPush, bm.Name)
		}
	}

	// Step 4: Detect which bookmarks have merged PRs
	mergedBookmarks, detectErrors := DetectMergedBookmarks(ctx, detectionBookmarks, gh, owner, repo)
	for _, e := range detectErrors {
		analysis.Warnings = append(analysis.Warnings, e.Error())
	}
	for i := range mergedBookmarks {
		inTrunk, err := jj.IsAncestor(ctx, mergedBookmarks[i].ChangeID, target)
		if err != nil {
			analysis.Errors = append(analysis.Errors, fmt.Errorf("determine whether merged bookmark %s is already in trunk: %w", mergedBookmarks[i].Name, err))
			continue
		}
		mergedBookmarks[i].InTrunk = inTrunk
	}

	// Step 5: Filter to only contiguous merged bookmarks from bottom of stack
	contiguousMerged, gapErrors := FilterMergedFromBottom(mergedBookmarks, graph)
	analysis.Errors = append(analysis.Errors, gapErrors...)
	analysis.MergedBookmarks = contiguousMerged

	// Step 6: Determine remaining bookmarks
	analysis.RemainingBookmarks = GetRemainingBookmarks(bookmarks, contiguousMerged)

	// Step 7: Identify independent remaining roots and whether remote trunk is
	// already in their ancestry. Only those roots need rebasing.
	remainingSet := make(map[string]bool, len(analysis.RemainingBookmarks))
	for _, name := range analysis.RemainingBookmarks {
		remainingSet[name] = true
	}
	for _, name := range analysis.RemainingBookmarks {
		parent := graph.ChildToParent[name]
		if parent != "" && remainingSet[parent] {
			continue
		}
		bm, ok := byName[name]
		if !ok {
			continue
		}
		basedOnTrunk, err := jj.IsAncestor(ctx, target, bm.ChangeID)
		if err != nil {
			analysis.Errors = append(analysis.Errors, fmt.Errorf("check whether %s is based on %s: %w", name, target, err))
			continue
		}
		if !basedOnTrunk || len(contiguousMerged) > 0 {
			analysis.RebaseRoots = append(analysis.RebaseRoots, name)
		}
	}

	// Step 8: Check for repository conflicts
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
	return append([]error(nil), analysis.Errors...)
}
