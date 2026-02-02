package sync

import (
	"context"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type fakeJJ struct {
	bookmarks []jjutils.Bookmark

	fetchCalls  int
	rebaseCalls []struct {
		source      string
		destination string
	}
	pushCalls    []struct{ remote, bookmark string }
	abandonCalls []string
}

func (f *fakeJJ) GetRepoRoot(context.Context) (string, error)              { panic("unexpected call") }
func (f *fakeJJ) GetDefaultBranch(context.Context) (string, error)         { panic("unexpected call") }
func (f *fakeJJ) GetTrunkInfo(context.Context) (*jjutils.TrunkInfo, error) { panic("unexpected call") }

func (f *fakeJJ) ListRemotes(context.Context) ([]jjutils.Remote, error) { panic("unexpected call") }
func (f *fakeJJ) Fetch(context.Context, string) error                   { panic("unexpected call") }
func (f *fakeJJ) FetchAllRemotes(context.Context) error {
	f.fetchCalls++
	return nil
}
func (f *fakeJJ) Push(_ context.Context, remote, bookmark string) error {
	f.pushCalls = append(f.pushCalls, struct{ remote, bookmark string }{remote: remote, bookmark: bookmark})
	return nil
}

func (f *fakeJJ) ListBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	return f.bookmarks, nil
}
func (f *fakeJJ) ListUserBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	panic("unexpected call")
}
func (f *fakeJJ) GetBookmarksForChange(context.Context, string) ([]jjutils.Bookmark, error) {
	panic("unexpected call")
}

func (f *fakeJJ) GetLog(context.Context, string, int) ([]jjutils.LogEntry, error) {
	panic("unexpected call")
}
func (f *fakeJJ) GetChange(context.Context, string) (*jjutils.LogEntry, error) {
	panic("unexpected call")
}
func (f *fakeJJ) GetChangesInRange(context.Context, string, string) ([]jjutils.LogEntry, error) {
	panic("unexpected call")
}

func (f *fakeJJ) BuildChangeGraph(context.Context) (*jjutils.ChangeGraph, error) {
	panic("unexpected call")
}

func (f *fakeJJ) Abandon(_ context.Context, revset string) error {
	f.abandonCalls = append(f.abandonCalls, revset)
	return nil
}
func (f *fakeJJ) Rebase(_ context.Context, source, destination string) error {
	f.rebaseCalls = append(f.rebaseCalls, struct {
		source      string
		destination string
	}{source: source, destination: destination})
	return nil
}
func (f *fakeJJ) HasConflicts(context.Context) (bool, error) { return false, nil }

func TestExecuteSync_SkipsPushForDeletedBookmark(t *testing.T) {
	ctx := context.Background()

	jj := &fakeJJ{
		// "initial_checks" is missing (e.g., deleted during fetch), others still exist.
		bookmarks: []jjutils.Bookmark{
			{Name: "additional_checks"},
			{Name: "country_code_conversion"},
			{Name: "create_billing_sync_return_type"},
			{Name: "test_sync_checks"},
		},
	}

	plan := &SyncPlan{
		RebaseTarget: "main@origin",
		NeedsRebase:  true,
		ToRebase: []string{
			"additional_checks",
			"country_code_conversion",
			"create_billing_sync_return_type",
			"initial_checks", // deleted
			"test_sync_checks",
		},
		ToPush: []string{
			"initial_checks", // planned push, but deleted by fetch
		},
	}

	result := ExecuteSync(ctx, plan, jj, nil)

	if !result.Success {
		t.Fatalf("expected success, got errors: %v", result.Errors)
	}

	for _, call := range jj.pushCalls {
		if call.bookmark == "initial_checks" {
			t.Fatalf("expected initial_checks to be skipped, but it was pushed")
		}
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if w == "bookmark initial_checks was deleted during fetch (remote branch likely deleted after PR merge)" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected deleted-during-fetch warning for initial_checks, got warnings: %v", result.Warnings)
	}
}
