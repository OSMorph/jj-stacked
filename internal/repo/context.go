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
	"github.com/OSMorph/jj-stacked/internal/jjutils"
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
	JJ     jjutils.JJFunctions

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

// Discover resolves local repository and remote settings without authenticating.
func Discover(ctx context.Context, opts RepoContextOptions) (*RepoContext, error) {
	executor := opts.Exec
	if executor == nil {
		executor = cmdexec.NewRealExecutor()
	}
	log := opts.Logger
	if log == nil {
		log = logger.NewFromEnv()
	}
	jj := jjutils.NewJJFunctions(executor, "")
	root, err := jj.GetRepoRoot(ctx)
	if err != nil {
		return nil, err
	}
	remotes, err := jj.ListRemotes(ctx)
	if err != nil {
		return nil, err
	}
	remote := opts.Remote
	if remote == "" {
		for _, candidate := range remotes {
			if candidate.Name == "origin" {
				remote = "origin"
				break
			}
		}
		if remote == "" && len(remotes) == 1 {
			remote = remotes[0].Name
		}
	}
	var remoteURL string
	for _, candidate := range remotes {
		if candidate.Name == remote {
			remoteURL = candidate.URL
			break
		}
	}
	if remoteURL == "" {
		return nil, fmt.Errorf("select a GitHub remote with --remote (requested %q)", remote)
	}
	owner, repository, host, err := parseRemoteURL(remoteURL)
	if err != nil {
		return nil, err
	}
	if value := os.Getenv("GITHUB_OWNER"); value != "" {
		owner = value
	}
	if value := os.Getenv("GITHUB_REPO"); value != "" {
		repository = value
	}
	if opts.GitHubHost != "" {
		host = opts.GitHubHost
	} else if value := os.Getenv("GITHUB_HOST"); value != "" {
		host = value
	}
	branch, err := jj.GetDefaultBranch(ctx)
	if err != nil {
		return nil, err
	}
	return &RepoContext{
		RootDir: root, Owner: owner, Repo: repository, Remote: remote,
		GitHubHost: host, DefaultBranch: branch, JJ: jj, Exec: executor, Logger: log,
	}, nil
}

// NewRepoContext discovers the repository and initializes its GitHub connection.
func NewRepoContext(ctx context.Context, opts RepoContextOptions) (*RepoContext, error) {
	repository, err := Discover(ctx, opts)
	if err != nil {
		return nil, err
	}
	authenticator, err := auth.NewAuthenticator(ctx, repository.Exec, repository.GitHubHost)
	if err != nil {
		return nil, err
	}
	token, err := authenticator.GetToken(ctx)
	if err != nil {
		return nil, err
	}
	repository.GitHub, err = github.NewClient(github.ClientOptions{
		Token: token, Hostname: repository.GitHubHost, APIBaseURL: os.Getenv("GITHUB_API_URL"),
	})
	if err != nil {
		return nil, err
	}
	if os.Getenv("TRUNK_BRANCH") == "" {
		branch, branchErr := repository.GitHub.GetDefaultBranch(ctx, repository.Owner, repository.Repo)
		if branchErr != nil {
			return nil, fmt.Errorf("resolve remote default branch (or set TRUNK_BRANCH): %w", branchErr)
		}
		repository.DefaultBranch = branch
	}
	return repository, nil
}

// parseRemoteURL extracts owner, repo, and host from a GitHub remote URL.
// Supports:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
//   - ssh://git@github.com/owner/repo.git
//   - ssh://git@github.com:22/owner/repo.git (with port)
//   - https://git.mycompany.com/owner/repo.git
//   - git@git.mycompany.com:owner/repo.git
func parseRemoteURL(url string) (owner, repo, host string, err error) {
	// Trim whitespace and any trailing characters
	url = strings.TrimSpace(url)

	// SSH format: git@host:owner/repo.git
	sshPattern := regexp.MustCompile(`^git@([^:]+):([^/]+)/(.+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return matches[2], stripGitSuffix(matches[3]), matches[1], nil
	}

	// SSH URL format: ssh://git@host/owner/repo.git or ssh://git@host:port/owner/repo.git
	sshURLPattern := regexp.MustCompile(`^ssh://git@([^/:]+)(?::\d+)?/([^/]+)/(.+?)(?:\.git)?$`)
	if matches := sshURLPattern.FindStringSubmatch(url); matches != nil {
		return matches[2], stripGitSuffix(matches[3]), matches[1], nil
	}

	// HTTPS format: https://host/owner/repo.git
	httpsPattern := regexp.MustCompile(`^https?://([^/]+)/([^/]+)/(.+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[2], stripGitSuffix(matches[3]), matches[1], nil
	}

	return "", "", "", &apperrors.ValidationError{
		Field:   "remote_url",
		Message: fmt.Sprintf("could not parse GitHub URL: %s", url),
	}
}

// stripGitSuffix removes .git suffix if present (handles edge cases where regex didn't catch it)
func stripGitSuffix(s string) string {
	return strings.TrimSuffix(s, ".git")
}
