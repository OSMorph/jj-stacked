package jjutils

import (
	"context"
	"strings"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// ListRemotes returns all configured git remotes.
func (j *jjFunctions) ListRemotes(ctx context.Context) ([]Remote, error) {
	output, err := j.exec.Run(ctx, j.jjCmd(), "git", "remote", "list")
	if err != nil {
		return nil, &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    []string{"git", "remote", "list"},
			Err:     err,
		}
	}

	return parseRemoteList(output), nil
}

// Fetch fetches from a specific remote.
func (j *jjFunctions) Fetch(ctx context.Context, remote string) error {
	args := []string{"git", "fetch", "--remote", remote}
	_, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}
	return nil
}

// FetchAllRemotes fetches from all configured remotes.
func (j *jjFunctions) FetchAllRemotes(ctx context.Context) error {
	args := []string{"git", "fetch", "--all-remotes"}
	_, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}
	return nil
}

// Push pushes a bookmark to a remote.
// Uses --allow-new to allow creating new branches on the remote.
func (j *jjFunctions) Push(ctx context.Context, remote, bookmark string) error {
	args := []string{"git", "push", "--remote", remote, "--bookmark", bookmark, "--allow-new"}
	_, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}
	return nil
}

// parseRemoteList parses the output of `jj git remote list`.
// Each line is: "remotename url"
func parseRemoteList(output string) []Remote {
	var remotes []Remote
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remotes = append(remotes, Remote{
				Name: parts[0],
				URL:  parts[1],
			})
		}
	}

	return remotes
}
