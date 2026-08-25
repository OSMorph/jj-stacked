package jjutils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

func TestPushUndescribedChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping jj integration test in short mode")
	}
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	workDir := filepath.Join(root, "work")

	runCommand(t, root, "git", "init", "--bare", remoteDir)
	runCommand(t, root, "git", "init", workDir)
	runCommand(t, workDir, "jj", "git", "init", "--colocate")
	runCommand(t, workDir, "jj", "config", "set", "--repo", "user.name", "Test User")
	runCommand(t, workDir, "jj", "config", "set", "--repo", "user.email", "test@example.com")
	runCommand(t, workDir, "git", "remote", "add", "origin", remoteDir)

	if err := os.WriteFile(filepath.Join(workDir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCommand(t, workDir, "jj", "bookmark", "create", "feature")

	executor := cmdexec.NewRealExecutorInDir(workDir)
	jj := NewJJFunctions(executor, "jj")
	if err := jj.Push(t.Context(), "origin", "feature", PushOptions{AllowEmptyDescription: true}); err != nil {
		t.Fatalf("push undescribed change: %v", err)
	}

	runCommand(t, root, "git", "--git-dir", remoteDir, "show-ref", "--verify", "refs/heads/feature")
}

func runCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, output)
	}
}
