package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type mergedGitHub struct {
	github.GitHubClient
	prs map[string]*github.PullRequest
}

func (g *mergedGitHub) FindPRByHeadAllStates(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	return g.prs[head], nil
}

func TestSyncMergedMultiChangeSegmentsPreservesActiveDescendants(t *testing.T) {
	if testing.Short() {
		t.Skip("real jj repository")
	}
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	for _, mergeStyle := range []string{"squash", "rebase", "anonymous child", "other workspace"} {
		t.Run(mergeStyle, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			executor := cmdexec.NewRealExecutorInDir(dir)
			jj := jjutils.NewJJFunctions(executor, "")
			run := func(args ...string) string {
				t.Helper()
				out, err := executor.Run(ctx, "jj", args...)
				if err != nil {
					t.Fatalf("jj %v: %v", args, err)
				}
				return out
			}
			run("git", "init", "--colocate")
			run("config", "set", "--repo", "user.name", "Fixture User")
			run("config", "set", "--repo", "user.email", "fixture@example.com")
			write := func(name string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("base.txt")
			run("describe", "-m", "base")
			run("bookmark", "create", "main")
			bare := filepath.Join(t.TempDir(), "remote.git")
			if _, err := executor.Run(ctx, "git", "init", "--bare", bare); err != nil {
				t.Fatal(err)
			}
			run("git", "remote", "add", "origin", bare)
			if err := jj.Push(ctx, "origin", "main"); err != nil {
				t.Fatal(err)
			}
			run("new", "main", "-m", "A first")
			write("a1.txt")
			run("new", "-m", "A second")
			write("a2.txt")
			run("bookmark", "create", "a")
			run("new", "-m", "B first")
			write("b1.txt")
			run("new", "-m", "B second")
			write("b2.txt")
			run("bookmark", "create", "b")
			run("new", "-m", "working")
			a, err := jj.GetChange(ctx, "a")
			if err != nil {
				t.Fatal(err)
			}
			if err := jj.Push(ctx, "origin", "a"); err != nil {
				t.Fatal(err)
			}
			if err := jj.Push(ctx, "origin", "b"); err != nil {
				t.Fatal(err)
			}
			switch mergeStyle {
			case "anonymous child":
				run("new", "a", "-m", "anonymous child")
				write("anonymous.txt")
			case "other workspace":
				run("workspace", "add", "-r", "a", filepath.Join(t.TempDir(), "other-workspace"))
			}
			// Simulate GitHub's new trunk commits: original PR head IDs remain local.
			run("new", "main", "-m", "upstream merged A")
			write("a1.txt")
			if mergeStyle == "rebase" {
				run("new", "-m", "upstream A second")
			}
			write("a2.txt")
			run("bookmark", "set", "main", "-r", "@")
			if err := jj.Push(ctx, "origin", "main"); err != nil {
				t.Fatal(err)
			}
			run("new", "b", "-m", "active workspace")
			now := time.Now()
			landed, err := jj.GetChange(ctx, "main@origin")
			if err != nil {
				t.Fatal(err)
			}
			gh := &mergedGitHub{prs: map[string]*github.PullRequest{"a": {Number: 1, Head: "a", Base: "main", HeadSHA: a.CommitID, MergeCommitSHA: landed.CommitID, Merged: true, MergedAt: &now}}}
			analysis, err := AnalyzeSyncWithOptions(ctx, jj, gh, "o", "r", AnalyzeOptions{Bookmark: "a", Remote: "origin", TrunkBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := CreateSyncPlan(analysis)
			if err != nil {
				t.Fatalf("plan: %v; analysis %+v", err, analysis)
			}
			if len(plan.AbandonCommits) != 2 || len(plan.RefreshBookmarks) != 1 || plan.RefreshBookmarks[0] != "b" {
				t.Fatalf("unexpected plan: %+v", plan)
			}
			state := CreateInitialState(plan, "op", "a", true)
			result := ExecuteSyncWithState(ctx, plan, state, jj, nil)
			if mergeStyle == "anonymous child" || mergeStyle == "other workspace" {
				if result.Success || len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Error(), "descendants outside") {
					t.Fatalf("unsafe descendant cleanup: %+v", result)
				}
				preserved, err := jj.GetChange(ctx, "a")
				if err != nil || preserved.CommitID != a.CommitID {
					t.Fatalf("merged work changed despite blocked cleanup: %v %+v", err, preserved)
				}
				return
			}
			if !result.Success {
				t.Fatalf("sync result: %+v; completed: %v", result, state.CompletedSteps)
			}
			for _, name := range []string{"a1.txt", "a2.txt", "b1.txt", "b2.txt"} {
				if got := run("file", "show", "-r", "b", name); got != name+"\n" {
					t.Fatalf("lost content %s: %q", name, got)
				}
			}
			remaining, err := jj.ListBookmarks(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, bm := range remaining {
				if bm.Name == "a" {
					t.Fatal("merged bookmark a remained")
				}
			}
			based, err := jj.IsAncestor(ctx, "main@origin", "b")
			if err != nil || !based {
				t.Fatalf("active branch not rebased: %v %v", based, err)
			}
			active, err := jj.GetLog(ctx, "main@origin..b", 0)
			if err != nil {
				t.Fatal(err)
			}
			remoteHead, err := executor.Run(ctx, "git", "--git-dir", bare, "rev-parse", "refs/heads/a")
			if err != nil || strings.TrimSpace(remoteHead) != a.CommitID {
				t.Fatalf("merged remote branch changed: %q %v", remoteHead, err)
			}
			tracking, err := jj.ListBookmarksForRemote(ctx, "origin")
			if err != nil {
				t.Fatal(err)
			}
			for _, bookmark := range tracking {
				if bookmark.Name == "a" {
					t.Fatal("forgotten merged bookmark remained in local tracking list")
				}
			}
			if len(active) != 2 {
				t.Fatalf("active segment has %d changes, want both B changes", len(active))
			}
		})
	}
}
