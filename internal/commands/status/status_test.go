package status

import (
	"context"
	"testing"
	"time"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/repo"
)

type statusJJ struct {
	jjutils.JJFunctions
	graph     *jjutils.ChangeGraph
	bookmarks []jjutils.Bookmark
	remote    string
	entries   []jjutils.LogEntry
}

func (j *statusJJ) BuildChangeGraphForBase(context.Context, string) (*jjutils.ChangeGraph, error) {
	return j.graph, nil
}
func (j *statusJJ) ListBookmarksForRemote(_ context.Context, remote string) ([]jjutils.Bookmark, error) {
	j.remote = remote
	return j.bookmarks, nil
}
func (j *statusJJ) GetLog(context.Context, string, int) ([]jjutils.LogEntry, error) {
	if j.entries != nil {
		return j.entries, nil
	}
	return []jjutils.LogEntry{{CommitID: "new", Conflict: true, Divergent: true}}, nil
}

type statusGitHub struct {
	github.GitHubClient
	pr *github.PullRequest
}

func (g *statusGitHub) FindPRByHeadAllStates(context.Context, string, string, string) (*github.PullRequest, error) {
	return g.pr, nil
}
func TestStatusUsesSelectedRemoteWithoutAuthentication(t *testing.T) {
	jj := &statusJJ{graph: jjutils.NewChangeGraph(), bookmarks: []jjutils.Bookmark{{Name: "main"}, {Name: "feature", CommitID: "new", RemoteName: "upstream", HasRemote: true, IsSynced: true}}}
	repository := &repo.RepoContext{JJ: jj, Remote: "upstream", DefaultBranch: "main"}
	result, err := collect(context.Background(), repository, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.GitHubChecked || jj.remote != "upstream" || len(result.Bookmarks) != 1 || result.Bookmarks[0].NeedsPush || !result.Bookmarks[0].Conflict || !result.Bookmarks[0].Divergent {
		t.Fatalf("incorrect local status: %+v", result)
	}
	now := time.Now()
	repository.GitHub = &statusGitHub{pr: &github.PullRequest{Number: 1, Merged: true, MergedAt: &now, HeadSHA: "old"}}
	result, err = collect(context.Background(), repository, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bookmarks[0].CleanupReason != "merged PR head differs; preserve local work" {
		t.Fatalf("reused bookmark marked disposable: %+v", result.Bookmarks[0])
	}
}

func TestStatusReportsConflictedBookmarkTargetsOmittedByNormalList(t *testing.T) {
	jj := &statusJJ{graph: jjutils.NewChangeGraph(), entries: []jjutils.LogEntry{{CommitID: "first", LocalBookmarks: []string{"conflicted"}}, {CommitID: "second", LocalBookmarks: []string{"conflicted"}}}}
	result, err := collect(context.Background(), &repo.RepoContext{JJ: jj, Remote: "origin", DefaultBranch: "main"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bookmarks) != 2 {
		t.Fatalf("conflicted references disappeared: %+v", result)
	}
	for i := range result.Bookmarks {
		if !result.Bookmarks[i].BookmarkConflict || !result.Bookmarks[i].Excluded {
			t.Fatalf("conflicted ref not identified: %+v", result.Bookmarks[i])
		}
	}
}
