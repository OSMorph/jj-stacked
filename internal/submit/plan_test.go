package submit

import (
	"context"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type fakeGitHubClient struct {
	openPRs   []*github.PullRequest
	comments  map[int][]*github.Comment
	branches  map[string]bool
	userLogin string
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
	return f.openPRs, nil
}
func (f *fakeGitHubClient) FindPRByHead(ctx context.Context, owner, repo, head string) (*github.PullRequest, error) {
	return nil, nil
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
	return f.branches[branch], nil
}
func (f *fakeGitHubClient) GetAuthenticatedUser(ctx context.Context) (string, error) {
	if f.userLogin == "" {
		return "testuser", nil
	}
	return f.userLogin, nil
}
func (f *fakeGitHubClient) Host() string { return "github.com" }

func TestFindOrphanedPRs_DoesNotCloseWhenBranchStillExistsOnRemote(t *testing.T) {
	ctx := context.Background()

	stackComment := github.MetadataPrefix + "dummy" + github.MetadataSuffix + "\n\n" + github.CommentSignature

	analysis := &AnalysisResult{
		TargetBookmark: "b",
		Stack: []StackBookmark{
			{Bookmark: jjutils.Bookmark{Name: "a"}},
			{Bookmark: jjutils.Bookmark{Name: "b"}},
		},
	}

	prFromOtherStack := &github.PullRequest{
		Number: 1,
		Head:   "other-stack-branch",
		Base:   "b",
		State:  "open",
		Author: "me",
	}
	actuallyOrphaned := &github.PullRequest{
		Number: 2,
		Head:   "renamed-old-branch",
		Base:   "b",
		State:  "open",
		Author: "me",
	}

	fakeGH := &fakeGitHubClient{
		openPRs: []*github.PullRequest{
			prFromOtherStack,
			actuallyOrphaned,
		},
		comments: map[int][]*github.Comment{
			1: {{Body: stackComment}},
			2: {{Body: stackComment}},
		},
		branches: map[string]bool{
			"other-stack-branch":    true,
			"renamed-old-branch":    false,
			"a":                     true,
			"b":                     true,
			"main":                  true,
			"some-unrelated-branch": true,
		},
	}

	deps := &PlanningDeps{
		GitHub:        fakeGH,
		Owner:         "o",
		Repo:          "r",
		DefaultBranch: "main",
		CurrentUser:   "me",
	}

	orphaned := findOrphanedPRs(ctx, deps, analysis, map[string]*github.PullRequest{})
	if len(orphaned) != 1 {
		t.Fatalf("orphaned PRs = %d, want 1", len(orphaned))
	}
	if orphaned[0].Number != 2 {
		t.Fatalf("orphaned[0].Number = %d, want 2", orphaned[0].Number)
	}
}
