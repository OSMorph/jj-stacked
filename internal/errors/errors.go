// Package errors provides custom error types for jj-stacked.
// AIDEV-NOTE: Error messages should be actionable and include suggestions.
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// JJError represents errors from jj command execution.
type JJError struct {
	Command string
	Args    []string
	Stderr  string
	Err     error
}

func (e *JJError) Error() string {
	cmd := e.Command
	if len(e.Args) > 0 {
		cmd = fmt.Sprintf("%s %s", e.Command, strings.Join(e.Args, " "))
	}
	if e.Stderr != "" {
		return fmt.Sprintf("jj command failed: %s\n%s", cmd, strings.TrimSpace(e.Stderr))
	}
	if e.Err != nil {
		return fmt.Sprintf("jj command failed: %s: %v", cmd, e.Err)
	}
	return fmt.Sprintf("jj command failed: %s", cmd)
}

// Hint returns a helpful suggestion for resolving this error.
func (e *JJError) Hint() string {
	stderr := strings.ToLower(e.Stderr)
	if strings.Contains(stderr, "no jj repo") {
		return "Run 'jj git init --colocate' in a git repository to create a jj workspace."
	}
	if strings.Contains(stderr, "not a valid revset") || strings.Contains(stderr, "revision") {
		return "Check that the bookmark or revision exists. Run 'jj bookmark list' to see available bookmarks."
	}
	if strings.Contains(stderr, "conflict") {
		return "Resolve conflicts with 'jj resolve' before continuing."
	}
	return ""
}

func (e *JJError) Unwrap() error {
	return e.Err
}

// GitHubError represents GitHub API errors.
type GitHubError struct {
	Operation  string
	StatusCode int
	Message    string
	Err        error
}

func (e *GitHubError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("GitHub API error during %s (HTTP %d): %s", e.Operation, e.StatusCode, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("GitHub API error during %s: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("GitHub API error during %s: %s", e.Operation, e.Message)
}

// Hint returns a helpful suggestion for resolving this error.
func (e *GitHubError) Hint() string {
	switch e.StatusCode {
	case 401:
		return "Your GitHub token may be invalid or expired. Run 'jj-stacked auth test' to verify, or 'gh auth login' to re-authenticate."
	case 403:
		if strings.Contains(strings.ToLower(e.Message), "rate limit") {
			return "GitHub API rate limit exceeded. Wait a few minutes and try again, or use a token with higher limits."
		}
		return "Your token may lack required permissions. Ensure it has 'repo' scope. Run 'gh auth login' to get a new token."
	case 404:
		return "The repository or resource was not found. Check the repository name and your access permissions."
	case 422:
		if strings.Contains(strings.ToLower(e.Message), "already exists") {
			return "A pull request for this branch already exists. The existing PR will be updated instead."
		}
		return "The request was invalid. Check the PR title, body, and branch names for issues."
	}
	return ""
}

func (e *GitHubError) Unwrap() error {
	return e.Err
}

// ValidationError represents input validation failures.
type ValidationError struct {
	Field   string
	Message string
	Hint    string // Optional hint for resolution
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// GetHint returns a helpful suggestion for resolving this error.
func (e *ValidationError) GetHint() string {
	if e.Hint != "" {
		return e.Hint
	}
	// Default hints based on field
	switch e.Field {
	case "bookmark":
		return "Run 'jj bookmark list' to see available bookmarks."
	case "change_id":
		return "Run 'jj log' to see available changes and their IDs."
	case "remote":
		return "Run 'jj git remote list' to see configured remotes."
	}
	return ""
}

func (e *ValidationError) Unwrap() error {
	return nil
}

// AuthError represents authentication failures.
type AuthError struct {
	Method  string // "gh_cli", "env_token", etc.
	Host    string // GitHub hostname
	Message string
	Err     error
}

func (e *AuthError) Error() string {
	host := e.Host
	if host == "" {
		host = "github.com"
	}
	if e.Method != "" {
		return fmt.Sprintf("authentication failed for %s (%s): %s", host, e.Method, e.Message)
	}
	return fmt.Sprintf("authentication failed for %s: %s", host, e.Message)
}

// Hint returns a helpful suggestion for resolving this error.
func (e *AuthError) Hint() string {
	host := e.Host
	if host == "" {
		host = "github.com"
	}

	if e.Method == "gh_cli" {
		if host == "github.com" {
			return "Run 'gh auth login' to authenticate with GitHub CLI."
		}
		return fmt.Sprintf("Run 'gh auth login --hostname %s' to authenticate with GitHub CLI.", host)
	}

	if host == "github.com" {
		return "Run 'jj-stacked auth help' for authentication setup instructions."
	}
	return fmt.Sprintf("Run 'jj-stacked auth help' for setup instructions. For GitHub Enterprise, use '--host %s'.", host)
}

func (e *AuthError) Unwrap() error {
	return e.Err
}

// BookmarkNotFoundError is returned when a requested bookmark doesn't exist.
type BookmarkNotFoundError struct {
	Bookmark           string
	AvailableBookmarks []string
}

func (e *BookmarkNotFoundError) Error() string {
	return fmt.Sprintf("bookmark '%s' not found", e.Bookmark)
}

// Hint returns a helpful suggestion for resolving this error.
func (e *BookmarkNotFoundError) Hint() string {
	if len(e.AvailableBookmarks) == 0 {
		return "Run 'jj bookmark list' to see available bookmarks."
	}
	if len(e.AvailableBookmarks) <= 5 {
		return fmt.Sprintf("Available bookmarks: %s", strings.Join(e.AvailableBookmarks, ", "))
	}
	return fmt.Sprintf("Available bookmarks include: %s (run 'jj bookmark list' for full list)",
		strings.Join(e.AvailableBookmarks[:5], ", "))
}

// Hinter is an interface for errors that provide resolution hints.
type Hinter interface {
	Hint() string
}

// FormatErrorWithHint formats an error message with a hint if available.
func FormatErrorWithHint(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	// Check if the error provides a hint
	var hinter Hinter
	if errors.As(err, &hinter) {
		hint := hinter.Hint()
		if hint != "" {
			msg = fmt.Sprintf("%s\n\nHint: %s", msg, hint)
		}
	}

	return msg
}

// IsJJError returns true if the error is a JJError.
func IsJJError(err error) bool {
	var jjErr *JJError
	return errors.As(err, &jjErr)
}

// IsGitHubError returns true if the error is a GitHubError.
func IsGitHubError(err error) bool {
	var ghErr *GitHubError
	return errors.As(err, &ghErr)
}

// IsValidationError returns true if the error is a ValidationError.
func IsValidationError(err error) bool {
	var valErr *ValidationError
	return errors.As(err, &valErr)
}

// IsAuthError returns true if the error is an AuthError.
func IsAuthError(err error) bool {
	var authErr *AuthError
	return errors.As(err, &authErr)
}

// AsJJError attempts to extract a JJError from an error chain.
func AsJJError(err error) (*JJError, bool) {
	var jjErr *JJError
	if errors.As(err, &jjErr) {
		return jjErr, true
	}
	return nil, false
}

// AsGitHubError attempts to extract a GitHubError from an error chain.
func AsGitHubError(err error) (*GitHubError, bool) {
	var ghErr *GitHubError
	if errors.As(err, &ghErr) {
		return ghErr, true
	}
	return nil, false
}

// AsAuthError attempts to extract an AuthError from an error chain.
func AsAuthError(err error) (*AuthError, bool) {
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return authErr, true
	}
	return nil, false
}
