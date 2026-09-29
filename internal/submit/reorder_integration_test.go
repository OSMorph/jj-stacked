package submit

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/testutil"
)

type reorderGitHub struct {
	github.GitHubClient
	remote   string
	prs      map[string]*github.PullRequest
	comments map[int][]*github.Comment
	events   []string
}

func (g *reorderGitHub) FindPRByHead(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	pr := g.prs[head]
	if pr == nil || pr.State != "open" {
		return nil, nil
	}
	return pr, nil
}

func (g *reorderGitHub) FindPRByHeadAllStates(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	return g.prs[head], nil
}

func (g *reorderGitHub) GetPullRequest(_ context.Context, _, _ string, number int) (*github.PullRequest, error) {
	for _, pr := range g.prs {
		if pr.Number == number {
			return pr, nil
		}
	}
	return nil, nil
}

func (g *reorderGitHub) CreatePullRequest(_ context.Context, _, _ string, req *github.CreatePRRequest) (*github.PullRequest, error) {
	return nil, fmt.Errorf("unexpected PR creation for %s", req.Head)
}

func (g *reorderGitHub) UpdatePullRequest(_ context.Context, _, _ string, number int, req *github.UpdatePRRequest) (*github.PullRequest, error) {
	for _, pr := range g.prs {
		if pr.Number == number {
			if req.Base != nil {
				pr.Base = *req.Base
				g.events = append(g.events, fmt.Sprintf("base:%s:%s", pr.Head, pr.Base))
			}
			return pr, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", number)
}

func (g *reorderGitHub) ListComments(_ context.Context, _, _ string, number int) ([]*github.Comment, error) {
	return g.comments[number], nil
}

func (g *reorderGitHub) CreateComment(_ context.Context, _, _ string, number int, body string) (*github.Comment, error) {
	comment := &github.Comment{ID: int64(number), Body: body}
	g.comments[number] = []*github.Comment{comment}
	g.events = append(g.events, fmt.Sprintf("comment:%d", number))
	return comment, nil
}

func (g *reorderGitHub) UpdateComment(_ context.Context, _, _ string, id int64, body string) (*github.Comment, error) {
	comment := &github.Comment{ID: id, Body: body}
	g.comments[int(id)] = []*github.Comment{comment}
	g.events = append(g.events, fmt.Sprintf("comment:%d", id))
	return comment, nil
}

func (g *reorderGitHub) evaluateIndirectMerges(t *testing.T) {
	t.Helper()
	var merged []string
	for head, pr := range g.prs {
		if pr.State != "open" || !gitRefExists(g.remote, head) || !gitRefExists(g.remote, pr.Base) {
			continue
		}
		command := exec.Command("git", "--git-dir", g.remote, "merge-base", "--is-ancestor", "refs/heads/"+head, "refs/heads/"+pr.Base)
		if err := command.Run(); err == nil {
			pr.State = "closed"
			pr.Merged = true
			merged = append(merged, head)
		} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			t.Fatalf("check ancestry for %s -> %s: %v", head, pr.Base, err)
		}
	}
	for _, head := range merged {
		command := exec.Command("git", "--git-dir", g.remote, "update-ref", "-d", "refs/heads/"+head)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("delete automatically merged branch %s: %v %s", head, err, output)
		}
		g.events = append(g.events, "auto-merged:"+head)
	}
}

type reorderJJ struct {
	jjutils.JJFunctions
	t  *testing.T
	gh *reorderGitHub
}

func (j *reorderJJ) Push(ctx context.Context, remote, bookmark string) error {
	if err := j.JJFunctions.Push(ctx, remote, bookmark); err != nil {
		return err
	}
	j.gh.events = append(j.gh.events, "push:"+bookmark)
	j.gh.evaluateIndirectMerges(j.t)
	return nil
}

func TestReorderedPublishedStackProtectsPRsBeforeRealPushes(t *testing.T) {
	t.Run("push-first reproduces indirect merge and branch deletion", func(t *testing.T) {
		fixture := newReorderFixture(t)
		for _, sb := range fixture.analysis.Stack {
			if err := fixture.jj.Push(t.Context(), "origin", sb.Bookmark.Name); err != nil {
				t.Fatal(err)
			}
		}
		if pr := fixture.gh.prs["b"]; pr.State != "closed" || !pr.Merged {
			t.Fatalf("push-first PR b = %+v, want automatically merged", pr)
		}
		if gitRefExists(fixture.remote, "b") {
			t.Fatal("push-first order preserved b after automatic branch deletion")
		}
	})

	t.Run("protected plan preserves PRs and applies final bases", func(t *testing.T) {
		fixture := newReorderFixture(t)
		plan, err := CreateSubmissionPlan(t.Context(), fixture.analysis, &PlanningDeps{
			GitHub: fixture.gh, Owner: "owner", Repo: "repo", Remote: "origin", DefaultBranch: "main",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := ExecuteSubmissionPlan(t.Context(), plan, &ActionDeps{
			JJ: fixture.jj, GitHub: fixture.gh, Owner: "owner", Repo: "repo", Remote: "origin",
		}, nil)
		if err != nil || result.Summary.Failed != 0 {
			t.Fatalf("execute result=%+v err=%v events=%v", result, err, fixture.gh.events)
		}
		wantBases := map[string]string{"b": "main", "c": "b", "a": "c"}
		wantNumbers := map[string]int{"a": 1, "b": 2, "c": 3}
		for head, base := range wantBases {
			pr := fixture.gh.prs[head]
			if pr.Number != wantNumbers[head] || pr.State != "open" || pr.Merged || pr.Base != base {
				t.Fatalf("PR %s = %+v, want open with base %s", head, pr, base)
			}
			if !gitRefExists(fixture.remote, head) {
				t.Fatalf("remote branch %s was deleted", head)
			}
		}
		joined := strings.Join(fixture.gh.events, ",")
		if !strings.HasPrefix(joined, "base:b:main,push:b,push:c,push:a,base:a:c") || strings.Contains(joined, "auto-merged") {
			t.Fatalf("unsafe execution order: %v", fixture.gh.events)
		}
	})
}

type reorderFixture struct {
	remote   string
	gh       *reorderGitHub
	jj       *reorderJJ
	analysis *AnalysisResult
}

func newReorderFixture(t *testing.T) *reorderFixture {
	t.Helper()
	r := testutil.NewRepository(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare remote: %v %s", err, output)
	}
	r.Run(t, "git", "remote", "add", "origin", remote)
	r.Write(t, "a", "a\n")
	r.Run(t, "describe", "-m", "A")
	r.Run(t, "bookmark", "create", "a")
	r.Run(t, "new", "-m", "B")
	r.Write(t, "b", "b\n")
	r.Run(t, "bookmark", "create", "b")
	r.Run(t, "new", "-m", "C")
	r.Write(t, "c", "c\n")
	r.Run(t, "bookmark", "create", "c")
	for _, bookmark := range []string{"main", "a", "b", "c"} {
		if err := r.JJ.Push(t.Context(), "origin", bookmark); err != nil {
			t.Fatalf("initial push %s: %v", bookmark, err)
		}
	}
	prs := map[string]*github.PullRequest{
		"a": {Number: 1, Head: "a", Base: "main", State: "open"},
		"b": {Number: 2, Head: "b", Base: "a", State: "open"},
		"c": {Number: 3, Head: "c", Base: "b", State: "open"},
	}
	for head, pr := range prs {
		pr.HeadSHA = gitRev(t, remote, head)
	}
	r.Run(t, "rebase", "-r", "a", "-d", "c")
	graph, err := r.JJ.BuildChangeGraphForBookmark(t.Context(), "a", jjutils.RemoteBookmarkRevset("main", "origin"))
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := AnalyzeSubmission(t.Context(), graph, "a")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.HasErrors() {
		t.Fatalf("analysis errors: %v", analysis.Errors)
	}
	wantOrder := []string{"b", "c", "a"}
	if len(analysis.Stack) != len(wantOrder) {
		t.Fatalf("stack = %+v", analysis.Stack)
	}
	for i, name := range wantOrder {
		if analysis.Stack[i].Bookmark.Name != name || !analysis.Stack[i].NeedsPush {
			t.Fatalf("stack[%d] = %+v, want pushed %s", i, analysis.Stack[i], name)
		}
	}
	gh := &reorderGitHub{remote: remote, prs: prs, comments: map[int][]*github.Comment{}}
	return &reorderFixture{
		remote:   remote,
		gh:       gh,
		jj:       &reorderJJ{JJFunctions: r.JJ, t: t, gh: gh},
		analysis: analysis,
	}
}

func gitRefExists(remote, branch string) bool {
	return exec.Command("git", "--git-dir", remote, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

func gitRev(t *testing.T, remote, branch string) string {
	t.Helper()
	output, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+branch).Output()
	if err != nil {
		t.Fatalf("read remote branch %s: %v", branch, err)
	}
	return strings.TrimSpace(string(output))
}
