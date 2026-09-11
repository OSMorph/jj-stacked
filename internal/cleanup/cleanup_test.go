package cleanup

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/testutil"
)

type pruneGitHub struct {
	github.GitHubClient
	prs      map[string]*github.PullRequest
	branches map[string]bool
}

func (g pruneGitHub) FindPRByHeadAllStates(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	return g.prs[head], nil
}

func (g pruneGitHub) BranchExists(_ context.Context, _, _, name string) (bool, error) {
	return g.branches[name], nil
}

func TestPruneMergedPreservesNewWorkAndRemoteBookmark(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Merged feature")
	r.Write(t, "feature", "old\n")
	r.Run(t, "bookmark", "create", "merged")
	merged := r.Change(t)
	remote := t.TempDir()
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare remote: %v %s", err, output)
	}
	r.Run(t, "git", "remote", "add", "origin", remote)
	if err := r.JJ.Push(t.Context(), "origin", "merged"); err != nil {
		t.Fatal(err)
	}
	r.Run(t, "new", "-m", "New work")
	r.Write(t, "feature", "new\n")
	r.Run(t, "bookmark", "create", "reused")
	newWork := r.Change(t)
	r.Run(t, "new", "main")
	when := time.Now()
	gh := pruneGitHub{prs: map[string]*github.PullRequest{
		"merged": {Number: 1, Merged: true, MergedAt: &when, HeadSHA: merged.CommitID},
		"reused": {Number: 2, Merged: true, MergedAt: &when, HeadSHA: merged.CommitID},
	}}
	plan, err := DiscoverPrune(t.Context(), r.JJ, gh, "owner", "repo", Options{Merged: true, Remote: "origin", TrunkBranch: "main"}, when)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Bookmark != "merged" || len(plan.Warnings) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	before, err := r.JJ.ListLocalBookmarks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	after, err := r.JJ.ListLocalBookmarks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("preview changed bookmarks")
	}
	if err := Execute(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	entries, err := r.JJ.GetLog(t.Context(), jjutils.BookmarkRevset("reused"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].CommitID != newWork.CommitID {
		t.Fatal("reused bookmark changed")
	}
	entries, err = r.JJ.GetLog(t.Context(), jjutils.BookmarkRevset("merged"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("merged bookmark not forgotten")
	}
	entries, err = r.JJ.GetLog(t.Context(), merged.CommitID+" & all()", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("forget abandoned work")
	}
	remoteEntries, err := r.JJ.GetLog(t.Context(), jjutils.RemoteBookmarkRevset("merged", "origin"), 0)
	if err != nil || len(remoteEntries) != 1 || remoteEntries[0].CommitID != merged.CommitID {
		t.Fatalf("forgot remote target: %v %v", remoteEntries, err)
	}
	r.Run(t, "git", "push", "--remote", "origin", "--all")
	if output, err := exec.Command("git", "--git-dir", remote, "show-ref", "--verify", "refs/heads/merged").CombinedOutput(); err != nil {
		t.Fatalf("prune queued a remote deletion: %v %s", err, output)
	}
	if err := r.JJ.RestoreOperation(t.Context(), plan.OperationID); err != nil {
		t.Fatal(err)
	}
	entries, err = r.JJ.GetLog(t.Context(), jjutils.BookmarkRevset("merged"), 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovery failed: %v", err)
	}
}

func TestStalePruneRequiresExplicitSelectionAndPreservesWorkspace(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Old draft")
	r.Write(t, "draft", "draft\n")
	old := r.Change(t)
	r.Run(t, "new", "main", "-m", "Active workspace")
	r.Write(t, "active", "keep\n")
	active := r.Change(t)
	plan, err := DiscoverPrune(t.Context(), r.JJ, nil, "", "", Options{Stale: true, OlderThan: 24 * time.Hour}, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Revision.CommitID != old.CommitID {
		t.Fatalf("plan=%+v", plan)
	}
	if err := Execute(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	if got := r.Change(t); got.CommitID != active.CommitID {
		t.Fatal("active workspace changed")
	}
	visible, err := r.JJ.GetLog(t.Context(), "all()", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range visible {
		if entry.CommitID == old.CommitID {
			t.Fatal("stale draft remains visible")
		}
	}
}

func TestCleanupRejectsChangedPlan(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Old draft")
	old := r.Change(t)
	r.Run(t, "new", "main")
	plan, err := Begin(t.Context(), r.JJ)
	if err != nil {
		t.Fatal(err)
	}
	plan.Candidates = []Candidate{{Action: "abandon", Revision: old}}
	r.Write(t, "new-uncommitted-work", "keep\n")
	if err := Execute(t.Context(), r.JJ, plan); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected changed-plan rejection, got %v", err)
	}
}

func TestCleanupProtectsOtherWorkspaceAndImmutableHistory(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Shared ancestor")
	ancestor := r.Change(t)
	other := t.TempDir() + "/other"
	r.Run(t, "workspace", "add", "--name", "other", "-r", ancestor.CommitID, other)
	r.Run(t, "new", "main")
	plan, err := Begin(t.Context(), r.JJ)
	if err != nil {
		t.Fatal(err)
	}
	plan.Candidates = []Candidate{{Action: "abandon", Revision: ancestor}}
	if err := Validate(t.Context(), r.JJ, plan); err == nil {
		t.Fatal("allowed rewriting another workspace ancestor")
	}
	main, err := r.JJ.GetChange(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	plan.Candidates = []Candidate{{Action: "abandon", Revision: *main}}
	if err := Validate(t.Context(), r.JJ, plan); err == nil {
		t.Fatal("allowed immutable history cleanup")
	}
}

func TestDivergedKeeperPreservesContentAndDescendants(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Original")
	r.Write(t, "feature", "old\n")
	old := r.Change(t)
	r.Run(t, "describe", "-m", "Keep this version")
	r.Write(t, "feature", "kept version\n")
	keeperBeforeRebase := r.Change(t)
	r.Run(t, "new", "main", "-m", "Alternate base")
	r.Write(t, "base-feature", "alternate parent\n")
	base := r.Change(t)
	if err := r.JJ.Rebase(t.Context(), keeperBeforeRebase.CommitID, base.CommitID); err != nil {
		t.Fatal(err)
	}
	keeperEntry, err := r.JJ.GetChange(t.Context(), keeperBeforeRebase.ChangeID)
	if err != nil {
		t.Fatal(err)
	}
	keeper := *keeperEntry
	r.Run(t, "bookmark", "create", "keeper", "-r", keeper.CommitID)
	r.Run(t, "bookmark", "create", "keeper-alias", "-r", keeper.CommitID)
	// Resurrect the predecessor with an independent descendant.
	r.Run(t, "new", old.CommitID, "-m", "Child")
	r.Write(t, "child", "preserve\n")
	r.Run(t, "bookmark", "create", "child")
	r.Run(t, "new", "main")
	plan, groups, err := DiscoverDivergence(t.Context(), r.JJ)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Versions) != 2 {
		t.Fatalf("groups=%+v", groups)
	}
	if err := KeepVersion(t.Context(), r.JJ, plan, groups[0], keeper.CommitID); err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Revision.CommitID != old.CommitID {
		t.Fatalf("plan=%+v", plan)
	}
	if err := Execute(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	entries, err := r.JJ.ListDivergentChanges(t.Context())
	if err != nil || len(entries) != 0 {
		t.Fatalf("remaining divergence=%v err=%v", entries, err)
	}
	if contents := r.Run(t, "file", "show", "-r", "child", "child"); contents != "preserve\n" {
		t.Fatalf("lost child content: %q", contents)
	}
	if contents := r.Run(t, "file", "show", "-r", keeper.CommitID, "feature"); contents != "kept version\n" {
		t.Fatalf("lost keeper content: %q", contents)
	}
}

func TestPruneClosedAndMissingRemoteOnlyForgetKnownBookmarks(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Preserve local content")
	r.Write(t, "work", "keep\n")
	for _, name := range []string{"closed", "missing", "unpublished", "still-remote"} {
		r.Run(t, "bookmark", "create", name)
	}
	work := r.Change(t)
	r.Run(t, "new", "main")
	gh := pruneGitHub{prs: map[string]*github.PullRequest{
		"closed": {Number: 1, State: "closed"}, "missing": {Number: 2, State: "open"}, "still-remote": {Number: 3, State: "open"},
	}, branches: map[string]bool{"still-remote": true}}
	plan, err := DiscoverPrune(t.Context(), r.JJ, gh, "o", "r", Options{Closed: true, MissingRemote: true, Remote: "origin", TrunkBranch: "main"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 2 || plan.Candidates[0].Bookmark != "closed" || plan.Candidates[1].Bookmark != "missing" {
		t.Fatalf("plan=%+v", plan)
	}
	if err := Execute(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unpublished", "still-remote"} {
		entry, err := r.JJ.GetChange(t.Context(), jjutils.BookmarkRevset(name))
		if err != nil || entry.CommitID != work.CommitID {
			t.Fatalf("changed %s: %v", name, err)
		}
	}
	if content := r.Run(t, "file", "show", "-r", work.CommitID, "work"); content != "keep\n" {
		t.Fatalf("changed preserved content: %q", content)
	}
}

func TestStaleExplicitRangePreservesUnrelatedHead(t *testing.T) {
	r := testutil.NewRepository(t)
	var selected []jjutils.LogEntry
	for _, name := range []string{"one", "two", "three"} {
		r.Run(t, "describe", "-m", name)
		r.Write(t, name, name+"\n")
		selected = append(selected, r.Change(t))
		r.Run(t, "new")
	}
	r.Run(t, "abandon", "@")
	r.Run(t, "new", "main", "-m", "unrelated old draft")
	r.Write(t, "unrelated", "preserve\n")
	unrelated := r.Change(t)
	r.Run(t, "new", "main")
	opts := Options{Stale: true, OlderThan: 24 * time.Hour, Revision: selected[0].CommitID + "::"}
	plan, err := DiscoverPrune(t.Context(), r.JJ, nil, "", "", opts, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 3 {
		t.Fatalf("expected whole range, got %+v", plan.Candidates)
	}
	if err := Execute(t.Context(), r.JJ, plan); err != nil {
		t.Fatal(err)
	}
	entries, err := r.JJ.GetLog(t.Context(), unrelated.CommitID+" & all()", 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unrelated draft changed: %v %v", entries, err)
	}
	visible, err := r.JJ.GetLog(t.Context(), "all()", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range selected {
		for j := range visible {
			if visible[j].CommitID == selected[i].CommitID {
				t.Fatalf("selected draft still visible: %s", selected[i].CommitID)
			}
		}
	}
}
