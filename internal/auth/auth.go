// Package auth provides GitHub authentication for jj-stacked.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// Authenticator is the interface for GitHub authentication.
// AIDEV-NOTE: Supports both GitHub.com and GitHub Enterprise instances.
type Authenticator interface {
	// GetToken returns a valid GitHub token for the configured host.
	GetToken(ctx context.Context) (string, error)

	// GetUser returns the authenticated user info.
	GetUser(ctx context.Context) (*GitHubUser, error)

	// Method returns which auth method is being used.
	Method() string

	// Host returns the GitHub hostname (github.com or GHE host).
	Host() string
}

// GitHubUser represents authenticated user information.
type GitHubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// NewAuthenticator creates an Authenticator by trying methods in order.
// hostname should be "github.com" or a GHE hostname like "git.mycompany.com".
func NewAuthenticator(ctx context.Context, exec cmdexec.CommandExecutor, hostname string) (Authenticator, error) {
	if hostname == "" {
		hostname = "github.com"
	}

	// Try gh CLI first
	ghAuth := &ghCLIAuthenticator{
		exec:     exec,
		hostname: hostname,
	}
	if token, err := ghAuth.GetToken(ctx); err == nil && token != "" {
		return ghAuth, nil
	}

	// Try environment variables
	envAuth := &envAuthenticator{
		hostname: hostname,
	}
	if token, err := envAuth.GetToken(ctx); err == nil && token != "" {
		return envAuth, nil
	}

	// No valid auth found
	return nil, &apperrors.AuthError{
		Host:    hostname,
		Message: authSetupInstructions(hostname),
	}
}

// authSetupInstructions returns setup instructions for the given host.
func authSetupInstructions(hostname string) string {
	if hostname == "github.com" {
		return `no valid authentication found

To authenticate, use one of these methods:

1. GitHub CLI (recommended):
   $ gh auth login

2. Environment variable:
   $ export GITHUB_TOKEN=your_token
   or
   $ export GH_TOKEN=your_token

Token must have 'repo' scope for full functionality.`
	}

	return fmt.Sprintf(`no valid authentication found for %s

To authenticate, use one of these methods:

1. GitHub CLI (recommended):
   $ gh auth login --hostname %s

2. Environment variable:
   $ export GHE_TOKEN=your_token
   or
   $ export GITHUB_TOKEN=your_token

Token must have 'repo' scope for full functionality.`, hostname, hostname)
}

// ghCLIAuthenticator uses gh CLI for authentication.
type ghCLIAuthenticator struct {
	exec     cmdexec.CommandExecutor
	hostname string
	token    string
	user     *GitHubUser
	mu       sync.Mutex
}

func (a *ghCLIAuthenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" {
		return a.token, nil
	}

	output, err := a.exec.Run(ctx, "gh", "auth", "token", "--hostname", a.hostname)
	if err != nil {
		return "", &apperrors.AuthError{
			Method:  "gh_cli",
			Host:    a.hostname,
			Message: "gh CLI not authenticated",
			Err:     err,
		}
	}

	token := strings.TrimSpace(output)
	if token == "" {
		return "", &apperrors.AuthError{
			Method:  "gh_cli",
			Host:    a.hostname,
			Message: "gh CLI returned empty token",
		}
	}

	a.token = token
	return token, nil
}

func (a *ghCLIAuthenticator) GetUser(ctx context.Context) (*GitHubUser, error) {
	a.mu.Lock()
	if a.user != nil {
		a.mu.Unlock()
		return a.user, nil
	}
	a.mu.Unlock()

	// Get token without holding lock (GetToken has its own locking)
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	// Make network call without holding lock
	user, err := validateTokenAndGetUser(ctx, token, a.hostname)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.user = user
	a.mu.Unlock()
	return user, nil
}

func (a *ghCLIAuthenticator) Method() string {
	return "gh_cli"
}

func (a *ghCLIAuthenticator) Host() string {
	return a.hostname
}

// envAuthenticator uses environment variables for authentication.
type envAuthenticator struct {
	hostname string
	token    string
	user     *GitHubUser
	mu       sync.Mutex
}

func (a *envAuthenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" {
		return a.token, nil
	}

	var token string

	// For GHE, check GHE_TOKEN first
	if a.hostname != "github.com" {
		token = os.Getenv("GHE_TOKEN")
	}

	// Fall back to GITHUB_TOKEN, then GH_TOKEN
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}

	if token == "" {
		return "", &apperrors.AuthError{
			Method:  "env_token",
			Host:    a.hostname,
			Message: "no token found in environment variables",
		}
	}

	a.token = token
	return token, nil
}

func (a *envAuthenticator) GetUser(ctx context.Context) (*GitHubUser, error) {
	a.mu.Lock()
	if a.user != nil {
		a.mu.Unlock()
		return a.user, nil
	}
	a.mu.Unlock()

	// Get token without holding lock (GetToken has its own locking)
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	// Make network call without holding lock
	user, err := validateTokenAndGetUser(ctx, token, a.hostname)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.user = user
	a.mu.Unlock()
	return user, nil
}

func (a *envAuthenticator) Method() string {
	return "env_token"
}

func (a *envAuthenticator) Host() string {
	return a.hostname
}

// validateTokenAndGetUser validates a token by calling the GitHub API.
func validateTokenAndGetUser(ctx context.Context, token, hostname string) (*GitHubUser, error) {
	apiURL := getAPIBaseURL(hostname) + "/user"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, &apperrors.AuthError{
			Host:    hostname,
			Message: "failed to create request",
			Err:     err,
		}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &apperrors.AuthError{
			Host:    hostname,
			Message: "failed to connect to GitHub API",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, &apperrors.AuthError{
			Host:    hostname,
			Message: "token is invalid or expired",
		}
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &apperrors.AuthError{
			Host:    hostname,
			Message: fmt.Sprintf("unexpected response from GitHub API (HTTP %d): %s", resp.StatusCode, string(body)),
		}
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, &apperrors.AuthError{
			Host:    hostname,
			Message: "failed to parse user response",
			Err:     err,
		}
	}

	return &user, nil
}

// getAPIBaseURL returns the API base URL for a hostname.
func getAPIBaseURL(hostname string) string {
	if hostname == "github.com" {
		return "https://api.github.com"
	}
	return fmt.Sprintf("https://%s/api/v3", hostname)
}
