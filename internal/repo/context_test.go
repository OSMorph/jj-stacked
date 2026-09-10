package repo

import (
	"context"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

func TestDiscoverUsesConfiguredJJWithoutAuthenticating(t *testing.T) {
	t.Setenv("JJ_PATH", "custom-jj")
	t.Setenv("TRUNK_BRANCH", "develop")
	executor := cmdexec.NewMockExecutor()
	executor.SetResponse("/fixture\n", "custom-jj", "root")
	executor.SetResponse("upstream https://example.test/owner/repository.git\n", "custom-jj", "git", "remote", "list")
	r, err := Discover(context.Background(), RepoContextOptions{Exec: executor})
	if err != nil {
		t.Fatal(err)
	}
	if r.GitHub != nil || r.JJ == nil || r.Remote != "upstream" || r.DefaultBranch != "develop" || r.Owner != "owner" || r.Repo != "repository" {
		t.Fatalf("discovery=%+v", r)
	}
	for _, call := range executor.Calls {
		if call.Name != "custom-jj" {
			t.Fatalf("discovery authenticated or ignored JJ_PATH: %+v", call)
		}
	}
}

func TestRemoteParsing(t *testing.T) {
	for _, value := range []string{"https://example.test/owner/repo.git", "git@example.test:owner/repo.git", "ssh://git@example.test:22/owner/repo.git"} {
		owner, repository, host, err := parseRemoteURL(value)
		if err != nil || owner != "owner" || repository != "repo" || host != "example.test" {
			t.Fatalf("parse %s = %s %s %s %v", value, owner, repository, host, err)
		}
	}
}
