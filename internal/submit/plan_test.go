package submit

import (
	"context"
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
	return nil, nil
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
