// Package repo provides repository context for jj-stacked.
package repo

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/auth"
	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/logger"
)

// RepoContext holds all repository-related context needed throughout the application.
// AIDEV-NOTE: Supports both GitHub.com and GitHub Enterprise instances.
type RepoContext struct {
	// Paths
	RootDir string // Repository root directory

	// Remote info
	Owner      string
	Repo       string
	Remote     string // e.g., "origin"
	GitHubHost string // "github.com" or GHE hostname like "git.mycompany.com"

	// Branch info
	DefaultBranch string // main, master, or trunk

	// Clients
	GitHub github.GitHubClient
	Exec   cmdexec.CommandExecutor

	// Config
	Logger *logger.Logger
}

// RepoContextOptions configures repository context creation.
type RepoContextOptions struct {
	// Remote overrides the remote to use (defaults to "origin")
	Remote string

	// GitHubHost overrides GitHub host detection
	GitHubHost string

	// Logger for debug output
	Logger *logger.Logger

	// Exec is the command executor to use (for testing)
	Exec cmdexec.CommandExecutor
}

// NewRepoContext discovers repo info and creates context.
func NewRepoContext(ctx context.Context, opts RepoContextOptions) (*RepoContext, error) {
	exec := opts.Exec
	if exec == nil {
		exec = cmdexec.NewRealExecutor()
	}

	log := opts.Logger
	if log == nil {
		log = logger.NewFromEnv()
	}

	// Find repository root
	rootDir, err := findRepoRoot(ctx, exec)
	if err != nil {
		return nil, err
	}

	// Get remote name
	remote := opts.Remote
	if remote == "" {
		remote = "origin"
	}

	// Get remote URL and parse it
	remoteURL, err := getRemoteURL(ctx, exec, remote)
	if err != nil {
		return nil, err
	}

	owner, repo, host, err := parseRemoteURL(remoteURL)
	if err != nil {
		return nil, err
	}

	// Apply environment overrides
	if envOwner := os.Getenv("GITHUB_OWNER"); envOwner != "" {
		owner = envOwner
	}
	if envRepo := os.Getenv("GITHUB_REPO"); envRepo != "" {
		repo = envRepo
	}
	if opts.GitHubHost != "" {
		host = opts.GitHubHost
	} else if envHost := os.Getenv("GITHUB_HOST"); envHost != "" {
		host = envHost
	}

	log.Debug("discovered repository",
		"root", rootDir,
		"remote", remote,
		"owner", owner,
		"repo", repo,
		"host", host,
	)

	// Authenticate
	authenticator, err := auth.NewAuthenticator(ctx, exec, host)
	if err != nil {
		return nil, err
	}

	token, err := authenticator.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	// Create GitHub client
	ghClient, err := github.NewClient(github.ClientOptions{
		Token:    token,
		Hostname: host,
	})
	if err != nil {
		return nil, err
	}

	// Get default branch
	defaultBranch, err := ghClient.GetDefaultBranch(ctx, owner, repo)
	if err != nil {
		// Fall back to common defaults
		log.Warn("could not get default branch from GitHub, using 'main'", "error", err)
		defaultBranch = "main"
	}

	return &RepoContext{
		RootDir:       rootDir,
		Owner:         owner,
		Repo:          repo,
		Remote:        remote,
		GitHubHost:    host,
		DefaultBranch: defaultBranch,
		GitHub:        ghClient,
		Exec:          exec,
		Logger:        log,
	}, nil
}

// findRepoRoot finds the repository root using jj root.
func findRepoRoot(ctx context.Context, exec cmdexec.CommandExecutor) (string, error) {
	output, err := exec.Run(ctx, "jj", "root")
	if err != nil {
		return "", &apperrors.JJError{
			Command: "jj",
			Args:    []string{"root"},
			Err:     err,
		}
	}
	return strings.TrimSpace(output), nil
}

// getRemoteURL gets the URL for a git remote.
func getRemoteURL(ctx context.Context, exec cmdexec.CommandExecutor, remote string) (string, error) {
	output, err := exec.Run(ctx, "jj", "git", "remote", "list")
	if err != nil {
		return "", &apperrors.JJError{
			Command: "jj",
			Args:    []string{"git", "remote", "list"},
			Err:     err,
		}
	}

	// Parse output: each line is "remotename url"
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == remote {
			return parts[1], nil
		}
	}

	return "", &apperrors.ValidationError{
		Field:   "remote",
		Message: fmt.Sprintf("remote %q not found", remote),
	}
}

// parseRemoteURL extracts owner, repo, and host from a GitHub remote URL.
// Supports:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
//   - https://git.mycompany.com/owner/repo.git
//   - git@git.mycompany.com:owner/repo.git
func parseRemoteURL(url string) (owner, repo, host string, err error) {
	// SSH format: git@host:owner/repo.git
	sshPattern := regexp.MustCompile(`^git@([^:]+):([^/]+)/(.+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return matches[2], matches[3], matches[1], nil
	}

	// HTTPS format: https://host/owner/repo.git
	httpsPattern := regexp.MustCompile(`^https?://([^/]+)/([^/]+)/(.+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[2], matches[3], matches[1], nil
	}

	return "", "", "", &apperrors.ValidationError{
		Field:   "remote_url",
		Message: fmt.Sprintf("could not parse GitHub URL: %s", url),
	}
}
