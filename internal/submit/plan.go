package submit

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/logger"
)

// AIDEV-NOTE: The planning phase queries GitHub to determine what actions are needed.
// It does not modify anything - it only reads state and creates a plan.

// CreateSubmissionPlan creates a plan of actions based on the analysis result.
// This queries GitHub to determine existing PRs and what needs to be created/updated.
func CreateSubmissionPlan(
	ctx context.Context,
	analysis *AnalysisResult,
	deps *PlanningDeps,
	callbacks *PlanningCallbacks,
) (*SubmissionPlan, error) {
	if analysis.HasErrors() {
		return nil, fmt.Errorf("cannot create plan: analysis has errors")
	}

	plan := &SubmissionPlan{
		Actions: make([]SubmissionAction, 0),
	}

	// Track PR info as we discover it
	prInfo := make(map[string]*github.PullRequest) // bookmark -> PR

	// Helper to emit progress
	progress := func(msg string) {
		if callbacks != nil && callbacks.OnProgress != nil {
			callbacks.OnProgress(msg)
		}
	}

	bookmarkChecked := func(bookmark string, hasPR bool) {
		if callbacks != nil && callbacks.OnBookmarkChecked != nil {
			callbacks.OnBookmarkChecked(bookmark, hasPR)
		}
	}

	// Phase 1: Collect push actions for all bookmarks that need push
	progress("Checking which bookmarks need push...")
	for _, sb := range analysis.Stack {
		if sb.NeedsPush {
			plan.Actions = append(plan.Actions, &PushAction{
				Bookmark: sb.Bookmark.Name,
				Remote:   deps.Remote,
			})
			plan.Summary.BookmarksToPush++
		}
	}

	// Phase 2: Query GitHub for existing PRs on each bookmark
	progress("Checking GitHub for existing PRs...")
	for _, sb := range analysis.Stack {
		pr, err := deps.GitHub.FindPRByHead(ctx, deps.Owner, deps.Repo, sb.Bookmark.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check PR for '%s': %w", sb.Bookmark.Name, err)
		}

		if pr != nil {
			prInfo[sb.Bookmark.Name] = pr
			// Track existing PR for display
			plan.ExistingPRs = append(plan.ExistingPRs, ExistingPR{
				Bookmark: sb.Bookmark.Name,
				Number:   pr.Number,
				URL:      pr.URL,
			})
			bookmarkChecked(sb.Bookmark.Name, true)
		} else {
			bookmarkChecked(sb.Bookmark.Name, false)
		}
	}

	// Phase 3: Create PR create/update actions
	progress("Planning PR actions...")
	for i, sb := range analysis.Stack {
		// Determine expected base branch
		var expectedBase string
		if i == 0 {
			// First bookmark in stack - base is default branch
			expectedBase = deps.DefaultBranch
		} else {
			// Stacked bookmark - base is previous bookmark
			expectedBase = analysis.Stack[i-1].Bookmark.Name
		}

		existingPR := prInfo[sb.Bookmark.Name]

		if existingPR == nil {
			// Need to create PR
			plan.Actions = append(plan.Actions, &CreatePRAction{
				Bookmark:   sb.Bookmark.Name,
				Title:      sb.Title,
				Body:       sb.Body,
				BaseBranch: expectedBase,
				Draft:      false, // Will be set by command flags
			})
			plan.Summary.PRsToCreate++
		} else {
			// PR exists - check if base needs update
			if existingPR.Base != expectedBase {
				plan.Actions = append(plan.Actions, &UpdateBaseAction{
					Bookmark: sb.Bookmark.Name,
					PRNumber: existingPR.Number,
					NewBase:  expectedBase,
					OldBase:  existingPR.Base,
				})
				plan.Summary.PRsToUpdate++
			}
		}
	}

	// Phase 4: Detect orphaned PRs (PRs whose branch no longer exists locally)
	// This handles the case where a bookmark was renamed - the old PR should be closed
	progress("Checking for orphaned PRs...")
	orphanedPRs := findOrphanedPRs(ctx, deps, analysis, prInfo)
	for _, orphan := range orphanedPRs {
		plan.Actions = append(plan.Actions, &ClosePRAction{
			PRNumber: orphan.Number,
			Branch:   orphan.Head,
			Reason:   "Branch no longer exists locally (bookmark may have been renamed)",
		})
		plan.Summary.PRsToClose++
	}

	// Phase 5: Create sync comment actions for all PRs
	// We need to wait until we know which PRs will exist
	progress("Planning stack comment sync...")

	// Build the complete stack entries (will be updated after PR creation)
	// For planning, we use what we know now
	stackEntries := buildStackEntries(analysis, prInfo)

	// Compute merged history from existing comments
	mergedHistory := computeMergedHistory(ctx, deps, analysis, prInfo)

	for _, sb := range analysis.Stack {
		existingPR := prInfo[sb.Bookmark.Name]
		prNumber := 0

		if existingPR != nil {
			prNumber = existingPR.Number
		}
		// Note: For new PRs, prNumber will be 0 during planning.
		// The execution phase will need to track created PRs and update sync actions.

		if prNumber > 0 || existingPR == nil {
			// Only add sync action if we'll have a PR (existing or to be created)
			plan.Actions = append(plan.Actions, &SyncCommentAction{
				Bookmark:      sb.Bookmark.Name,
				PRNumber:      prNumber, // 0 for new PRs - will be filled during execution
				StackEntries:  stackEntries,
				BaseBranch:    deps.DefaultBranch,
				MergedHistory: mergedHistory,
			})
			plan.Summary.CommentsToSync++
		}
	}

	return plan, nil
}

// buildStackEntries creates github.StackEntry slice from analysis and PR info.
func buildStackEntries(analysis *AnalysisResult, prInfo map[string]*github.PullRequest) []github.StackEntry {
	entries := make([]github.StackEntry, len(analysis.Stack))

	for i, sb := range analysis.Stack {
		entry := github.StackEntry{
			Bookmark: sb.Bookmark.Name,
		}

		if pr, ok := prInfo[sb.Bookmark.Name]; ok {
			entry.PRNumber = pr.Number
			entry.PRURL = pr.URL
			entry.IsMerged = pr.Merged
		}

		entries[i] = entry
	}

	return entries
}

// SetDraftFlag updates all CreatePRAction in the plan to use the specified draft flag.
func SetDraftFlag(plan *SubmissionPlan, draft bool) {
	for _, action := range plan.Actions {
		if createAction, ok := action.(*CreatePRAction); ok {
			createAction.Draft = draft
		}
	}
}

// GetActionsOfType returns all actions of a specific type.
func GetActionsOfType(plan *SubmissionPlan, actionType ActionType) []SubmissionAction {
	var result []SubmissionAction
	for _, action := range plan.Actions {
		if action.Type() == actionType {
			result = append(result, action)
		}
	}
	return result
}

// findOrphanedPRs finds open PRs that are likely orphaned because their branch
// no longer exists locally. This typically happens when a bookmark is renamed.
func findOrphanedPRs(
	ctx context.Context,
	deps *PlanningDeps,
	analysis *AnalysisResult,
	existingPRs map[string]*github.PullRequest,
) []*github.PullRequest {
	var orphaned []*github.PullRequest

	// Build a set of local bookmark names in this stack
	localBookmarks := make(map[string]bool)
	for _, sb := range analysis.Stack {
		localBookmarks[sb.Bookmark.Name] = true
	}

	// Build a set of base branches used in this stack
	baseBranches := make(map[string]bool)
	baseBranches[deps.DefaultBranch] = true
	for _, sb := range analysis.Stack {
		baseBranches[sb.Bookmark.Name] = true
	}

	// Get all open PRs
	allOpenPRs, err := deps.GitHub.ListOpenPullRequests(ctx, deps.Owner, deps.Repo)
	if err != nil {
		// On error, just skip orphan detection
		return nil
	}

	// Find PRs that:
	// 1. Were created by the current user (to avoid closing teammates' PRs)
	// 2. Have a head branch that doesn't exist in our local bookmarks
	// 3. Have a base branch that IS one of our bookmarks (suggesting it was part of this stack)
	// 4. Have a jj-stacked stack comment
	for _, pr := range allOpenPRs {
		// Skip if the PR wasn't created by the current user
		// This prevents accidentally closing PRs from teammates
		if deps.CurrentUser != "" && pr.Author != deps.CurrentUser {
			continue
		}

		// Skip if the PR's branch exists locally
		if localBookmarks[pr.Head] {
			continue
		}

		// Skip if we already have a PR for this branch (it's not orphaned)
		if _, exists := existingPRs[pr.Head]; exists {
			continue
		}

		// Check if the PR's base is one of our bookmarks (suggesting it was part of this stack)
		if baseBranches[pr.Base] {
			// This PR has a base that's in our stack but its head branch doesn't exist
			// Check if it has a jj-stacked stack comment to confirm it was managed by us
			comments, err := deps.GitHub.ListComments(ctx, deps.Owner, deps.Repo, pr.Number)
			if err != nil {
				continue
			}

			for _, comment := range comments {
				if github.IsStackComment(comment.Body) {
					// This PR has a jj-stacked comment and its branch doesn't exist
					// It's likely orphaned (bookmark was renamed)
					orphaned = append(orphaned, pr)
					break
				}
			}
		}
	}

	return orphaned
}

// computeMergedHistory extracts merged PR history from existing comments and identifies
// newly merged PRs that should be added to the history.
// If parsing an existing comment fails (e.g., due to manual edits), we proceed with
// whatever information we have - the comment will be overwritten with current data.
func computeMergedHistory(
	ctx context.Context,
	deps *PlanningDeps,
	analysis *AnalysisResult,
	prInfo map[string]*github.PullRequest,
) []github.MergedPRInfo {
	log := logger.NewFromEnv()

	// Build a set of current bookmarks in the stack
	currentBookmarks := make(map[string]bool)
	for _, sb := range analysis.Stack {
		currentBookmarks[sb.Bookmark.Name] = true
	}

	// Track merged history from all PRs in the stack
	// Use a map to deduplicate by PR number
	mergedByPRNum := make(map[int]github.MergedPRInfo)

	// First, collect existing merged history from any PR that has a stack comment
	for _, pr := range prInfo {
		if pr == nil || pr.Number == 0 {
			continue
		}

		comments, err := deps.GitHub.ListComments(ctx, deps.Owner, deps.Repo, pr.Number)
		if err != nil {
			continue
		}

		for _, comment := range comments {
			if !github.IsStackComment(comment.Body) {
				continue
			}

			existingData, err := github.ParseStackComment(comment.Body)
			if err != nil || existingData == nil {
				// Comment is malformed (possibly manually edited) - skip extracting
				// history from it. The comment will be overwritten with current data.
				log.Debug("failed to parse stack comment, will overwrite",
					"pr", pr.Number,
					"error", err,
				)
				continue
			}

			// Add existing merged history
			for _, m := range existingData.MergedHistory {
				if _, exists := mergedByPRNum[m.PRNumber]; !exists {
					mergedByPRNum[m.PRNumber] = m
				}
			}

			// Check if any bookmarks from the existing comment are now merged
			// These are bookmarks that were in the previous stack but are no longer
			// in our current stack
			for _, bookmark := range existingData.Bookmarks {
				// Skip if this bookmark is still in our current stack
				if currentBookmarks[bookmark] {
					continue
				}

				// Skip if we don't have PR info for this bookmark
				prNum, hasPRNum := existingData.PRNumbers[bookmark]
				prURL, hasPRURL := existingData.PRURLs[bookmark]
				if !hasPRNum || !hasPRURL || prNum == 0 {
					continue
				}

				// Skip if already in merged history
				if _, exists := mergedByPRNum[prNum]; exists {
					continue
				}

				// Query GitHub to check if this PR was merged
				oldPR, err := deps.GitHub.GetPullRequest(ctx, deps.Owner, deps.Repo, prNum)
				if err != nil {
					continue
				}

				if oldPR.Merged {
					mergedByPRNum[prNum] = github.MergedPRInfo{
						Bookmark:   bookmark,
						PRNumber:   prNum,
						PRURL:      prURL,
						MergedInto: oldPR.Base,
					}
				}
			}

			// Only need to parse one stack comment per PR
			break
		}
	}

	// Convert map to slice
	result := make([]github.MergedPRInfo, 0, len(mergedByPRNum))
	for _, m := range mergedByPRNum {
		result = append(result, m)
	}

	return result
}
