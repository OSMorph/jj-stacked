package sync

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/logger"
	"github.com/OSMorph/jj-stacked/internal/repo"
	syncpkg "github.com/OSMorph/jj-stacked/internal/sync"
)

type refreshJJ struct {
	jjutils.JJFunctions
	graph *jjutils.ChangeGraph
}

func (j *refreshJJ) BuildChangeGraphForBase(context.Context, string) (*jjutils.ChangeGraph, error) {
	return j.graph, nil
}

type refreshGitHub struct {
	github.GitHubClient
	updates  []int
	comments []int
}

func (g *refreshGitHub) FindPRByHead(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	num := 2
	if head == "unrelated" {
		num = 3
	}
	return &github.PullRequest{Number: num, Head: head, Base: "a"}, nil
}
func (g *refreshGitHub) ListComments(context.Context, string, string, int) ([]*github.Comment, error) {
	return nil, nil
}
func (g *refreshGitHub) UpdatePullRequest(_ context.Context, _, _ string, number int, _ *github.UpdatePRRequest) (*github.PullRequest, error) {
	g.updates = append(g.updates, number)
	return &github.PullRequest{Number: number}, nil
}
func (g *refreshGitHub) CreateComment(_ context.Context, _, _ string, number int, _ string) (*github.Comment, error) {
	g.comments = append(g.comments, number)
	return &github.Comment{ID: int64(number)}, nil
}

func TestRefreshSurvivorsAfterSelectedAnchorRemoved(t *testing.T) {
	graph := jjutils.NewChangeGraph()
	for _, name := range []string{"b", "unrelated"} {
		bm := jjutils.Bookmark{Name: name}
		segment := jjutils.BookmarkSegment{Bookmark: bm, Changes: []jjutils.LogEntry{{Description: "active", DescriptionFirstLine: "active"}}}
		graph.Bookmarks[name] = bm
		graph.Segments[name] = &segment
		graph.Roots = append(graph.Roots, name)
		graph.Leaves = append(graph.Leaves, name)
		graph.Stacks = append(graph.Stacks, jjutils.BranchStack{Segments: []jjutils.BookmarkSegment{segment}})
	}
	for _, resume := range []bool{false, true} {
		plan, err := syncpkg.CreateSyncPlan(&syncpkg.SyncAnalysis{Remote: "origin", TrunkBranch: "main", RemainingBookmarks: []string{"b"}})
		if err != nil {
			t.Fatal(err)
		}
		state := syncpkg.CreateInitialState(plan, "op", "a", false)
		if resume {
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			state = &syncpkg.SyncState{}
			if err := json.Unmarshal(data, state); err != nil {
				t.Fatal(err)
			}
		}
		selection, err := state.RefreshSelection()
		if err != nil {
			t.Fatal(err)
		}
		gh := &refreshGitHub{}
		err = refreshExistingPRs(context.Background(), &refreshJJ{graph: graph}, &repo.RepoContext{GitHub: gh, DefaultBranch: "main", Remote: "origin"}, selection, logger.NewFromEnv())
		if err != nil {
			t.Fatal(err)
		}
		if len(gh.updates) != 1 || gh.updates[0] != 2 || len(gh.comments) != 1 || gh.comments[0] != 2 {
			t.Fatalf("resume=%v touched wrong PRs: updates=%v comments=%v", resume, gh.updates, gh.comments)
		}
	}
}

func TestLegacyRefreshScopeUsesPersistedAnalysis(t *testing.T) {
	state := &syncpkg.SyncState{Bookmark: "a", Plan: &syncpkg.SyncPlan{Analysis: &syncpkg.SyncAnalysis{RemainingBookmarks: []string{"b"}}}}
	selection, err := state.RefreshSelection()
	if err != nil || len(selection) != 1 || selection[0] != "b" {
		t.Fatalf("selection=%v error=%v", selection, err)
	}
}
