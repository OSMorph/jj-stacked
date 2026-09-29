package submit

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type fakeGitHubClient struct {
	listOpenCalls, branchCalls, userCalls int
	openPRs                               []*github.PullRequest
	comments                              map[int][]*github.Comment
	branches                              map[string]bool
	userLogin                             string
	prsByHead                             map[string]*github.PullRequest
	historicalPRsByHead                   map[string]*github.PullRequest
}

func (f *fakeGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	return nil, nil
}
func (f *fakeGitHubClient) CreatePullRequest(ctx context.Context, owner, repo string, req *github.CreatePRRequest) (*github.PullRequest, error) {
	return nil, nil
}
func (f *fakeGitHubClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, req *github.UpdatePRRequest) (*github.PullRequest, error) {
	return nil, nil
}
func (f *fakeGitHubClient) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*github.PullRequest, error) {
	f.listOpenCalls++
	return f.openPRs, nil
}
func (f *fakeGitHubClient) FindPRByHead(ctx context.Context, owner, repo, head string) (*github.PullRequest, error) {
	return f.prsByHead[head], nil
}

func TestCreatePRRefreshPlanNeverCreatesOrPushes(t *testing.T) {
	analysis := &AnalysisResult{TargetBookmark: "b", Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "b"}, NeedsPush: true},
	}}
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", Base: "old-base"},
		},
		comments: map[int][]*github.Comment{}, branches: map[string]bool{},
	}
	plan, err := CreatePRRefreshPlan(context.Background(), analysis, &PlanningDeps{
		GitHub: client, Owner: "o", Repo: "r", Remote: "origin", DefaultBranch: "main",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Type() == ActionPush || action.Type() == ActionCreatePR || action.Type() == ActionType("close_pr") {
			t.Fatalf("refresh plan contains forbidden action %s", action.Type())
		}
	}
	if client.listOpenCalls != 0 || client.branchCalls != 0 {
		t.Fatal("refresh performed unrelated discovery")
	}
	if len(plan.Actions) != 2 { // update a's base and refresh a's comment
		t.Fatalf("refresh actions = %d, want 2", len(plan.Actions))
	}
}
func (f *fakeGitHubClient) FindPRByHeadAllStates(ctx context.Context, owner, repo, head string) (*github.PullRequest, error) {
	return f.historicalPRsByHead[head], nil
}
func (f *fakeGitHubClient) CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.Comment, error) {
	return nil, nil
}
func (f *fakeGitHubClient) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) (*github.Comment, error) {
	return nil, nil
}
func (f *fakeGitHubClient) ListComments(ctx context.Context, owner, repo string, prNumber int) ([]*github.Comment, error) {
	return f.comments[prNumber], nil
}
func (f *fakeGitHubClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return "main", nil
}
func (f *fakeGitHubClient) BranchExists(ctx context.Context, owner, repo, branch string) (bool, error) {
	f.branchCalls++
	return f.branches[branch], nil
}
func (f *fakeGitHubClient) GetAuthenticatedUser(ctx context.Context) (string, error) {
	f.userCalls++
	if f.userLogin == "" {
		return "testuser", nil
	}
	return f.userLogin, nil
}
func (f *fakeGitHubClient) Host() string { return "github.com" }

func TestSubmissionDoesNotDiscoverOrCloseUnrelatedPRs(t *testing.T) {
	client := &fakeGitHubClient{openPRs: []*github.PullRequest{{Number: 9, Head: "unrelated", Base: "main"}}}
	plan, err := CreateSubmissionPlan(context.Background(), &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a"}}}}, &PlanningDeps{GitHub: client, DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Type() == ActionType("close_pr") {
			t.Fatal("submission must never close a PR")
		}
	}
	if client.listOpenCalls != 0 || client.branchCalls != 0 || client.userCalls != 0 {
		t.Fatalf("unrelated discovery: %+v", client)
	}
}

func TestSubmissionProtectsReorderedPRsBeforePush(t *testing.T) {
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", Base: "main", URL: "https://example.test/1"},
			"b": {Number: 2, Head: "b", Base: "a", URL: "https://example.test/2"},
		},
		comments: map[int][]*github.Comment{},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "b"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true},
	}}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{
		GitHub: client, Remote: "origin", DefaultBranch: "main",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"protect_base:2:a:main",
		"push:b", "push:a",
		"update_base:1:main:b",
		"sync_comment:b", "sync_comment:a",
	}
	if got := actionSignatures(plan.Actions); !equalStrings(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	if plan.Summary.PRsToProtect != 1 || plan.Summary.PRsToUpdate != 1 {
		t.Fatalf("summary = %+v", plan.Summary)
	}

	dryRun := FormatDryRunOutput(analysis, plan)
	protectAt := strings.Index(dryRun, "[PROTECT BASE]")
	pushAt := strings.Index(dryRun, "[PUSH]")
	finalAt := strings.Index(dryRun, "[UPDATE BASE]")
	if protectAt < 0 || pushAt < protectAt || finalAt < pushAt {
		t.Fatalf("dry-run order is unsafe:\n%s", dryRun)
	}
	if !strings.Contains(dryRun, "1 PR base(s) to protect before pushing") || !strings.Contains(dryRun, "1 PR base(s) to set after pushing") {
		t.Fatalf("dry-run summary does not distinguish base phases:\n%s", dryRun)
	}
}

func TestSubmissionProtectsMoveAcrossMultipleChildren(t *testing.T) {
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", Base: "main"},
			"b": {Number: 2, Head: "b", Base: "a"},
			"c": {Number: 3, Head: "c", Base: "b"},
			"d": {Number: 4, Head: "d", Base: "c"},
		},
		comments: map[int][]*github.Comment{},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "b"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "c"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "d"}, NeedsPush: true},
	}}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: client, Remote: "origin", DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"protect_base:2:a:main", "protect_base:4:c:main", "push:b", "push:c", "push:a", "push:d"}
	got := actionSignatures(plan.Actions)
	if len(got) < len(wantPrefix) || !equalStrings(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("action prefix = %v, want %v", got, wantPrefix)
	}
	if plan.Summary.PRsToProtect != 2 || plan.Summary.PRsToUpdate != 2 {
		t.Fatalf("summary = %+v", plan.Summary)
	}
}

func TestSubmissionDoesNotProtectUnchangedOrNewPRs(t *testing.T) {
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", Base: "main"},
			"b": {Number: 2, Head: "b", Base: "a"},
		},
		comments: map[int][]*github.Comment{},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "a"}},
		{Bookmark: jjutils.Bookmark{Name: "b"}},
		{Bookmark: jjutils.Bookmark{Name: "c"}, NeedsPush: true},
	}}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: client, Remote: "origin", DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.PRsToProtect != 0 {
		t.Fatalf("new/unchanged stack planned protection: %v", actionSignatures(plan.Actions))
	}
	if got := actionSignatures(plan.Actions); len(got) == 0 || got[0] != "push:c" {
		t.Fatalf("actions = %v", got)
	}
}

func TestSubmissionProtectsMismatchedNonStackBaseWhenPushPlanned(t *testing.T) {
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", Base: "main"},
			"b": {Number: 2, Head: "b", Base: "legacy-base"},
		},
		comments: map[int][]*github.Comment{},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "a"}},
		{Bookmark: jjutils.Bookmark{Name: "b"}},
		{Bookmark: jjutils.Bookmark{Name: "c"}, NeedsPush: true},
	}}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: client, Remote: "origin", DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := actionSignatures(plan.Actions)
	wantPrefix := []string{"protect_base:2:legacy-base:main", "push:c", "update_base:2:main:a", "create_pr:c"}
	if len(got) < len(wantPrefix) || !equalStrings(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("actions = %v, want prefix %v", got, wantPrefix)
	}
	if plan.Summary.PRsToProtect != 1 {
		t.Fatalf("summary = %+v", plan.Summary)
	}
}

func TestSubmissionDoesNotReplaceHistoricalPRAtCurrentCommit(t *testing.T) {
	const current = "1111111111111111111111111111111111111111"
	analysis := &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a", CommitID: current}}}}
	for _, test := range []struct {
		name       string
		historical *github.PullRequest
		wantReason string
		wantFix    string
	}{
		{
			name: "marked merged after pushes already succeeded",
			historical: &github.PullRequest{
				Number: 1, Head: "a", HeadSHA: current, State: "closed", Merged: true,
			},
			wantReason: "matches this commit",
			wantFix:    "GitHub cannot reopen merged PRs; use a new bookmark name",
		},
		{
			name: "closed without merge",
			historical: &github.PullRequest{
				Number: 1, Head: "a", HeadSHA: current, State: "closed",
			},
			wantReason: "matches this commit",
			wantFix:    "Reopen it on GitHub or use a new bookmark name",
		},
		{
			name: "missing head identity",
			historical: &github.PullRequest{
				Number: 1, Head: "a", State: "closed",
			},
			wantReason: "has no reviewed head commit",
			wantFix:    "Reopen it on GitHub or use a new bookmark name",
		},
		{
			name: "merged with missing head identity",
			historical: &github.PullRequest{
				Number: 1, Head: "a", State: "closed", Merged: true,
			},
			wantReason: "has no reviewed head commit",
			wantFix:    "GitHub cannot reopen merged PRs; use a new bookmark name",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeGitHubClient{
				prsByHead:           map[string]*github.PullRequest{},
				historicalPRsByHead: map[string]*github.PullRequest{"a": test.historical},
				comments:            map[int][]*github.Comment{},
			}
			_, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: client, Remote: "origin", DefaultBranch: "main"}, nil)
			if err == nil || !strings.Contains(err.Error(), test.wantReason) || !strings.Contains(err.Error(), test.wantFix) || !strings.Contains(err.Error(), "will not reopen or replace it") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSubmissionAllowsReusedBookmarkAtDifferentCommit(t *testing.T) {
	const current = "1111111111111111111111111111111111111111"
	client := &fakeGitHubClient{
		prsByHead: map[string]*github.PullRequest{},
		historicalPRsByHead: map[string]*github.PullRequest{
			"a": {Number: 1, Head: "a", HeadSHA: "2222222222222222222222222222222222222222", State: "closed", Merged: true},
		},
		comments: map[int][]*github.Comment{},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a", CommitID: current}, NeedsPush: true}}}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: client, Remote: "origin", DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := actionSignatures(plan.Actions)
	if len(got) < 2 || !equalStrings(got[:2], []string{"push:a", "create_pr:a"}) {
		t.Fatalf("actions = %v", got)
	}
}

func actionSignatures(actions []SubmissionAction) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		switch a := action.(type) {
		case *PushAction:
			result = append(result, "push:"+a.Bookmark)
		case *CreatePRAction:
			result = append(result, "create_pr:"+a.Bookmark)
		case *UpdateBaseAction:
			result = append(result, string(a.Type())+":"+fmt.Sprint(a.PRNumber)+":"+a.OldBase+":"+a.NewBase)
		case *SyncCommentAction:
			result = append(result, "sync_comment:"+a.Bookmark)
		}
	}
	return result
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
