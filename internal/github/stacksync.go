package github

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/logger"
)

// StackCommentSyncer synchronizes stack comments across all PRs in a stack.
type StackCommentSyncer struct {
	client GitHubClient
	logger *logger.Logger
}

// NewStackCommentSyncer creates a new StackCommentSyncer.
func NewStackCommentSyncer(client GitHubClient, log *logger.Logger) *StackCommentSyncer {
	if log == nil {
		log = logger.NewFromEnv()
	}
	return &StackCommentSyncer{
		client: client,
		logger: log,
	}
}

// SyncStackComments updates comments on all PRs in the stack.
// Each PR will have a comment showing the full stack with appropriate indicators.
func (s *StackCommentSyncer) SyncStackComments(
	ctx context.Context,
	owner, repo string,
	entries []StackEntry,
	baseBranch string,
) error {
	for _, entry := range entries {
		if entry.PRNumber == 0 {
			// No PR for this bookmark yet
			continue
		}

		if err := s.syncSinglePR(ctx, owner, repo, entry.PRNumber, entry.Bookmark, entries, baseBranch); err != nil {
			return fmt.Errorf("failed to sync comment on PR #%d: %w", entry.PRNumber, err)
		}
	}

	return nil
}

// syncSinglePR updates or creates the stack comment on a single PR.
func (s *StackCommentSyncer) syncSinglePR(
	ctx context.Context,
	owner, repo string,
	prNumber int,
	currentBookmark string,
	entries []StackEntry,
	baseBranch string,
) error {
	// Generate comment body
	body := BuildStackComment(entries, currentBookmark, baseBranch)

	// Look for existing jj-stacked comment
	existingComment, err := s.FindExistingComment(ctx, owner, repo, prNumber)
	if err != nil {
		return err
	}

	if existingComment != nil {
		// Update existing comment
		s.logger.Debug("updating existing stack comment",
			"pr", prNumber,
			"comment_id", existingComment.ID,
		)
		_, err = s.client.UpdateComment(ctx, owner, repo, existingComment.ID, body)
		if err != nil {
			return fmt.Errorf("failed to update comment: %w", err)
		}
	} else {
		// Create new comment
		s.logger.Debug("creating new stack comment", "pr", prNumber)
		_, err = s.client.CreateComment(ctx, owner, repo, prNumber, body)
		if err != nil {
			return fmt.Errorf("failed to create comment: %w", err)
		}
	}

	return nil
}

// FindExistingComment finds the jj-stacked comment on a PR, if any.
func (s *StackCommentSyncer) FindExistingComment(
	ctx context.Context,
	owner, repo string,
	prNumber int,
) (*Comment, error) {
	comments, err := s.client.ListComments(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}

	for _, comment := range comments {
		if IsStackComment(comment.Body) {
			return comment, nil
		}
	}

	return nil, nil
}

// GetStackInfo retrieves stack information from an existing comment on a PR.
func (s *StackCommentSyncer) GetStackInfo(
	ctx context.Context,
	owner, repo string,
	prNumber int,
) (*StackCommentData, error) {
	comment, err := s.FindExistingComment(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}

	if comment == nil {
		return nil, nil
	}

	return ParseStackComment(comment.Body)
}
