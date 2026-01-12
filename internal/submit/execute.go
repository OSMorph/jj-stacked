package submit

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
)

// AIDEV-NOTE: The execution phase performs all planned actions.
// It executes in order: push → create PR → update base → sync comments.
// Push failures abort execution. Other failures are collected but continue.

// ExecuteSubmissionPlan executes all actions in the plan.
func ExecuteSubmissionPlan(
	ctx context.Context,
	plan *SubmissionPlan,
	deps *ActionDeps,
	callbacks *ExecutionCallbacks,
) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Executed: make([]ActionResult, 0, len(plan.Actions)),
	}

	// Track created PRs so we can update sync comment actions
	createdPRs := make(map[string]*github.PullRequest) // bookmark -> PR

	// Helper functions for callbacks
	notifyStart := func(action SubmissionAction) {
		if callbacks != nil && callbacks.OnActionStart != nil {
			callbacks.OnActionStart(action)
		}
	}

	notifyComplete := func(action SubmissionAction, ar ActionResult) {
		if callbacks != nil && callbacks.OnActionComplete != nil {
			callbacks.OnActionComplete(action, ar)
		}
	}

	notifyProgress := func(completed, total int) {
		if callbacks != nil && callbacks.OnProgress != nil {
			callbacks.OnProgress(completed, total)
		}
	}

	total := len(plan.Actions)
	completed := 0

	// Execute actions in order
	for _, action := range plan.Actions {
		// Check context cancellation
		select {
		case <-ctx.Done():
			result.Summary.Skipped = total - completed
			return result, ctx.Err()
		default:
		}

		notifyStart(action)

		// Special handling for sync comment actions - update PR number if we created it
		if syncAction, ok := action.(*SyncCommentAction); ok {
			if syncAction.PRNumber == 0 {
				// Look up the PR number from recently created PRs
				if pr, found := createdPRs[syncAction.Bookmark]; found {
					syncAction.PRNumber = pr.Number
					// Also update stack entries
					syncAction.StackEntries = updateStackEntries(syncAction.StackEntries, createdPRs)
				} else {
					// No PR to sync comment on - skip this action
					result.Summary.Skipped++
					completed++
					notifyProgress(completed, total)
					continue
				}
			} else {
				// Update stack entries with any newly created PRs
				syncAction.StackEntries = updateStackEntries(syncAction.StackEntries, createdPRs)
			}
		}

		// Execute the action
		actionResult, err := action.Execute(ctx, deps)
		if actionResult == nil {
			actionResult = &ActionResult{
				Action:  action,
				Success: false,
				Error:   err,
				Details: make(map[string]interface{}),
			}
		}

		result.Executed = append(result.Executed, *actionResult)

		if actionResult.Success {
			result.Summary.Succeeded++

			// Track created PRs for later use
			if createAction, ok := action.(*CreatePRAction); ok {
				if prNum, ok := actionResult.Details["pr_number"].(int); ok {
					prURL, _ := actionResult.Details["pr_url"].(string)
					createdPRs[createAction.Bookmark] = &github.PullRequest{
						Number: prNum,
						URL:    prURL,
						Head:   createAction.Bookmark,
						Base:   createAction.BaseBranch,
					}
				}
			}
		} else {
			result.Summary.Failed++

			// Determine if this is a critical error that should abort
			if isCriticalAction(action) {
				// Critical failure - abort remaining actions
				remainingActions := total - completed - 1
				result.Summary.Skipped = remainingActions
				notifyComplete(action, *actionResult)
				return result, fmt.Errorf("critical action failed: %w", actionResult.Error)
			}
		}

		completed++
		notifyComplete(action, *actionResult)
		notifyProgress(completed, total)
	}

	return result, nil
}

// isCriticalAction returns true if failure of this action should abort execution.
func isCriticalAction(action SubmissionAction) bool {
	switch action.Type() {
	case ActionPush:
		// Push failures are critical - can't create PR without push
		return true
	case ActionCreatePR:
		// PR creation failures are critical for that PR's workflow
		return true
	case ActionUpdateBase, ActionSyncComment:
		// These are non-critical - continue on failure
		return false
	default:
		return false
	}
}

// updateStackEntries updates stack entries with newly created PR info.
func updateStackEntries(entries []github.StackEntry, createdPRs map[string]*github.PullRequest) []github.StackEntry {
	updated := make([]github.StackEntry, len(entries))
	copy(updated, entries)

	for i, entry := range updated {
		if entry.PRNumber == 0 {
			if pr, found := createdPRs[entry.Bookmark]; found {
				updated[i].PRNumber = pr.Number
				updated[i].PRURL = pr.URL
			}
		}
	}

	return updated
}

// ExecuteDryRun validates the plan without executing.
// Returns nil if the plan looks valid.
func ExecuteDryRun(plan *SubmissionPlan) error {
	for _, action := range plan.Actions {
		switch a := action.(type) {
		case *PushAction:
			if a.Bookmark == "" {
				return fmt.Errorf("push action missing bookmark")
			}
		case *CreatePRAction:
			if a.Bookmark == "" {
				return fmt.Errorf("create PR action missing bookmark")
			}
			if a.BaseBranch == "" {
				return fmt.Errorf("create PR action missing base branch")
			}
		case *UpdateBaseAction:
			if a.PRNumber == 0 {
				return fmt.Errorf("update base action missing PR number")
			}
		case *SyncCommentAction:
			// PRNumber can be 0 if we're creating the PR
		}
	}
	return nil
}

// GetFailedActions returns all failed actions from the result.
func GetFailedActions(result *ExecutionResult) []ActionResult {
	var failed []ActionResult
	for _, ar := range result.Executed {
		if !ar.Success {
			failed = append(failed, ar)
		}
	}
	return failed
}

// GetCreatedPRURLs returns URLs of all PRs created during execution.
func GetCreatedPRURLs(result *ExecutionResult) []string {
	var urls []string
	for _, ar := range result.Executed {
		if ar.Action.Type() == ActionCreatePR && ar.Success {
			if url, ok := ar.Details["pr_url"].(string); ok {
				urls = append(urls, url)
			}
		}
	}
	return urls
}
