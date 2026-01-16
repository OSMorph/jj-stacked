package sync

import (
	"time"
)

// AIDEV-NOTE: These types support the three-phase sync architecture:
// 1. Analysis - detect what needs to be synced
// 2. Planning - determine what actions to take
// 3. Execution - perform the sync operations

// SyncAnalysis is the result of analyzing what needs to be synced.
// It captures the current state and identifies merged bookmarks.
type SyncAnalysis struct {
	// MergedBookmarks are bookmarks whose PRs have been merged on GitHub
	MergedBookmarks []MergedBookmark

	// RemainingBookmarks are bookmarks that remain after sync (not merged)
	RemainingBookmarks []string

	// BookmarksNeedingPush are bookmarks that are ahead of origin and need pushing
	BookmarksNeedingPush []string

	// TrunkBranch is the name of the trunk branch (main, master, trunk, etc.)
	TrunkBranch string

	// TrunkChangeID is the change ID of the current trunk commit
	TrunkChangeID string

	// Warnings are non-fatal issues discovered during analysis
	Warnings []string

	// Errors are issues that prevent sync from proceeding
	Errors []error
}

// MergedBookmark represents a bookmark whose PR has been merged.
type MergedBookmark struct {
	// Name is the bookmark name
	Name string

	// ChangeID is the jj change ID for this bookmark
	ChangeID string

	// CommitID is the git commit ID for this bookmark
	CommitID string

	// PRNumber is the GitHub PR number
	PRNumber int

	// PRTitle is the title of the merged PR
	PRTitle string

	// MergedAt is when the PR was merged
	MergedAt time.Time

	// MergedBy is the username who merged the PR
	MergedBy string
}

// SyncPlan describes what actions will be taken during sync.
type SyncPlan struct {
	// ToPush lists bookmarks to push to remote (ahead of origin)
	ToPush []string

	// ToAbandon lists bookmarks to abandon (in order, bottom-up from trunk)
	ToAbandon []string

	// NeedsRebase indicates whether remaining bookmarks need rebasing
	NeedsRebase bool

	// RebaseTarget is the target for rebase (trunk branch name or revset)
	RebaseTarget string

	// ToRebase lists bookmarks that will be rebased onto the new trunk
	ToRebase []string

	// Summary provides a human-readable summary of the plan
	Summary SyncSummary

	// Analysis is the original analysis this plan is based on
	Analysis *SyncAnalysis
}

// SyncSummary provides counts and info for display.
type SyncSummary struct {
	// PushCount is the number of bookmarks to push
	PushCount int

	// MergedCount is the number of PRs that were merged
	MergedCount int

	// AbandonCount is the number of bookmarks to abandon
	AbandonCount int

	// RebaseCount is the number of bookmarks to rebase
	RebaseCount int

	// TrunkBranch is the name of the trunk branch
	TrunkBranch string
}

// SyncResult is the outcome of executing a sync operation.
type SyncResult struct {
	// Pushed lists bookmarks that were successfully pushed
	Pushed []string

	// Abandoned lists bookmarks that were successfully abandoned
	Abandoned []string

	// Rebased lists bookmarks that were successfully rebased
	Rebased []string

	// Errors are any errors that occurred during execution
	Errors []error

	// Success indicates whether the sync completed successfully
	Success bool

	// HasConflicts indicates whether rebase resulted in conflicts
	HasConflicts bool

	// ConflictFiles lists files with conflicts (if HasConflicts is true)
	ConflictFiles []string
}

// SyncCallbacks allows callers to receive progress updates during sync.
type SyncCallbacks struct {
	// OnPushStart is called when a bookmark push begins
	OnPushStart func(bookmark string)

	// OnPushComplete is called when a bookmark push completes
	OnPushComplete func(bookmark string, err error)

	// OnFetchStart is called when fetch begins
	OnFetchStart func()

	// OnFetchComplete is called when fetch completes
	OnFetchComplete func(err error)

	// OnAbandon is called when a bookmark is about to be abandoned
	OnAbandon func(bookmark string)

	// OnAbandonComplete is called when a bookmark has been abandoned
	OnAbandonComplete func(bookmark string, err error)

	// OnRebaseStart is called when rebase begins
	OnRebaseStart func(bookmarks []string)

	// OnRebaseComplete is called when rebase completes
	OnRebaseComplete func(err error)
}

// CanProceed returns true if the analysis found no blocking errors.
func (a *SyncAnalysis) CanProceed() bool {
	return len(a.Errors) == 0
}

// HasMergedBookmarks returns true if any bookmarks have been merged.
func (a *SyncAnalysis) HasMergedBookmarks() bool {
	return len(a.MergedBookmarks) > 0
}

// IsEmpty returns true if there's nothing to sync.
func (p *SyncPlan) IsEmpty() bool {
	return len(p.ToPush) == 0 && len(p.ToAbandon) == 0 && !p.NeedsRebase
}

// HasBookmarksToPush returns true if any bookmarks need to be pushed.
func (a *SyncAnalysis) HasBookmarksToPush() bool {
	return len(a.BookmarksNeedingPush) > 0
}

// SyncState tracks an in-progress sync operation.
// This is persisted to disk to support --continue and --abort flags.
type SyncState struct {
	// StartedAt is when the sync operation began
	StartedAt time.Time `json:"started_at"`

	// Plan is the sync plan being executed
	Plan *SyncPlan `json:"plan"`

	// CompletedSteps lists steps that have been completed
	CompletedSteps []string `json:"completed_steps"`

	// PendingSteps lists steps that remain to be done
	PendingSteps []string `json:"pending_steps"`

	// ConflictFiles lists files with conflicts (if sync paused for conflicts)
	ConflictFiles []string `json:"conflict_files,omitempty"`

	// Phase indicates the current phase (fetch, abandon, rebase)
	Phase string `json:"phase"`
}
