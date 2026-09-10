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

// Type implements SubmissionAction.
func (a *PushAction) Type() ActionType {
	return ActionPush
}

// Description implements SubmissionAction.
func (a *PushAction) Description() string {
	return fmt.Sprintf("Push bookmark '%s' to %s", a.Bookmark, a.Remote)
}

// Execute implements SubmissionAction.
func (a *PushAction) Execute(ctx context.Context, deps *ActionDeps) ActionResult {
	result := ActionResult{Action: a}

	err := deps.JJ.Push(ctx, deps.Remote, a.Bookmark)
	if err != nil {
		result.Error = fmt.Errorf("failed to push bookmark '%s': %w", a.Bookmark, err)
		return result
	}

	return result
}

// CreatePRAction creates a new pull request.
type CreatePRAction struct {
	Bookmark   string
	Title      string
	Body       string
	BaseBranch string
	Draft      bool
}

// Type implements SubmissionAction.
func (a *CreatePRAction) Type() ActionType {
	return ActionCreatePR
}

// Description implements SubmissionAction.
func (a *CreatePRAction) Description() string {
	return fmt.Sprintf("Create PR for '%s' → %s", a.Bookmark, a.BaseBranch)
}

// Execute implements SubmissionAction.
func (a *CreatePRAction) Execute(ctx context.Context, deps *ActionDeps) ActionResult {
	result := ActionResult{Action: a}

	req := &github.CreatePRRequest{
		Title: a.Title,
		Body:  a.Body,
		Head:  a.Bookmark,
		Base:  a.BaseBranch,
		Draft: a.Draft,
	}

	pr, err := deps.GitHub.CreatePullRequest(ctx, deps.Owner, deps.Repo, req)
	if err != nil {
		result.Error = fmt.Errorf("failed to create PR for '%s': %w", a.Bookmark, err)
		return result
	}

	result.PRNumber = pr.Number
	result.CreatedPR = pr
	result.Bookmark = a.Bookmark
	return result
}

// UpdateBaseAction updates the base branch of an existing PR.
type UpdateBaseAction struct {
	Bookmark string
	PRNumber int
	NewBase  string
	OldBase  string
}

// Type implements SubmissionAction.
func (a *UpdateBaseAction) Type() ActionType {
	return ActionUpdateBase
}

// Description implements SubmissionAction.
func (a *UpdateBaseAction) Description() string {
	return fmt.Sprintf("Update PR #%d base: %s → %s", a.PRNumber, a.OldBase, a.NewBase)
}

// Execute implements SubmissionAction.
func (a *UpdateBaseAction) Execute(ctx context.Context, deps *ActionDeps) ActionResult {
	result := ActionResult{Action: a}

	req := &github.UpdatePRRequest{
		Base: &a.NewBase,
	}

	_, err := deps.GitHub.UpdatePullRequest(ctx, deps.Owner, deps.Repo, a.PRNumber, req)
	if err != nil {
		result.Error = fmt.Errorf("failed to update base for PR #%d: %w", a.PRNumber, err)
		return result
	}

	result.PRNumber = a.PRNumber
	return result
}

// SyncCommentAction creates or updates the stack navigation comment on a PR.
type SyncCommentAction struct {
	Bookmark      string
	PRNumber      int
	StackEntries  []github.StackEntry
	BaseBranch    string
	MergedHistory []github.MergedPRInfo
}

// Type implements SubmissionAction.
func (a *SyncCommentAction) Type() ActionType {
	return ActionSyncComment
}

// Description implements SubmissionAction.
func (a *SyncCommentAction) Description() string {
	return fmt.Sprintf("Sync stack comment on PR #%d", a.PRNumber)
}

// Execute implements SubmissionAction.
func (a *SyncCommentAction) Execute(ctx context.Context, deps *ActionDeps) ActionResult {
	result := ActionResult{Action: a}

	prNumber := a.PRNumber
	if prNumber == 0 {
		if pr := deps.createdPRs[a.Bookmark]; pr != nil {
			prNumber = pr.Number
		} else {
			result.Skipped = true
			return result
		}
	}
	entries := updateStackEntries(a.StackEntries, deps.createdPRs)
	commentBody := github.BuildStackComment(entries, a.Bookmark, a.BaseBranch, a.MergedHistory)

	// List existing comments to find our comment
	comments, err := deps.GitHub.ListComments(ctx, deps.Owner, deps.Repo, prNumber)
	if err != nil {
		result.Error = fmt.Errorf("failed to list comments on PR #%d: %w", prNumber, err)
		return result
	}

	// Find existing jj-stacked comment
	var existingCommentID int64
	for _, comment := range comments {
		if github.IsStackComment(comment.Body) {
			existingCommentID = comment.ID
			if comment.Body == commentBody {
				result.Unchanged = true
				result.CommentID = comment.ID
				result.PRNumber = prNumber
				return result
			}
			break
		}
	}

	if existingCommentID > 0 {
		// Update existing comment
		_, err = deps.GitHub.UpdateComment(ctx, deps.Owner, deps.Repo, existingCommentID, commentBody)
		if err != nil {
			result.Error = fmt.Errorf("failed to update comment on PR #%d: %w", prNumber, err)
			return result
		}
		result.CommentID = existingCommentID
	} else {
		// Create new comment
		comment, err := deps.GitHub.CreateComment(ctx, deps.Owner, deps.Repo, prNumber, commentBody)
		if err != nil {
			result.Error = fmt.Errorf("failed to create comment on PR #%d: %w", prNumber, err)
			return result
		}
		result.CommentID = comment.ID
	}

	result.PRNumber = prNumber
	return result
}

// Ensure all action types implement SubmissionAction
var (
	_ SubmissionAction = (*PushAction)(nil)
	_ SubmissionAction = (*CreatePRAction)(nil)
	_ SubmissionAction = (*UpdateBaseAction)(nil)
	_ SubmissionAction = (*SyncCommentAction)(nil)
)
