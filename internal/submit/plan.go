package submit

import (
	"context"
	"fmt"
	"sort"

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

	// Helper to emit progress
	progress := func(msg string) {
		if callbacks != nil && callbacks.OnProgress != nil {
			callbacks.OnProgress(msg)
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
	prInfo, err := discoverPRs(ctx, analysis, deps, plan, callbacks)
	if err != nil {
		return nil, err
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
		} else if existingPR.Base != expectedBase {
			// PR exists and base needs update
			plan.Actions = append(plan.Actions, &UpdateBaseAction{
				Bookmark: sb.Bookmark.Name,
				PRNumber: existingPR.Number,
				NewBase:  expectedBase,
				OldBase:  existingPR.Base,
			})
			plan.Summary.PRsToUpdate++
		}
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

// CreatePRRefreshPlan plans only idempotent updates to existing PR bases and
// stack comments. It never pushes, creates, or closes pull requests.
func CreatePRRefreshPlan(
	ctx context.Context,
	analysis *AnalysisResult,
	deps *PlanningDeps,
	callbacks *PlanningCallbacks,
) (*SubmissionPlan, error) {
	if analysis.HasErrors() {
		return nil, fmt.Errorf("cannot create plan: analysis has errors")
	}
	plan := &SubmissionPlan{}
	prInfo, err := discoverPRs(ctx, analysis, deps, plan, callbacks)
	if err != nil {
		return nil, err
	}
	for i, sb := range analysis.Stack {
		pr := prInfo[sb.Bookmark.Name]
		if pr == nil {
			continue
		}
		base := deps.DefaultBranch
		if i > 0 {
			base = analysis.Stack[i-1].Bookmark.Name
		}
		if pr.Base != base {
			plan.Actions = append(plan.Actions, &UpdateBaseAction{
				Bookmark: sb.Bookmark.Name, PRNumber: pr.Number, OldBase: pr.Base, NewBase: base,
			})
			plan.Summary.PRsToUpdate++
		}
	}
	entries := buildStackEntries(analysis, prInfo)
	history := computeMergedHistory(ctx, deps, analysis, prInfo)
	for _, sb := range analysis.Stack {
		if pr := prInfo[sb.Bookmark.Name]; pr != nil {
			plan.Actions = append(plan.Actions, &SyncCommentAction{
				Bookmark: sb.Bookmark.Name, PRNumber: pr.Number,
				StackEntries: entries, BaseBranch: deps.DefaultBranch, MergedHistory: history,
			})
			plan.Summary.CommentsToSync++
		}
	}
	return plan, nil
}

func discoverPRs(ctx context.Context, analysis *AnalysisResult, deps *PlanningDeps, plan *SubmissionPlan, callbacks *PlanningCallbacks) (map[string]*github.PullRequest, error) {
	prs := make(map[string]*github.PullRequest)
	for _, sb := range analysis.Stack {
		pr, err := deps.GitHub.FindPRByHead(ctx, deps.Owner, deps.Repo, sb.Bookmark.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check PR for %q: %w", sb.Bookmark.Name, err)
		}
		if pr != nil {
			prs[sb.Bookmark.Name] = pr
			plan.ExistingPRs = append(plan.ExistingPRs, ExistingPR{Bookmark: sb.Bookmark.Name, Number: pr.Number, URL: pr.URL})
		}
		if callbacks != nil && callbacks.OnBookmarkChecked != nil {
			callbacks.OnBookmarkChecked(sb.Bookmark.Name, pr != nil)
		}
	}
	return prs, nil
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
	for _, sb := range analysis.Stack {
		pr := prInfo[sb.Bookmark.Name]
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

				if oldPR != nil && oldPR.Merged {
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

	sort.Slice(result, func(i, j int) bool { return result[i].PRNumber < result[j].PRNumber })
	return result
}
