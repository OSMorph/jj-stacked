package submit

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
)

// AIDEV-NOTE: The execution phase performs all planned actions.
// It executes in order: protect bases → push → create PR → set final bases → sync comments.
// Protection, push, PR creation, and final-base failures abort dependent work.

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

	// Each execution owns its PR discoveries; the reviewed plan remains unchanged.
	executionDeps := *deps
	executionDeps.createdPRs = make(map[string]*github.PullRequest)

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
			result.Summary.Skipped += total - completed
			return result, ctx.Err()
		default:
		}

		notifyStart(action)

		actionResult := action.Execute(ctx, &executionDeps)
		actionResult.Action = action

		result.Executed = append(result.Executed, actionResult)

		switch {
		case actionResult.Skipped:
			result.Summary.Skipped++
		case actionResult.Error == nil:
			result.Summary.Succeeded++
			if actionResult.CreatedPR != nil {
				executionDeps.createdPRs[actionResult.Bookmark] = actionResult.CreatedPR
			}
		default:
			result.Summary.Failed++

			// Determine if this is a critical error that should abort
			if isCriticalAction(action) {
				// Critical failure - abort remaining actions
				remainingActions := total - completed - 1
				result.Summary.Skipped += remainingActions
				notifyComplete(action, actionResult)
				return result, criticalActionError(action, actionResult.Error, result.Executed)
			}
		}

		completed++
		notifyComplete(action, actionResult)
		notifyProgress(completed, total)
	}

	return result, nil
}

// isCriticalAction returns true if failure of this action should abort execution.
func isCriticalAction(action SubmissionAction) bool {
	switch action.Type() {
	case ActionProtectBase, ActionPush, ActionCreatePR, ActionUpdateBase:
		return true
	case ActionSyncComment:
		return false
	default:
		return false
	}
}

func criticalActionError(action SubmissionAction, actionErr error, executed []ActionResult) error {
	if action.Type() == ActionProtectBase {
		return fmt.Errorf("protective base update failed before any push; fix the PR base error and rerun submit: %w", actionErr)
	}
	if action.Type() == ActionUpdateBase {
		return fmt.Errorf("PR base update failed; rerun the current command. For an interrupted sync, use sync --continue to finish base and comment updates: %w", actionErr)
	}
	for _, result := range executed {
		if result.Action.Type() == ActionProtectBase && result.Error == nil {
			return fmt.Errorf("submission stopped after protective base updates; fix the error and rerun submit to finish final bases and comments: %w", actionErr)
		}
	}
	return fmt.Errorf("critical action failed: %w", actionErr)
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
		if ar.Error != nil {
			failed = append(failed, ar)
		}
	}
	return failed
}

// GetCreatedPRURLs returns URLs of all PRs created during execution.
func GetCreatedPRURLs(result *ExecutionResult) []string {
	var urls []string
	for _, ar := range result.Executed {
		if ar.Error == nil && ar.CreatedPR != nil {
			urls = append(urls, ar.CreatedPR.URL)
		}
	}
	return urls
}
