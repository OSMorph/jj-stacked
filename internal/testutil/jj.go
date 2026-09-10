// Package testutil builds isolated jj repositories for integration tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// Repository owns an isolated workspace. It never uses Git commits or GitHub.
type Repository struct {
	Dir  string
	JJ   jjutils.JJFunctions
	Exec *cmdexec.RealExecutor
}

// NewRepository creates a base and working copy with repository-local settings.
func NewRepository(t *testing.T) *Repository {
	t.Helper()
	if testing.Short() {
		t.Skip("jj integration test")
	}
	if _, err := exec.LookPath(jjutils.Binary()); err != nil {
		t.Skip("jj not installed")
	}
	r := &Repository{Dir: t.TempDir()}
	r.Exec = cmdexec.NewRealExecutorInDir(r.Dir)
	r.JJ = jjutils.NewJJFunctions(r.Exec, "")
	r.Run(t, "git", "init", "--colocate")
	r.Run(t, "config", "set", "--repo", "user.name", "Test User")
	r.Run(t, "config", "set", "--repo", "user.email", "test@example.com")
	r.Run(t, "config", "set", "--repo", `revset-aliases."trunk()"`, "main")
	r.Write(t, "base", "base\n")
	r.Run(t, "describe", "-m", "Base")
	r.Run(t, "bookmark", "create", "main")
	r.Run(t, "new", "main")
	return r
}

// Run invokes jj in the fixture and returns stdout.
func (r *Repository) Run(t *testing.T, args ...string) string {
	t.Helper()
	output, err := r.Exec.Run(t.Context(), jjutils.Binary(), args...)
	if err != nil {
		t.Fatalf("jj %v: %v", args, err)
	}
	return output
}

// Write changes one workspace file for the next snapshot.
func (r *Repository) Write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Change snapshots and returns the current commit.
func (r *Repository) Change(t *testing.T) jjutils.LogEntry {
	t.Helper()
	entry, err := r.JJ.GetChange(t.Context(), "@")
	if err != nil {
		t.Fatal(err)
	}
	return *entry
}
