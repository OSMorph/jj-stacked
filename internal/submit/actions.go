package submit

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
)

// AIDEV-NOTE: Each action type implements SubmissionAction interface.
// Actions are self-contained and independent - they know how to execute themselves.

// PushAction pushes a bookmark to a remote.
type PushAction struct {
	Bookmark string
	Remote   string
}

func (a *PushAction) Type() ActionType {
	return ActionPush
}

func (a *PushAction) Description() string {
	return fmt.Sprintf("Push bookmark '%s' to %s", a.Bookmark, a.Remote)
}

func (a *PushAction) Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error) {
	result := &ActionResult{
		Action:  a,
		Details: make(map[string]interface{}),
	}

	err := deps.JJ.Push(ctx, deps.Remote, a.Bookmark)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to push bookmark '%s': %w", a.Bookmark, err)
		return result, result.Error
	}

	result.Success = true
	result.Details["bookmark"] = a.Bookmark
	result.Details["remote"] = deps.Remote
	return result, nil
}

// CreatePRAction creates a new pull request.
type CreatePRAction struct {
	Bookmark   string
	Title      string
	Body       string
	BaseBranch string
	Draft      bool
}

func (a *CreatePRAction) Type() ActionType {
	return ActionCreatePR
}

func (a *CreatePRAction) Description() string {
	return fmt.Sprintf("Create PR for '%s' → %s", a.Bookmark, a.BaseBranch)
}

func (a *CreatePRAction) Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error) {
	result := &ActionResult{
		Action:  a,
		Details: make(map[string]interface{}),
	}

	req := &github.CreatePRRequest{
		Title: a.Title,
		Body:  a.Body,
		Head:  a.Bookmark,
		Base:  a.BaseBranch,
		Draft: a.Draft,
	}

	pr, err := deps.GitHub.CreatePullRequest(ctx, deps.Owner, deps.Repo, req)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to create PR for '%s': %w", a.Bookmark, err)
		return result, result.Error
	}

	result.Success = true
	result.Details["pr_number"] = pr.Number
	result.Details["pr_url"] = pr.URL
	result.Details["bookmark"] = a.Bookmark
	result.Details["base"] = a.BaseBranch
	return result, nil
}

// UpdateBaseAction updates the base branch of an existing PR.
type UpdateBaseAction struct {
	Bookmark string
	PRNumber int
	NewBase  string
	OldBase  string
}

func (a *UpdateBaseAction) Type() ActionType {
	return ActionUpdateBase
}

func (a *UpdateBaseAction) Description() string {
	return fmt.Sprintf("Update PR #%d base: %s → %s", a.PRNumber, a.OldBase, a.NewBase)
}

func (a *UpdateBaseAction) Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error) {
	result := &ActionResult{
		Action:  a,
		Details: make(map[string]interface{}),
	}

	req := &github.UpdatePRRequest{
		Base: &a.NewBase,
	}

	_, err := deps.GitHub.UpdatePullRequest(ctx, deps.Owner, deps.Repo, a.PRNumber, req)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to update base for PR #%d: %w", a.PRNumber, err)
		return result, result.Error
	}

	result.Success = true
	result.Details["pr_number"] = a.PRNumber
	result.Details["old_base"] = a.OldBase
	result.Details["new_base"] = a.NewBase
	return result, nil
}

// SyncCommentAction creates or updates the stack navigation comment on a PR.
type SyncCommentAction struct {
	Bookmark      string
	PRNumber      int
	StackEntries  []github.StackEntry
	BaseBranch    string
	MergedHistory []github.MergedPRInfo
}

func (a *SyncCommentAction) Type() ActionType {
	return ActionSyncComment
}

func (a *SyncCommentAction) Description() string {
	return fmt.Sprintf("Sync stack comment on PR #%d", a.PRNumber)
}

func (a *SyncCommentAction) Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error) {
	result := &ActionResult{
		Action:  a,
		Details: make(map[string]interface{}),
	}

	// Build the comment body
	commentBody := github.BuildStackComment(a.StackEntries, a.Bookmark, a.BaseBranch, a.MergedHistory)

	// List existing comments to find our comment
	comments, err := deps.GitHub.ListComments(ctx, deps.Owner, deps.Repo, a.PRNumber)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to list comments on PR #%d: %w", a.PRNumber, err)
		return result, result.Error
	}

	// Find existing jj-stacked comment
	var existingCommentID int64
	for _, comment := range comments {
		if github.IsStackComment(comment.Body) {
			existingCommentID = comment.ID
			break
		}
	}

	if existingCommentID > 0 {
		// Update existing comment
		_, err = deps.GitHub.UpdateComment(ctx, deps.Owner, deps.Repo, existingCommentID, commentBody)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to update comment on PR #%d: %w", a.PRNumber, err)
			return result, result.Error
		}
		result.Details["action"] = "updated"
		result.Details["comment_id"] = existingCommentID
	} else {
		// Create new comment
		comment, err := deps.GitHub.CreateComment(ctx, deps.Owner, deps.Repo, a.PRNumber, commentBody)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to create comment on PR #%d: %w", a.PRNumber, err)
			return result, result.Error
		}
		result.Details["action"] = "created"
		result.Details["comment_id"] = comment.ID
	}

	result.Success = true
	result.Details["pr_number"] = a.PRNumber
	return result, nil
}

// ClosePRAction closes an orphaned PR (PR whose branch no longer exists locally).
type ClosePRAction struct {
	PRNumber int
	Branch   string // The branch name that no longer exists
	Reason   string // Why the PR is being closed
}

func (a *ClosePRAction) Type() ActionType {
	return ActionClosePR
}

func (a *ClosePRAction) Description() string {
	return fmt.Sprintf("Close orphaned PR #%d (branch '%s' no longer exists)", a.PRNumber, a.Branch)
}

func (a *ClosePRAction) Execute(ctx context.Context, deps *ActionDeps) (*ActionResult, error) {
	result := &ActionResult{
		Action:  a,
		Details: make(map[string]interface{}),
	}

	closedState := "closed"
	req := &github.UpdatePRRequest{
		State: &closedState,
	}

	_, err := deps.GitHub.UpdatePullRequest(ctx, deps.Owner, deps.Repo, a.PRNumber, req)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to close PR #%d: %w", a.PRNumber, err)
		return result, result.Error
	}

	result.Success = true
	result.Details["pr_number"] = a.PRNumber
	result.Details["branch"] = a.Branch
	result.Details["reason"] = a.Reason
	return result, nil
}

// Ensure all action types implement SubmissionAction
var (
	_ SubmissionAction = (*PushAction)(nil)
	_ SubmissionAction = (*CreatePRAction)(nil)
	_ SubmissionAction = (*UpdateBaseAction)(nil)
	_ SubmissionAction = (*SyncCommentAction)(nil)
	_ SubmissionAction = (*ClosePRAction)(nil)
)
