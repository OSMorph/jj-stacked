package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type syncFixture struct {
	t           *testing.T
	ctx         context.Context
	dir, remote string
	exec        *cmdexec.RealExecutor
	jj          jjutils.JJFunctions
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("real jj repository")
	}
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	f := &syncFixture{t: t, ctx: t.Context(), dir: t.TempDir(), remote: filepath.Join(t.TempDir(), "remote.git")}
	f.exec = cmdexec.NewRealExecutorInDir(f.dir)
	f.jj = jjutils.NewJJFunctions(f.exec, "")
	f.run("git", "init", "--colocate")
	f.run("config", "set", "--repo", "user.name", "Fixture User")
	f.run("config", "set", "--repo", "user.email", "fixture@example.com")
	f.write("shared.txt", "base\n")
	f.run("describe", "-m", "base")
	f.run("bookmark", "create", "main")
	if _, err := f.exec.Run(f.ctx, "git", "init", "--bare", f.remote); err != nil {
		t.Fatal(err)
	}
	f.run("git", "remote", "add", "origin", f.remote)
	f.push("main")
	return f
}
func (f *syncFixture) run(args ...string) string {
	f.t.Helper()
	out, err := f.exec.Run(f.ctx, "jj", args...)
	if err != nil {
		f.t.Fatalf("jj %v: %v", args, err)
	}
	return out
}
func (f *syncFixture) write(name, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *syncFixture) change(base, description, body, bookmark string) *jjutils.LogEntry {
	f.t.Helper()
	f.run("new", base, "-m", description)
	f.write("shared.txt", body)
	if bookmark != "" {
		f.run("bookmark", "create", bookmark)
	}
	return f.get("@")
}
func (f *syncFixture) get(revision string) *jjutils.LogEntry {
	f.t.Helper()
	entry, err := f.jj.GetChange(f.ctx, revision)
	if err != nil {
		f.t.Fatal(err)
	}
	return entry
}
func (f *syncFixture) push(bookmark string) {
	f.t.Helper()
	if err := f.jj.Push(f.ctx, "origin", bookmark); err != nil {
		f.t.Fatal(err)
	}
}
func (f *syncFixture) moveMain() {
	f.t.Helper()
	f.run("bookmark", "set", "main", "-r", "@")
	f.push("main")
}
func (f *syncFixture) remoteHead(bookmark string) string {
	f.t.Helper()
	head, err := f.exec.Run(f.ctx, "git", "--git-dir", f.remote, "rev-parse", "refs/heads/"+bookmark)
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(head)
}
func (f *syncFixture) plan(anchor string, prs map[string]*github.PullRequest) *SyncPlan {
	f.t.Helper()
	analysis, err := AnalyzeSyncWithOptions(f.ctx, f.jj, &mergedGitHub{prs: prs}, "o", "r", AnalyzeOptions{Bookmark: anchor, Remote: "origin", TrunkBranch: "main"})
	if err != nil {
		f.t.Fatal(err)
	}
	plan, err := CreateSyncPlan(analysis)
	if err != nil {
		f.t.Fatalf("plan: %v; analysis=%+v", err, analysis)
	}
	return plan
}
func mergedPR(head *jjutils.LogEntry, name, base, mergeCommit string) *github.PullRequest {
	now := time.Now()
	return &github.PullRequest{Number: 1, Head: name, Base: base, HeadSHA: head.CommitID, MergeCommitSHA: mergeCommit, Merged: true, MergedAt: &now}
}

func TestSyncSameFileDependenciesFinishRebaseBeforeCheckingFinalConflicts(t *testing.T) {
	for _, scenario := range []string{"squash", "rebase", "resume after abandon", "final conflict"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSyncFixture(t)
			f.change("main", "A first", "A first\n", "")
			a := f.change("@", "A final", "A\n", "a")
			b := f.change("a", "B depends on A", "B\n", "b")
			f.push("a")
			f.push("b")
			if scenario == "rebase" {
				f.change("main", "upstream A first", "A first\n", "")
				f.change("@", "upstream A final", "A\n", "")
			} else {
				f.change("main", "upstream squash A", "A\n", "")
			}
			f.moveMain()
			landed := f.get("main@origin")
			if scenario == "final conflict" {
				f.change("main", "upstream edit after merge", "upstream\n", "")
				f.moveMain()
			}
			f.run("new", "b", "-m", "active workspace")
			plan := f.plan("a", map[string]*github.PullRequest{"a": mergedPR(a, "a", "main", landed.CommitID)})
			op, err := f.jj.GetOperationID(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			state := CreateInitialState(plan, op, "a", true)
			var result *SyncResult
			if scenario == "resume after abandon" {
				result = ExecuteSyncWithState(f.ctx, plan, state, f.jj, &SyncCallbacks{OnCheckpoint: func(state *SyncState) error {
					if err := SaveSyncState(f.ctx, f.jj, state); err != nil {
						return err
					}
					if state.Phase == "abandon" {
						return errors.New("simulate interruption")
					}
					return nil
				}})
				if result.Success || !state.StepComplete("abandon:a") || state.StepComplete("rebase:b") {
					t.Fatalf("wrong interruption: %+v %+v", result, state)
				}
				conflicts, err := f.jj.HasConflicts(f.ctx)
				if err != nil || !conflicts {
					t.Fatalf("fixture did not expose transient conflict: %v %v", conflicts, err)
				}
				state, err = LoadSyncState(f.ctx, f.jj)
				if err != nil {
					t.Fatal(err)
				}
				if err := ValidateCanContinue(f.ctx, f.jj, state); err != nil {
					t.Fatalf("pending rebase refused: %v", err)
				}
			}
			result = ExecuteSyncWithState(f.ctx, state.Plan, state, f.jj, nil)
			if scenario == "final conflict" {
				if result.Success || !result.HasConflicts || len(result.Pushed) > 0 || f.remoteHead("b") != b.CommitID {
					t.Fatalf("final conflict pushed work: %+v", result)
				}
				if err := ValidateCanContinue(f.ctx, f.jj, state); err == nil {
					t.Fatal("final conflict allowed continuation")
				}
				return
			}
			if !result.Success || result.HasConflicts {
				t.Fatalf("sync failed: %+v", result)
			}
			if got := f.run("file", "show", "-r", "b", "shared.txt"); got != "B\n" {
				t.Fatalf("B content lost: %q", got)
			}
			if based, err := f.jj.IsAncestor(f.ctx, "main@origin", "b"); err != nil || !based {
				t.Fatalf("B not rebased: %v %v", based, err)
			}
			if f.remoteHead("a") != a.CommitID {
				t.Fatal("merged remote branch changed")
			}
		})
	}
}

func TestSyncInTrunkAnchorPreservesSurvivingScope(t *testing.T) {
	for _, style := range []string{"fast-forward", "normal merge"} {
		for _, resume := range []bool{false, true} {
			t.Run(style+map[bool]string{false: " initial", true: " resume"}[resume], func(t *testing.T) {
				f := newSyncFixture(t)
				x := f.change("main", "unrelated branch", "X\n", "unrelated")
				f.push("unrelated")
				a := f.change("main", "A", "A\n", "a")
				f.change("a", "B", "B\n", "b")
				f.push("a")
				f.push("b")
				if style == "fast-forward" {
					f.run("bookmark", "set", "main", "-r", "a")
					f.push("main")
				} else {
					f.run("new", "main", "-m", "independent trunk commit")
					f.write("trunk.txt", "trunk\n")
					f.moveMain()
					f.run("new", "main", "a", "-m", "merge A into main")
					f.moveMain()
				}
				landed := f.get("main@origin")
				f.run("new", "b", "-m", "active workspace")
				plan := f.plan("a", map[string]*github.PullRequest{"a": mergedPR(a, "a", "main", landed.CommitID)})
				if !reflect.DeepEqual(plan.RefreshBookmarks, []string{"b"}) || !reflect.DeepEqual(plan.ToDelete, []string{"a"}) {
					t.Fatalf("lost selected survivors: %+v", plan)
				}
				state := CreateInitialState(plan, "op", "a", false)
				if resume {
					result := ExecuteSyncWithState(f.ctx, plan, state, f.jj, &SyncCallbacks{OnCheckpoint: func(state *SyncState) error {
						if err := SaveSyncState(f.ctx, f.jj, state); err != nil {
							return err
						}
						if state.Phase == "delete" {
							return errors.New("simulate interruption")
						}
						return nil
					}})
					if result.Success || !state.StepComplete("delete:a") {
						t.Fatalf("wrong checkpoint: %+v", result)
					}
					var err error
					state, err = LoadSyncState(f.ctx, f.jj)
					if err != nil {
						t.Fatal(err)
					}
					if err := ValidateCanContinue(f.ctx, f.jj, state); err != nil {
						t.Fatal(err)
					}
				}
				result := ExecuteSyncWithState(f.ctx, state.Plan, state, f.jj, nil)
				if !result.Success {
					t.Fatalf("sync: %+v", result)
				}
				selection, err := state.RefreshSelection()
				if err != nil || !reflect.DeepEqual(selection, []string{"b"}) {
					t.Fatalf("refresh selection=%v error=%v", selection, err)
				}
				if f.get("unrelated").CommitID != x.CommitID || f.remoteHead("unrelated") != x.CommitID {
					t.Fatal("unrelated branch was changed")
				}
				for _, name := range result.Pushed {
					if name != "b" {
						t.Fatalf("pushed out-of-scope bookmark %s", name)
					}
				}
			})
		}
	}
}

func TestSyncRequiresMergedDestinationProof(t *testing.T) {
	for _, scenario := range []string{"missing base bookmark", "older base merge", "unknown PR base", "unfetched merge"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSyncFixture(t)
			a := f.change("main", "A", "A\n", "a")
			b := f.change("a", "B", "B\n", "b")
			f.change("b", "C", "C\n", "c")
			f.run("new", "c", "-m", "active workspace")
			prs := map[string]*github.PullRequest{"b": mergedPR(b, "b", "a", b.CommitID)}
			switch scenario {
			case "missing base bookmark":
				if err := f.jj.ForgetBookmark(f.ctx, "a"); err != nil {
					t.Fatal(err)
				}
			case "older base merge":
				f.change("main", "old merge of A", "A\n", "")
				f.moveMain()
				prs["a"] = mergedPR(a, "a", "main", f.get("main@origin").CommitID)
				f.run("new", "c", "-m", "active workspace")
			case "unknown PR base":
				prs["b"].Base = ""
			case "unfetched merge":
				prs["b"].Base = "main"
			}
			analysis, err := AnalyzeSyncWithOptions(f.ctx, f.jj, &mergedGitHub{prs: prs}, "o", "r", AnalyzeOptions{Bookmark: "b", Remote: "origin", TrunkBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CreateSyncPlan(analysis); err == nil {
				t.Fatalf("unproven destination accepted: %+v", analysis)
			}
			if f.get("b").CommitID != b.CommitID {
				t.Fatal("unproven work changed")
			}
		})
	}
}

func TestSyncAcceptsDependencyIncludedInProvenMergedHead(t *testing.T) {
	f := newSyncFixture(t)
	b := f.change("main", "B contribution", "B\n", "b")
	a := f.change("b", "A includes merged B", "A\n", "a")
	f.change("a", "C active", "C\n", "c")
	f.push("a")
	f.push("b")
	f.push("c")
	f.change("main", "squash A including B", "A\n", "")
	f.moveMain()
	landed := f.get("main@origin")
	f.run("new", "c", "-m", "active workspace")
	plan := f.plan("b", map[string]*github.PullRequest{"b": mergedPR(b, "b", "a", a.CommitID), "a": mergedPR(a, "a", "main", landed.CommitID)})
	if len(plan.ToAbandon) != 2 || plan.CleanupProofs["b"] != landed.CommitID {
		t.Fatalf("proven dependency not accepted: %+v", plan)
	}
	result := ExecuteSync(f.ctx, plan, f.jj, nil)
	if !result.Success {
		t.Fatalf("sync failed: %+v", result)
	}
	if got := f.run("file", "show", "-r", "c", "shared.txt"); got != "C\n" {
		t.Fatalf("C content changed: %q", got)
	}
}

func TestSyncRecoversSurvivorsAcrossSeveralIncorporatedBookmarks(t *testing.T) {
	for _, incorporatedCount := range []int{2, 3} {
		for _, style := range []string{"fast-forward", "normal merge"} {
			for _, resume := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d incorporated %s resume=%v", incorporatedCount, style, resume), func(t *testing.T) {
					f := newSyncFixture(t)
					oldUnrelated := f.change("main", "old unrelated stack", "old unrelated\n", "old-unrelated")
					f.push("old-unrelated")
					names := []string{"a", "b", "c", "d", "e"}
					heads := make(map[string]*jjutils.LogEntry)
					parent := "main"
					for _, name := range names[:incorporatedCount+2] {
						heads[name] = f.change(parent, "stack "+name, name+"\n", name)
						f.push(name)
						parent = name
					}
					lastIncorporated := names[incorporatedCount-1]
					if style == "fast-forward" {
						f.run("bookmark", "set", "main", "-r", lastIncorporated)
						f.push("main")
					} else {
						f.run("new", "main", "-m", "independent trunk commit")
						f.write("trunk.txt", "trunk\n")
						f.moveMain()
						f.run("new", "main", lastIncorporated, "-m", "merge several stack bookmarks")
						f.moveMain()
					}
					landed := f.get("main@origin")
					// New independent stacks from later trunk are descendants of A,
					// but their boundary is not an incorporated feature bookmark.
					f.run("new", "main", "-m", "later trunk change")
					f.write("later.txt", "later\n")
					f.moveMain()
					newUnrelated := f.change("main", "new unrelated stack", "new unrelated\n", "new-unrelated")
					f.push("new-unrelated")
					survivors := names[incorporatedCount : incorporatedCount+2]
					f.run("new", survivors[1], "-m", "active workspace")
					prs := make(map[string]*github.PullRequest)
					for _, name := range names[:incorporatedCount] {
						prs[name] = mergedPR(heads[name], name, "main", landed.CommitID)
					}
					plan := f.plan("a", prs)
					if !reflect.DeepEqual(plan.RefreshBookmarks, survivors) || !reflect.DeepEqual(plan.ToDelete, names[:incorporatedCount]) {
						t.Fatalf("incorporated lineage lost: plan=%+v", plan)
					}
					op, err := f.jj.GetOperationID(f.ctx)
					if err != nil {
						t.Fatal(err)
					}
					state := CreateInitialState(plan, op, "a", false)
					if resume {
						result := ExecuteSyncWithState(f.ctx, plan, state, f.jj, &SyncCallbacks{OnCheckpoint: func(state *SyncState) error {
							if err := SaveSyncState(f.ctx, f.jj, state); err != nil {
								return err
							}
							if state.StepComplete("delete:" + lastIncorporated) {
								return errors.New("simulate interruption after incorporated cleanup")
							}
							return nil
						}})
						if result.Success || !state.StepComplete("delete:"+lastIncorporated) {
							t.Fatalf("wrong checkpoint: %+v", result)
						}
						state, err = LoadSyncState(f.ctx, f.jj)
						if err != nil {
							t.Fatal(err)
						}
						if err := ValidateCanContinue(f.ctx, f.jj, state); err != nil {
							t.Fatal(err)
						}
					}
					result := ExecuteSyncWithState(f.ctx, state.Plan, state, f.jj, nil)
					if !result.Success {
						t.Fatalf("sync: %+v", result)
					}
					selection, err := state.RefreshSelection()
					if err != nil || !reflect.DeepEqual(selection, survivors) {
						t.Fatalf("refresh scope=%v error=%v", selection, err)
					}
					if f.get("old-unrelated").CommitID != oldUnrelated.CommitID || f.remoteHead("old-unrelated") != oldUnrelated.CommitID || f.get("new-unrelated").CommitID != newUnrelated.CommitID || f.remoteHead("new-unrelated") != newUnrelated.CommitID {
						t.Fatal("an unrelated local or remote stack changed")
					}
					for _, name := range result.Pushed {
						if !contains(survivors, name) {
							t.Fatalf("pushed out-of-scope bookmark %s", name)
						}
					}
					for _, name := range names[:incorporatedCount] {
						if f.remoteHead(name) != heads[name].CommitID {
							t.Fatalf("merged remote branch %s changed", name)
						}
					}
					if got := f.run("file", "show", "-r", survivors[1], "shared.txt"); got != survivors[1]+"\n" {
						t.Fatalf("surviving content changed: %q", got)
					}
				})
			}
		}
	}
}
