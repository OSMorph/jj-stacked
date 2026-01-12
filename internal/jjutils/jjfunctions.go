package jjutils

import (
	"context"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

// JJFunctions is the interface for all Jujutsu operations.
// AIDEV-NOTE: This abstraction enables dependency injection for testing.
type JJFunctions interface {
	// Repository info
	GetRepoRoot(ctx context.Context) (string, error)
	GetDefaultBranch(ctx context.Context) (string, error)
	GetTrunkInfo(ctx context.Context) (*TrunkInfo, error)

	// Remote operations
	ListRemotes(ctx context.Context) ([]Remote, error)
	Fetch(ctx context.Context, remote string) error
	FetchAllRemotes(ctx context.Context) error
	Push(ctx context.Context, remote, bookmark string) error

	// Bookmark operations
	ListBookmarks(ctx context.Context) ([]Bookmark, error)
	ListUserBookmarks(ctx context.Context) ([]Bookmark, error)
	GetBookmarksForChange(ctx context.Context, changeID string) ([]Bookmark, error)

	// Log/History operations
	GetLog(ctx context.Context, revset string, limit int) ([]LogEntry, error)
	GetChange(ctx context.Context, changeID string) (*LogEntry, error)
	GetChangesInRange(ctx context.Context, from, to string) ([]LogEntry, error)

	// Graph building
	BuildChangeGraph(ctx context.Context) (*ChangeGraph, error)

	// Mutation operations (for sync command)
	Abandon(ctx context.Context, revset string) error
	Rebase(ctx context.Context, source, destination string) error
	HasConflicts(ctx context.Context) (bool, error)
}

// jjFunctions is the real implementation of JJFunctions.
type jjFunctions struct {
	exec   cmdexec.CommandExecutor
	jjPath string
}

// NewJJFunctions creates a new JJFunctions implementation.
// If jjPath is empty, "jj" will be used (found via PATH).
func NewJJFunctions(exec cmdexec.CommandExecutor, jjPath string) JJFunctions {
	if jjPath == "" {
		jjPath = "jj"
	}
	return &jjFunctions{
		exec:   exec,
		jjPath: jjPath,
	}
}

// jjCmd returns the jj binary path.
func (j *jjFunctions) jjCmd() string {
	return j.jjPath
}
