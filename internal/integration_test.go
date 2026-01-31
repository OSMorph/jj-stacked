// Package internal contains integration tests that test the full workflow
// with real jj repositories and mock GitHub API.
package internal

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
	"github.com/OSMorph/jj-stacked/internal/submit"
)

// AIDEV-NOTE: Integration tests create real jj repositories to test the full workflow.
// GitHub API is mocked to avoid external dependencies.

// checkJJInstalled checks if jj is installed and returns an error if not.
func checkJJInstalled(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj not installed, skipping integration test")
	}
}

// checkGitInstalled checks if git is installed and returns an error if not.
func checkGitInstalled(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed, skipping integration test")
	}
}

// testRepo holds a test repository configuration.
type testRepo struct {
	dir     string
	cleanup func()
	exec    *cmdexec.RealExecutor
}

// setupTestRepo creates a temporary directory with a git/jj repository.
// Returns the repo path and a cleanup function.
func setupTestRepo(t *testing.T) *testRepo {
	t.Helper()
	checkJJInstalled(t)
	checkGitInstalled(t)

	// Create temp directory
	dir, err := os.MkdirTemp("", "jj-stacked-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	// Initialize git repo
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	executor := cmdexec.NewRealExecutorInDir(dir)

	// Initialize git
	if _, err := executor.Run(ctx, "git", "init"); err != nil {
		cleanup()
		t.Fatalf("failed to init git: %v", err)
	}

	// Configure git user (required for commits)
	if _, err := executor.Run(ctx, "git", "config", "user.email", "test@example.com"); err != nil {
		cleanup()
		t.Fatalf("failed to configure git email: %v", err)
	}
	if _, err := executor.Run(ctx, "git", "config", "user.name", "Test User"); err != nil {
		cleanup()
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create initial commit (git requires at least one commit for jj)
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repository\n"), 0o644); err != nil {
		cleanup()
		t.Fatalf("failed to write README: %v", err)
	}

	if _, err := executor.Run(ctx, "git", "add", "."); err != nil {
		cleanup()
		t.Fatalf("failed to git add: %v", err)
	}
	if _, err := executor.Run(ctx, "git", "commit", "-m", "Initial commit"); err != nil {
		cleanup()
		t.Fatalf("failed to git commit: %v", err)
	}

	// Initialize jj colocated repo
	if _, err := executor.Run(ctx, "jj", "git", "init", "--colocate"); err != nil {
		cleanup()
		t.Fatalf("failed to init jj: %v", err)
	}

	return &testRepo{
		dir:     dir,
		cleanup: cleanup,
		exec:    executor,
	}
}

// createChange creates a new jj change with the given description.
func (r *testRepo) createChange(t *testing.T, description string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a new change
	if _, err := r.exec.Run(ctx, "jj", "new", "-m", description); err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	// Create a file to make the change non-empty
	filename := strings.ReplaceAll(description, " ", "_") + ".txt"
	filePath := filepath.Join(r.dir, filename)
	if err := os.WriteFile(filePath, []byte(description+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

// createBookmark creates a bookmark at the current change.
func (r *testRepo) createBookmark(t *testing.T, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := r.exec.Run(ctx, "jj", "bookmark", "create", name); err != nil {
		t.Fatalf("failed to create bookmark %s: %v", name, err)
	}
}

// TestIntegration_SimpleStack tests building a graph with a simple stack of 3 bookmarks.
func TestIntegration_SimpleStack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Create a stack of 3 changes with bookmarks:
	// feature-c → feature-b → feature-a → main

	// Create feature-a
	repo.createChange(t, "Add feature A")
	repo.createBookmark(t, "feature-a")

	// Create feature-b on top of feature-a
	repo.createChange(t, "Add feature B")
	repo.createBookmark(t, "feature-b")

	// Create feature-c on top of feature-b
	repo.createChange(t, "Add feature C")
	repo.createBookmark(t, "feature-c")

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Verify we have 3 bookmarks
	if len(graph.Bookmarks) != 3 {
		t.Errorf("expected 3 bookmarks, got %d", len(graph.Bookmarks))
	}

	// Verify bookmark names exist
	for _, name := range []string{"feature-a", "feature-b", "feature-c"} {
		if _, ok := graph.Bookmarks[name]; !ok {
			t.Errorf("expected bookmark %q to exist", name)
		}
	}

	// Verify we have exactly one stack
	if len(graph.Stacks) != 1 {
		t.Errorf("expected 1 stack, got %d", len(graph.Stacks))
	}

	// Verify stack structure
	if len(graph.Stacks) > 0 {
		stack := graph.Stacks[0]
		if len(stack.Segments) != 3 {
			t.Errorf("expected 3 segments in stack, got %d", len(stack.Segments))
		}

		// Verify order: feature-a (bottom) to feature-c (top)
		expectedOrder := []string{"feature-a", "feature-b", "feature-c"}
		for i, seg := range stack.Segments {
			if i < len(expectedOrder) && seg.Bookmark.Name != expectedOrder[i] {
				t.Errorf("segment[%d] = %q, want %q", i, seg.Bookmark.Name, expectedOrder[i])
			}
		}
	}

	// Verify roots and leaves
	if len(graph.Roots) != 1 || graph.Roots[0] != "feature-a" {
		t.Errorf("expected roots = [feature-a], got %v", graph.Roots)
	}

	if len(graph.Leaves) != 1 || graph.Leaves[0] != "feature-c" {
		t.Errorf("expected leaves = [feature-c], got %v", graph.Leaves)
	}

	// Verify parent-child relationships
	if graph.ChildToParent["feature-b"] != "feature-a" {
		t.Errorf("expected feature-b parent = feature-a, got %q", graph.ChildToParent["feature-b"])
	}
	if graph.ChildToParent["feature-c"] != "feature-b" {
		t.Errorf("expected feature-c parent = feature-b, got %q", graph.ChildToParent["feature-c"])
	}

	// Verify no tainted bookmarks
	if graph.ExcludedCount != 0 {
		t.Errorf("expected 0 excluded bookmarks, got %d", graph.ExcludedCount)
	}
}

// TestIntegration_MultipleStacks tests building a graph with multiple independent stacks.
func TestIntegration_MultipleStacks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Create first stack: feature-b → feature-a → main
	repo.createChange(t, "Add feature A")
	repo.createBookmark(t, "feature-a")

	repo.createChange(t, "Add feature B")
	repo.createBookmark(t, "feature-b")

	// Go back to trunk (main) to create a second independent stack
	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Move back to the root to create independent stack
	if _, err := repo.exec.Run(ctx2, "jj", "new", "root()"); err != nil {
		t.Fatalf("failed to go back to root: %v", err)
	}

	// Create second stack: bugfix-y → bugfix-x → main
	repo.createChange(t, "Fix bug X")
	repo.createBookmark(t, "bugfix-x")

	repo.createChange(t, "Fix bug Y")
	repo.createBookmark(t, "bugfix-y")

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Verify we have 4 bookmarks
	if len(graph.Bookmarks) != 4 {
		t.Errorf("expected 4 bookmarks, got %d", len(graph.Bookmarks))
	}

	// Verify we have 2 stacks
	if len(graph.Stacks) != 2 {
		t.Errorf("expected 2 stacks, got %d", len(graph.Stacks))
	}

	// Verify roots (both feature-a and bugfix-x should be roots)
	if len(graph.Roots) != 2 {
		t.Errorf("expected 2 roots, got %d: %v", len(graph.Roots), graph.Roots)
	}

	// Verify leaves (both feature-b and bugfix-y should be leaves)
	if len(graph.Leaves) != 2 {
		t.Errorf("expected 2 leaves, got %d: %v", len(graph.Leaves), graph.Leaves)
	}
}

// TestIntegration_SubmitAnalysis tests the analysis phase with a real repository.
func TestIntegration_SubmitAnalysis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Create a stack: feature-b → feature-a → main
	repo.createChange(t, "Add feature A")
	repo.createBookmark(t, "feature-a")

	repo.createChange(t, "Add feature B with detailed description")
	repo.createBookmark(t, "feature-b")

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Run analysis
	result, err := submit.AnalyzeSubmission(ctx, graph, "feature-b")
	if err != nil {
		t.Fatalf("AnalyzeSubmission failed: %v", err)
	}

	// Verify no errors
	if result.HasErrors() {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Verify stack
	if len(result.Stack) != 2 {
		t.Errorf("expected 2 bookmarks in stack, got %d", len(result.Stack))
	}

	// Verify order
	if len(result.Stack) >= 2 {
		if result.Stack[0].Bookmark.Name != "feature-a" {
			t.Errorf("Stack[0] = %q, want %q", result.Stack[0].Bookmark.Name, "feature-a")
		}
		if result.Stack[1].Bookmark.Name != "feature-b" {
			t.Errorf("Stack[1] = %q, want %q", result.Stack[1].Bookmark.Name, "feature-b")
		}
	}

	// Verify all bookmarks need push (no remote set up)
	for _, sb := range result.Stack {
		if !sb.NeedsPush {
			t.Errorf("expected %q to need push", sb.Bookmark.Name)
		}
	}

	// Verify titles were extracted
	if len(result.Stack) > 0 && result.Stack[0].Title == "" {
		t.Error("expected Stack[0] to have a title")
	}
}

// TestIntegration_SubmitDryRun tests the dry run mode with a real repository
// and mock GitHub client.
func TestIntegration_SubmitDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Create a stack: feature-b → feature-a → main
	repo.createChange(t, "Add feature A")
	repo.createBookmark(t, "feature-a")

	repo.createChange(t, "Add feature B")
	repo.createBookmark(t, "feature-b")

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Run analysis
	analysis, err := submit.AnalyzeSubmission(ctx, graph, "feature-b")
	if err != nil {
		t.Fatalf("AnalyzeSubmission failed: %v", err)
	}

	if analysis.HasErrors() {
		t.Fatalf("analysis has errors: %v", analysis.Errors)
	}

	// Create mock GitHub client
	mockGH := &mockGitHubClient{
		prs: make(map[string]*github.PullRequest),
	}

	// Create submission plan
	deps := &submit.PlanningDeps{
		GitHub:        mockGH,
		Owner:         "testowner",
		Repo:          "testrepo",
		Remote:        "origin",
		DefaultBranch: "main",
	}

	plan, err := submit.CreateSubmissionPlan(ctx, analysis, deps, nil)
	if err != nil {
		t.Fatalf("CreateSubmissionPlan failed: %v", err)
	}

	// Verify plan has correct actions
	// Should have: 2 push actions + 2 create PR actions + 2 sync comment actions
	if len(plan.Actions) < 4 {
		t.Errorf("expected at least 4 actions, got %d", len(plan.Actions))
	}

	// Count action types
	pushCount := 0
	createCount := 0
	for _, action := range plan.Actions {
		switch action.Type() {
		case submit.ActionPush:
			pushCount++
		case submit.ActionCreatePR:
			createCount++
		case submit.ActionUpdateBase, submit.ActionSyncComment, submit.ActionClosePR:
			// Not counting these action types in this test
		}
	}

	if pushCount != 2 {
		t.Errorf("expected 2 push actions, got %d", pushCount)
	}
	if createCount != 2 {
		t.Errorf("expected 2 create PR actions, got %d", createCount)
	}

	// Verify dry run output is non-empty
	dryRunOutput := submit.FormatDryRunOutput(analysis, plan)
	if dryRunOutput == "" {
		t.Error("expected non-empty dry run output")
	}

	// Verify dry run output mentions the bookmarks
	if !strings.Contains(dryRunOutput, "feature-a") {
		t.Error("dry run output should mention feature-a")
	}
	if !strings.Contains(dryRunOutput, "feature-b") {
		t.Error("dry run output should mention feature-b")
	}
}

// TestIntegration_EmptyRepo tests handling of a repository with no user bookmarks.
func TestIntegration_EmptyRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Don't create any bookmarks - just use the repo as-is

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Verify empty graph
	if len(graph.Bookmarks) != 0 {
		t.Errorf("expected 0 bookmarks, got %d", len(graph.Bookmarks))
	}

	if len(graph.Stacks) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(graph.Stacks))
	}
}

// TestIntegration_SingleBookmark tests a single bookmark directly on trunk.
func TestIntegration_SingleBookmark(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := setupTestRepo(t)
	defer repo.cleanup()

	ctx := context.Background()
	jj := jjutils.NewJJFunctions(repo.exec, "jj")

	// Create a single change with a bookmark
	repo.createChange(t, "Add single feature")
	repo.createBookmark(t, "single-feature")

	// Build the change graph
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		t.Fatalf("BuildChangeGraph failed: %v", err)
	}

	// Verify single bookmark
	if len(graph.Bookmarks) != 1 {
		t.Errorf("expected 1 bookmark, got %d", len(graph.Bookmarks))
	}

	// Should be both root and leaf
	if len(graph.Roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(graph.Roots))
	}
	if len(graph.Leaves) != 1 {
		t.Errorf("expected 1 leaf, got %d", len(graph.Leaves))
	}

	// Verify it's a valid stack
	if len(graph.Stacks) != 1 {
		t.Errorf("expected 1 stack, got %d", len(graph.Stacks))
	}

	// Run analysis on the single bookmark
	result, err := submit.AnalyzeSubmission(ctx, graph, "single-feature")
	if err != nil {
		t.Fatalf("AnalyzeSubmission failed: %v", err)
	}

	if result.HasErrors() {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	if len(result.Stack) != 1 {
		t.Errorf("expected 1 bookmark in stack, got %d", len(result.Stack))
	}
}

// mockGitHubClient is a mock implementation of github.GitHubClient for testing.
type mockGitHubClient struct {
	prs         map[string]*github.PullRequest // key: head branch
	prCounter   int
	comments    map[int][]*github.Comment // key: PR number
	returnError error
}

func (m *mockGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	for _, pr := range m.prs {
		if pr.Number == number {
			return pr, nil
		}
	}
	return nil, nil
}

func (m *mockGitHubClient) CreatePullRequest(ctx context.Context, owner, repo string, req *github.CreatePRRequest) (*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	m.prCounter++
	pr := &github.PullRequest{
		Number: m.prCounter,
		Title:  req.Title,
		Body:   req.Body,
		Head:   req.Head,
		Base:   req.Base,
		State:  "open",
		URL:    "https://github.com/testowner/testrepo/pull/" + string(rune('0'+m.prCounter)),
	}
	m.prs[req.Head] = pr
	return pr, nil
}

func (m *mockGitHubClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, req *github.UpdatePRRequest) (*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	for _, pr := range m.prs {
		if pr.Number == number {
			if req.Title != nil {
				pr.Title = *req.Title
			}
			if req.Body != nil {
				pr.Body = *req.Body
			}
			if req.Base != nil {
				pr.Base = *req.Base
			}
			return pr, nil
		}
	}
	return nil, nil
}

func (m *mockGitHubClient) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	var result []*github.PullRequest
	for _, pr := range m.prs {
		if pr.State == "open" {
			result = append(result, pr)
		}
	}
	return result, nil
}

func (m *mockGitHubClient) FindPRByHead(ctx context.Context, owner, repo, head string) (*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	return m.prs[head], nil
}

func (m *mockGitHubClient) FindPRByHeadAllStates(ctx context.Context, owner, repo, head string) (*github.PullRequest, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	// Return the PR regardless of state (for sync command testing)
	return m.prs[head], nil
}

func (m *mockGitHubClient) CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.Comment, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	comment := &github.Comment{
		ID:   int64(len(m.comments[prNumber]) + 1),
		Body: body,
		User: "test-bot",
	}
	m.comments[prNumber] = append(m.comments[prNumber], comment)
	return comment, nil
}

func (m *mockGitHubClient) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) (*github.Comment, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	return &github.Comment{
		ID:   commentID,
		Body: body,
		User: "test-bot",
	}, nil
}

func (m *mockGitHubClient) ListComments(ctx context.Context, owner, repo string, prNumber int) ([]*github.Comment, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	return m.comments[prNumber], nil
}

func (m *mockGitHubClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	if m.returnError != nil {
		return "", m.returnError
	}
	return "main", nil
}

func (m *mockGitHubClient) GetAuthenticatedUser(ctx context.Context) (string, error) {
	if m.returnError != nil {
		return "", m.returnError
	}
	return "testuser", nil
}

func (m *mockGitHubClient) Host() string {
	return "github.com"
}
