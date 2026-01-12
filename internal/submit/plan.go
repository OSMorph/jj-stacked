package submit

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
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

	// Phase 4: Create sync comment actions for all PRs
	// We need to wait until we know which PRs will exist
	progress("Planning stack comment sync...")

	// Build the complete stack entries (will be updated after PR creation)
	// For planning, we use what we know now
	stackEntries := buildStackEntries(analysis, prInfo)

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
				Bookmark:     sb.Bookmark.Name,
				PRNumber:     prNumber, // 0 for new PRs - will be filled during execution
				StackEntries: stackEntries,
				BaseBranch:   deps.DefaultBranch,
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
