// Package submit implements the three-phase submission workflow for jj-stacked.
// AIDEV-NOTE: The workflow consists of: Analysis (pure), Planning (queries GitHub),
// and Execution (performs actions). This separation enables testability and dry-run mode.
package submit

import (
	"context"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AnalysisResult is the output of the analysis phase.
// It contains information about the stack to be submitted without any side effects.
type AnalysisResult struct {
	// TargetBookmark is the bookmark being submitted
	TargetBookmark string

	// Stack is the ordered list of bookmarks from trunk to target
	Stack []StackBookmark

	// Warnings are non-fatal validation messages
	Warnings []string

	// Errors are issues that prevent submission
	Errors []error
}

// HasErrors returns true if there are any blocking errors.
func (r *AnalysisResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// StackBookmark represents a bookmark in the submission stack with its metadata.
type StackBookmark struct {
	// Bookmark is the underlying jjutils.Bookmark
	Bookmark jjutils.Bookmark

	// Segment is the bookmark segment containing changes (may be nil for empty segments)
	Segment *jjutils.BookmarkSegment

	// NeedsPush indicates if this bookmark needs to be pushed to remote
	NeedsPush bool

	// Title is the PR title (from first line of description)
	Title string

	// Body is the full PR body (from full description)
	Body string
}

// ExistingPR holds information about a PR that already exists.
type ExistingPR struct {
	Bookmark string
	Number   int
	URL      string
}

// SubmissionPlan is the output of the planning phase.
// It contains the ordered list of actions to perform.
type SubmissionPlan struct {
	// Actions is the ordered list of actions to execute
	Actions []SubmissionAction

	// ExistingPRs tracks PRs that already exist (for display purposes)
	ExistingPRs []ExistingPR

	// Summary provides counts of planned operations
	Summary PlanSummary
}

// ActionType identifies the type of submission action.
type ActionType string

const (
	ActionPush        ActionType = "push"
	ActionCreatePR    ActionType = "create_pr"
	ActionUpdateBase  ActionType = "update_base"
	ActionSyncComment ActionType = "sync_comment"
	ActionClosePR     ActionType = "close_pr"
)

// SubmissionAction is the interface for actions that can be executed.
type SubmissionAction interface {
	// Type returns the action type
	Type() ActionType

	// Description returns a human-readable description
	Description() string

	// Execute performs the action
	Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error)
}

// ActionDeps provides dependencies needed by actions during execution.
type ActionDeps struct {
	JJ     jjutils.JJFunctions
	GitHub github.GitHubClient
	Owner  string
	Repo   string
	Remote string
}

// PlanSummary provides counts of planned operations.
type PlanSummary struct {
	BookmarksToPush int
	PRsToCreate     int
	PRsToUpdate     int
	PRsToClose      int
	CommentsToSync  int
}

// ExecutionResult is the output of the execution phase.
type ExecutionResult struct {
	// Executed contains results of all attempted actions
	Executed []ActionResult

	// Summary provides counts of execution outcomes
	Summary ExecutionSummary
}

// ActionResult is the result of executing a single action.
type ActionResult struct {
	// Action is the action that was executed
	Action SubmissionAction

	// Success indicates if the action succeeded
	Success bool

	// Error is set if the action failed
	Error error

	// Details contains action-specific output (e.g., PR URL, PR number)
	Details map[string]interface{}
}

// ExecutionSummary provides counts of execution outcomes.
type ExecutionSummary struct {
	Succeeded int
	Failed    int
	Skipped   int
}

// PlanningDeps provides dependencies needed during the planning phase.
type PlanningDeps struct {
	GitHub        github.GitHubClient
	Owner         string
	Repo          string
	Remote        string
	DefaultBranch string
	CurrentUser   string // GitHub username of the current user (for filtering orphaned PRs)
}

// PlanningCallbacks provides optional callbacks for planning progress.
type PlanningCallbacks struct {
	// OnProgress is called with progress messages
	OnProgress func(message string)

	// OnBookmarkChecked is called when a bookmark's PR status is checked
	OnBookmarkChecked func(bookmark string, hasPR bool)
}

// ExecutionCallbacks provides optional callbacks for execution progress.
type ExecutionCallbacks struct {
	// OnActionStart is called when an action begins
	OnActionStart func(action SubmissionAction)

	// OnActionComplete is called when an action finishes
	OnActionComplete func(action SubmissionAction, result ActionResult)

	// OnProgress is called with overall progress
	OnProgress func(completed, total int)
}

// Note: For stack comment generation, use github.StackEntry from the github package.
