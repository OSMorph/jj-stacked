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
	analysis := &SyncAnalysis{Remote: opts.Remote, RebaseSources: make(map[string]string)}
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
	target := jjutils.RemoteBookmarkRevset(analysis.TrunkBranch, analysis.Remote)
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
	incorporatedNames := make(map[string]bool)
	if opts.Bookmark != "" {
		bookmarkNames = graph.GetConnectedBookmarks(opts.Bookmark)
		if len(bookmarkNames) == 0 {
			anchor, exists := remoteStatus[opts.Bookmark]
			if !exists {
				return nil, fmt.Errorf("bookmark %q not found in any stack", opts.Bookmark)
			}
			// Several consecutive stack bookmarks can disappear into fetched
			// trunk at once. Retain that exact incorporated lineage as both
			// cleanup scope and the boundary for surviving draft roots. Draft
			// descendants of A alone do not establish stack membership.
			incorporatedTargets := make(map[string]bool)
			for _, candidate := range remoteBookmarks {
				if candidate.Name == analysis.TrunkBranch {
					continue
				}
				inTrunk, err := jj.IsAncestor(ctx, candidate.CommitID, target)
				if err != nil {
					return nil, fmt.Errorf("inspect incorporated scope for %s: %w", candidate.Name, err)
				}
				if !inTrunk {
					continue
				}
				inLineage, err := jj.IsAncestor(ctx, anchor.CommitID, candidate.CommitID)
				if err != nil {
					return nil, fmt.Errorf("inspect selected lineage for %s: %w", candidate.Name, err)
				}
				if inLineage {
					incorporatedNames[candidate.Name] = true
					incorporatedTargets[candidate.CommitID] = true
				}
			}
			seen := make(map[string]bool)
			for _, root := range graph.Roots {
				segment := graph.Segments[root]
				if segment == nil || len(segment.Changes) == 0 {
					continue
				}
				fromLineage := false
				for _, parent := range segment.Changes[0].Parents {
					if incorporatedTargets[parent] {
						fromLineage = true
						break
					}
				}
				if !fromLineage {
					continue
				}
				for _, name := range graph.GetConnectedBookmarks(root) {
					if !seen[name] {
						bookmarkNames = append(bookmarkNames, name)
						seen[name] = true
					}
				}
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
		if detected[candidate.Name] || candidate.Name == analysis.TrunkBranch {
			continue
		}
		include := opts.Bookmark == "" || incorporatedNames[candidate.Name]
		if !include && anchor.CommitID != "" {
			ancestor, err := jj.IsAncestor(ctx, candidate.CommitID, anchor.CommitID)
			if err != nil {
				analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not determine whether bookmark %s belongs to selected stack", candidate.Name))
				continue
			}
			include = ancestor
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
	verifiedMerged := make([]MergedBookmark, 0, len(mergedBookmarks))
	for i := range mergedBookmarks {
		merged := &mergedBookmarks[i]
		inTrunk, err := jj.IsAncestor(ctx, merged.CommitID, target)
		if err != nil {
			analysis.Errors = append(analysis.Errors, fmt.Errorf("determine whether merged bookmark %s is already in trunk: %w", merged.Name, err))
			continue
		}
		merged.InTrunk = inTrunk
		if !inTrunk {
			segment := graph.Segments[merged.Name]
			if segment == nil || len(segment.Changes) == 0 || segment.Changes[len(segment.Changes)-1].CommitID != merged.CommitID {
				analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("cannot establish complete merged segment for %s; preserving it for review", merged.Name))
				continue
			}
			for i := range segment.Changes {
				merged.Commits = append(merged.Commits, segment.Changes[i].CommitID)
			}
		}
		verifiedMerged = append(verifiedMerged, *merged)
	}

	verifiedMerged, proofErrors := proveMergedDestinations(ctx, jj, verifiedMerged, analysis.TrunkBranch, target)
	analysis.Errors = append(analysis.Errors, proofErrors...)

	// Step 5: Filter to only contiguous merged bookmarks from bottom of stack
	contiguousMerged, gapErrors := FilterMergedFromBottom(verifiedMerged, graph)
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
			segment := graph.Segments[name]
			if segment == nil || len(segment.Changes) == 0 {
				analysis.Errors = append(analysis.Errors, fmt.Errorf("cannot establish complete rebase segment for %s", name))
				continue
			}
			analysis.RebaseSources[name] = segment.Changes[0].ChangeID
		}
	}

	// Step 8: Check for repository conflicts
	hasConflicts, err := jj.HasConflicts(ctx)
	if err != nil {
		analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not check for conflicts: %v", err))
	} else if hasConflicts {
		analysis.ConflictDetail = conflictsError(ctx, jj)
		analysis.Errors = append(analysis.Errors, analysis.ConflictDetail)
	}

	return analysis, nil
}

// ValidateAnalysis checks if the analysis result is valid for proceeding with sync.
// Returns a list of validation errors if the analysis is not valid.
func ValidateAnalysis(analysis *SyncAnalysis) []error {
	return append([]error(nil), analysis.Errors...)
}
