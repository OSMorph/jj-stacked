package jjutils

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

var jjVersionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)(?:\.\d+)?`)

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

// Push pushes a bookmark to a remote. jj 0.36+ can mark a local bookmark as
// tracking a not-yet-created remote bookmark; older supported versions require
// --allow-new instead. jj 0.41 introduced the separate --remote tracking option.
func (j *jjFunctions) Push(ctx context.Context, remote, bookmark string) error {
	version, err := j.getVersion(ctx)
	if err != nil {
		return err
	}

	args := []string{"git", "push", "--remote", remote, "--bookmark", bookmark}
	switch {
	case version.atLeast(0, 41):
		trackArgs := []string{"bookmark", "track", bookmark, "--remote", remote}
		if _, err := j.exec.Run(ctx, j.jjCmd(), trackArgs...); err != nil {
			return &apperrors.JJError{
				Command: j.jjCmd(),
				Args:    trackArgs,
				Err:     err,
			}
		}
	case version.atLeast(0, 36):
		trackArgs := []string{"bookmark", "track", bookmark + "@" + remote}
		if _, err := j.exec.Run(ctx, j.jjCmd(), trackArgs...); err != nil {
			return &apperrors.JJError{
				Command: j.jjCmd(),
				Args:    trackArgs,
				Err:     err,
			}
		}
	default:
		args = append(args, "--allow-new")
	}

	_, err = j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}
	return nil
}

type jjVersion struct {
	major int
	minor int
}

func (v jjVersion) atLeast(major, minor int) bool {
	return v.major > major || v.major == major && v.minor >= minor
}

func (j *jjFunctions) getVersion(ctx context.Context) (jjVersion, error) {
	args := []string{"--version"}
	output, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return jjVersion{}, &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}

	match := jjVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return jjVersion{}, fmt.Errorf("could not parse jj version from %q", strings.TrimSpace(output))
	}

	major, err := strconv.Atoi(match[1])
	if err != nil {
		return jjVersion{}, fmt.Errorf("parse jj major version: %w", err)
	}
	minor, err := strconv.Atoi(match[2])
	if err != nil {
		return jjVersion{}, fmt.Errorf("parse jj minor version: %w", err)
	}

	return jjVersion{major: major, minor: minor}, nil
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
