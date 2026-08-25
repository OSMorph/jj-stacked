package sync

import (
	"context"
	"errors"
	"strings"
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
	pushCalls        []struct{ remote, bookmark string }
	abandonCalls     []string
	pushErrors       map[string]error
	listBookmarksErr error
	conflictOn       int
	conflictChecks   int
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
func (f *fakeJJ) Push(_ context.Context, remote, bookmark string, _ jjutils.PushOptions) error {
	f.pushCalls = append(f.pushCalls, struct{ remote, bookmark string }{remote: remote, bookmark: bookmark})
	return f.pushErrors[bookmark]
}

func (f *fakeJJ) ListBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	return f.bookmarks, f.listBookmarksErr
}
func (f *fakeJJ) ListLocalBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	return f.bookmarks, f.listBookmarksErr
}

func TestExecuteSyncWithState_DoesNotCheckpointMissingBookmarksWhenListingFails(t *testing.T) {
	jj := &fakeJJ{listBookmarksErr: errors.New("jj log failed")}
	plan := &SyncPlan{Remote: "origin", ToDelete: []string{"merged"}}
	state := CreateInitialState(plan, "op", "", true)

	result := ExecuteSyncWithState(context.Background(), plan, state, jj, nil)

	if result.Success || state.StepComplete("delete:merged") {
		t.Fatalf("success=%v completed=%v, want failed and incomplete", result.Success, state.CompletedSteps)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Error(), "list bookmarks") {
		t.Fatalf("errors = %v", result.Errors)
	}
}
func (f *fakeJJ) ListBookmarksForRemote(context.Context, string) ([]jjutils.Bookmark, error) {
	return f.bookmarks, nil
}
func (f *fakeJJ) ListUserBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	panic("unexpected call")
}
func (f *fakeJJ) ListUserBookmarksForBase(context.Context, string) ([]jjutils.Bookmark, error) {
	panic("unexpected call")
}
func (f *fakeJJ) GetBookmarksForChange(context.Context, string) ([]jjutils.Bookmark, error) {
	panic("unexpected call")
}
func (f *fakeJJ) DeleteBookmark(_ context.Context, name string) error {
	for i, bookmark := range f.bookmarks {
		if bookmark.Name == name {
			f.bookmarks = append(f.bookmarks[:i], f.bookmarks[i+1:]...)
			break
		}
	}
	return nil
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
func (f *fakeJJ) BuildChangeGraphForBase(context.Context, string) (*jjutils.ChangeGraph, error) {
	panic("unexpected call")
}
func (f *fakeJJ) BuildChangeGraphForBookmark(context.Context, string, string) (*jjutils.ChangeGraph, error) {
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
func (f *fakeJJ) HasConflicts(context.Context) (bool, error) {
	f.conflictChecks++
	return f.conflictOn > 0 && f.conflictChecks >= f.conflictOn, nil
}
func (f *fakeJJ) GetConflictFiles(context.Context) ([]string, error) {
	return []string{"conflicted.txt"}, nil
}
func (f *fakeJJ) IsAncestor(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeJJ) GetOperationID(context.Context) (string, error)           { return "op", nil }
func (f *fakeJJ) RestoreOperation(context.Context, string) error           { return nil }

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
		Remote:       "origin",
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
		if w == "skipping push for initial_checks: bookmark no longer exists" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected deleted-bookmark warning for initial_checks, got warnings: %v", result.Warnings)
	}
}

func TestExecuteSync_RebasesEveryIndependentRootAndUsesSelectedRemote(t *testing.T) {
	jj := &fakeJJ{bookmarks: []jjutils.Bookmark{{Name: "a"}, {Name: "b"}, {Name: "x"}}}
	plan := &SyncPlan{
		Remote: "upstream", RebaseTarget: "main@upstream", NeedsRebase: true,
		RebaseRoots: []string{"a", "x"}, ToRebase: []string{"a", "b", "x"},
		ToPush: []string{"a", "b", "x"},
	}
	result := ExecuteSync(context.Background(), plan, jj, nil)
	if !result.Success {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(jj.rebaseCalls) != 2 {
		t.Fatalf("got %d rebases, want 2", len(jj.rebaseCalls))
	}
	for _, call := range jj.pushCalls {
		if call.remote != "upstream" {
			t.Errorf("push remote = %q, want upstream", call.remote)
		}
	}
}

func TestExecuteSyncWithState_DoesNotRepeatCompletedSteps(t *testing.T) {
	jj := &fakeJJ{bookmarks: []jjutils.Bookmark{{Name: "a"}, {Name: "b"}}}
	plan := &SyncPlan{Remote: "origin", RebaseTarget: "main@origin", RebaseRoots: []string{"a"}, ToPush: []string{"a", "b"}, ToAbandon: []string{"merged"}}
	state := CreateInitialState(plan, "op", "a", true)
	state.MarkStepComplete("abandon:merged")
	state.MarkStepComplete("rebase:a")
	state.MarkStepComplete("push:a")
	result := ExecuteSyncWithState(context.Background(), plan, state, jj, nil)
	if !result.Success {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(jj.abandonCalls) != 0 || len(jj.rebaseCalls) != 0 {
		t.Fatalf("completed local steps repeated: abandon=%v rebase=%v", jj.abandonCalls, jj.rebaseCalls)
	}
	if len(jj.pushCalls) != 1 || jj.pushCalls[0].bookmark != "b" {
		t.Fatalf("push calls = %v, want only b", jj.pushCalls)
	}
}

func TestExecuteSyncWithState_PausesBeforePushOnConflict(t *testing.T) {
	jj := &fakeJJ{bookmarks: []jjutils.Bookmark{{Name: "a"}}, conflictOn: 1}
	plan := &SyncPlan{Remote: "origin", RebaseTarget: "main@origin", RebaseRoots: []string{"a"}, ToPush: []string{"a"}}
	state := CreateInitialState(plan, "op", "a", true)
	result := ExecuteSyncWithState(context.Background(), plan, state, jj, nil)
	if !result.HasConflicts || result.Success {
		t.Fatalf("got success=%v conflicts=%v", result.Success, result.HasConflicts)
	}
	if len(result.ConflictFiles) != 1 || result.ConflictFiles[0] != "conflicted.txt" {
		t.Fatalf("conflict files = %v", result.ConflictFiles)
	}
	if len(jj.pushCalls) != 0 {
		t.Fatal("push ran despite conflict")
	}
}

func TestExecuteSyncWithState_CheckpointsPartialPushes(t *testing.T) {
	jj := &fakeJJ{bookmarks: []jjutils.Bookmark{{Name: "a"}, {Name: "b"}}, pushErrors: map[string]error{"a": errors.New("rejected")}}
	plan := &SyncPlan{Remote: "origin", ToPush: []string{"a", "b"}}
	state := CreateInitialState(plan, "op", "", true)
	result := ExecuteSyncWithState(context.Background(), plan, state, jj, nil)
	if result.Success || !state.StepComplete("push:b") || state.StepComplete("push:a") {
		t.Fatalf("unexpected result/state: success=%v completed=%v", result.Success, state.CompletedSteps)
	}
}

func TestExecuteSyncWithState_DeletesMergedBookmarkAlreadyInTrunk(t *testing.T) {
	jj := &fakeJJ{bookmarks: []jjutils.Bookmark{{Name: "merged"}, {Name: "remaining"}}}
	plan := &SyncPlan{Remote: "origin", ToDelete: []string{"merged"}}
	state := CreateInitialState(plan, "op", "remaining", true)
	result := ExecuteSyncWithState(context.Background(), plan, state, jj, nil)
	if !result.Success || len(result.Deleted) != 1 || result.Deleted[0] != "merged" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !state.StepComplete("delete:merged") {
		t.Fatal("delete step was not checkpointed")
	}
}
