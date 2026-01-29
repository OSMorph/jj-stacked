// Package github provides a wrapper around go-github for GitHub API operations.
package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v67/github"
	"golang.org/x/oauth2"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// GitHubClient is the interface for GitHub API operations.
// AIDEV-NOTE: Supports both GitHub.com and GitHub Enterprise instances.
type GitHubClient interface {
	// PR operations
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error)
	CreatePullRequest(ctx context.Context, owner, repo string, req *CreatePRRequest) (*PullRequest, error)
	UpdatePullRequest(ctx context.Context, owner, repo string, number int, req *UpdatePRRequest) (*PullRequest, error)
	ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*PullRequest, error)
	FindPRByHead(ctx context.Context, owner, repo, head string) (*PullRequest, error)
	FindPRByHeadAllStates(ctx context.Context, owner, repo, head string) (*PullRequest, error)

	// Comment operations
	CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*Comment, error)
	UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) (*Comment, error)
	ListComments(ctx context.Context, owner, repo string, prNumber int) ([]*Comment, error)

	// Repository info
	GetDefaultBranch(ctx context.Context, owner, repo string) (string, error)

	// User info
	GetAuthenticatedUser(ctx context.Context) (string, error)

	// Host info
	Host() string
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number    int
	Title     string
	Body      string
	State     string
	URL       string
	Base      string
	Head      string
	Author    string // GitHub username of PR author
	Merged    bool
	Mergeable *bool
	MergedAt  *time.Time
	MergedBy  string
}

// CreatePRRequest represents a request to create a pull request.
type CreatePRRequest struct {
	Title string
	Body  string
	Head  string
	Base  string
	Draft bool
}

// UpdatePRRequest represents a request to update a pull request.
type UpdatePRRequest struct {
	Title *string
	Body  *string
	Base  *string
	State *string
}

// Comment represents a GitHub issue comment.
type Comment struct {
	ID   int64
	Body string
	User string
}

// ClientOptions configures the GitHub client.
type ClientOptions struct {
	Token      string
	Hostname   string // "github.com" or GHE hostname
	APIBaseURL string // Optional override
}

// client implements GitHubClient.
type client struct {
	gh       *github.Client
	hostname string
}

// NewClient creates a GitHub client for the specified host.
func NewClient(opts ClientOptions) (GitHubClient, error) {
	if opts.Token == "" {
		return nil, &apperrors.GitHubError{
			Operation: "create_client",
			Message:   "token is required",
		}
	}

	hostname := opts.Hostname
	if hostname == "" {
		hostname = "github.com"
	}

	// Create OAuth2 transport
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: opts.Token})
	tc := oauth2.NewClient(context.Background(), ts)

	var gh *github.Client
	var err error

	if hostname == "github.com" {
		gh = github.NewClient(tc)
	} else {
		// GitHub Enterprise
		baseURL := opts.APIBaseURL
		if baseURL == "" {
			baseURL = fmt.Sprintf("https://%s/api/v3/", hostname)
		}
		// Ensure trailing slash
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		uploadURL := fmt.Sprintf("https://%s/api/uploads/", hostname)

		gh, err = github.NewClient(tc).WithEnterpriseURLs(baseURL, uploadURL)
		if err != nil {
			return nil, &apperrors.GitHubError{
				Operation: "create_client",
				Message:   fmt.Sprintf("failed to configure GHE client: %v", err),
			}
		}
	}

	return &client{
		gh:       gh,
		hostname: hostname,
	}, nil
}

func (c *client) Host() string {
	return c.hostname
}

func (c *client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, c.wrapError("get_pull_request", resp, err)
	}
	return convertPR(pr), nil
}

func (c *client) CreatePullRequest(ctx context.Context, owner, repo string, req *CreatePRRequest) (*PullRequest, error) {
	newPR := &github.NewPullRequest{
		Title: github.String(req.Title),
		Body:  github.String(req.Body),
		Head:  github.String(req.Head),
		Base:  github.String(req.Base),
		Draft: github.Bool(req.Draft),
	}

	pr, resp, err := c.gh.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return nil, c.wrapError("create_pull_request", resp, err)
	}
	return convertPR(pr), nil
}

func (c *client) UpdatePullRequest(ctx context.Context, owner, repo string, number int, req *UpdatePRRequest) (*PullRequest, error) {
	update := &github.PullRequest{}
	if req.Title != nil {
		update.Title = req.Title
	}
	if req.Body != nil {
		update.Body = req.Body
	}
	if req.Base != nil {
		update.Base = &github.PullRequestBranch{Ref: req.Base}
	}
	if req.State != nil {
		update.State = req.State
	}

	pr, resp, err := c.gh.PullRequests.Edit(ctx, owner, repo, number, update)
	if err != nil {
		return nil, c.wrapError("update_pull_request", resp, err)
	}
	return convertPR(pr), nil
}

func (c *client) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]*PullRequest, error) {
	var allPRs []*PullRequest

	opts := &github.PullRequestListOptions{
		State: "open",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		prs, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return nil, c.wrapError("list_pull_requests", resp, err)
		}

		for _, pr := range prs {
			allPRs = append(allPRs, convertPR(pr))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allPRs, nil
}

func (c *client) FindPRByHead(ctx context.Context, owner, repo, head string) (*PullRequest, error) {
	opts := &github.PullRequestListOptions{
		State: "open",
		Head:  fmt.Sprintf("%s:%s", owner, head),
		ListOptions: github.ListOptions{
			PerPage: 1,
		},
	}

	prs, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
	if err != nil {
		return nil, c.wrapError("find_pr_by_head", resp, err)
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return convertPR(prs[0]), nil
}

func (c *client) FindPRByHeadAllStates(ctx context.Context, owner, repo, head string) (*PullRequest, error) {
	// Search in all states to find merged/closed PRs
	opts := &github.PullRequestListOptions{
		State: "all",
		Head:  fmt.Sprintf("%s:%s", owner, head),
		ListOptions: github.ListOptions{
			PerPage: 10, // Get a few to find the most recent
		},
	}

	prs, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
	if err != nil {
		return nil, c.wrapError("find_pr_by_head_all_states", resp, err)
	}

	if len(prs) == 0 {
		return nil, nil
	}

	// Return the most recent PR (first in the list)
	return convertPR(prs[0]), nil
}

func (c *client) CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*Comment, error) {
	comment := &github.IssueComment{
		Body: github.String(body),
	}

	created, resp, err := c.gh.Issues.CreateComment(ctx, owner, repo, prNumber, comment)
	if err != nil {
		return nil, c.wrapError("create_comment", resp, err)
	}

	return &Comment{
		ID:   created.GetID(),
		Body: created.GetBody(),
		User: created.GetUser().GetLogin(),
	}, nil
}

func (c *client) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) (*Comment, error) {
	comment := &github.IssueComment{
		Body: github.String(body),
	}

	updated, resp, err := c.gh.Issues.EditComment(ctx, owner, repo, commentID, comment)
	if err != nil {
		return nil, c.wrapError("update_comment", resp, err)
	}

	return &Comment{
		ID:   updated.GetID(),
		Body: updated.GetBody(),
		User: updated.GetUser().GetLogin(),
	}, nil
}

func (c *client) ListComments(ctx context.Context, owner, repo string, prNumber int) ([]*Comment, error) {
	var allComments []*Comment

	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		comments, resp, err := c.gh.Issues.ListComments(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, c.wrapError("list_comments", resp, err)
		}

		for _, comment := range comments {
			allComments = append(allComments, &Comment{
				ID:   comment.GetID(),
				Body: comment.GetBody(),
				User: comment.GetUser().GetLogin(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allComments, nil
}

func (c *client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	repository, resp, err := c.gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", c.wrapError("get_default_branch", resp, err)
	}

	return repository.GetDefaultBranch(), nil
}

func (c *client) GetAuthenticatedUser(ctx context.Context) (string, error) {
	user, resp, err := c.gh.Users.Get(ctx, "")
	if err != nil {
		return "", c.wrapError("get_authenticated_user", resp, err)
	}

	return user.GetLogin(), nil
}

// wrapError wraps a GitHub API error with context.
func (c *client) wrapError(operation string, resp *github.Response, err error) error {
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode

		// Handle rate limiting
		if statusCode == http.StatusForbidden && resp.Rate.Remaining == 0 {
			resetTime := resp.Rate.Reset.Time
			return &apperrors.GitHubError{
				Operation:  operation,
				StatusCode: statusCode,
				Message:    fmt.Sprintf("rate limit exceeded, resets at %s", resetTime.Format(time.RFC3339)),
				Err:        err,
			}
		}
	}

	return &apperrors.GitHubError{
		Operation:  operation,
		StatusCode: statusCode,
		Message:    err.Error(),
		Err:        err,
	}
}

// convertPR converts a github.PullRequest to our PullRequest type.
func convertPR(pr *github.PullRequest) *PullRequest {
	result := &PullRequest{
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		Body:      pr.GetBody(),
		State:     pr.GetState(),
		URL:       pr.GetHTMLURL(),
		Base:      pr.GetBase().GetRef(),
		Head:      pr.GetHead().GetRef(),
		Merged:    pr.GetMerged(),
		Mergeable: pr.Mergeable,
	}

	if pr.User != nil {
		result.Author = pr.User.GetLogin()
	}

	if pr.MergedAt != nil {
		t := pr.MergedAt.Time
		result.MergedAt = &t
	}

	if pr.MergedBy != nil {
		result.MergedBy = pr.MergedBy.GetLogin()
	}

	return result
}
