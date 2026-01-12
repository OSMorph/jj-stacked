package jjutils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// AIDEV-NOTE: These templates produce JSON output from jj commands.
// The templates use jj's template language with .escape_json() for proper escaping.
// For jj 0.27+, templates should use string concatenation rather than {interpolation} at top level.

// logEntryTemplate is the jj template for getting change information.
// Produces one JSON object per line.
// Note: jj 0.27+ types (Email, RefName, etc.) don't have escape_json, so we stringify directly.
// This may cause issues with special characters in names/emails - we handle escaping in Go.
const logEntryTemplate = `concat(
  '{"commit_id":"', commit_id.short(), '",',
  '"change_id":"', change_id.short(), '",',
  '"author_name":"', author.name(), '",',
  '"author_email":"', author.email(), '",',
  '"description_first_line":"', description.first_line(), '",',
  '"description":"', description, '",',
  '"parents":[', parents.map(|p| '"' ++ p.commit_id().short() ++ '"').join(","), '],',
  '"local_bookmarks":[', local_bookmarks.map(|b| '"' ++ b ++ '"').join(","), '],',
  '"remote_bookmarks":[', remote_bookmarks.map(|b| '"' ++ b ++ '"').join(","), '],',
  '"is_working_copy":', if(current_working_copy, "true", "false"), ',',
  '"is_empty":', if(empty, "true", "false"), ',',
  '"conflict":', if(conflict, "true", "false"),
  '}
'
)`

// bookmarkTemplate is the jj template for getting bookmark information.
// For jj 0.27+, bookmark list uses self.normal_target() to get commit info.
// Note: synced() doesn't exist in jj 0.27+, so we set is_synced based on whether remote exists.
const bookmarkTemplate = `if(
  self.normal_target(),
  concat(
    '{"name":"', name, '",',
    '"commit_id":"', self.normal_target().commit_id().short(), '",',
    '"change_id":"', self.normal_target().change_id().short(), '",',
    '"has_remote":', if(self.remote(), "true", "false"), ',',
    '"is_synced":', if(self.remote(), "true", "false"),
    '}
'
  ),
  ""
)`

// GetRepoRoot returns the repository root directory.
func (j *jjFunctions) GetRepoRoot(ctx context.Context) (string, error) {
	output, err := j.exec.Run(ctx, j.jjCmd(), "root")
	if err != nil {
		return "", &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    []string{"root"},
			Err:     err,
		}
	}
	return strings.TrimSpace(output), nil
}

// GetDefaultBranch returns the trunk branch name (main, master, or trunk).
// Detection priority:
// 1. TRUNK_BRANCH environment variable (if set)
// 2. jj trunk() revset (respects revset-aliases in jj config)
// 3. Common branch names: main, master, trunk
// 4. First bookmark on trunk() commit
// 5. Fallback to "main"
func (j *jjFunctions) GetDefaultBranch(ctx context.Context) (string, error) {
	// Check environment variable override first
	if envTrunk := os.Getenv("TRUNK_BRANCH"); envTrunk != "" {
		return envTrunk, nil
	}

	// Try to resolve trunk() and get its bookmarks
	output, err := j.exec.Run(ctx, j.jjCmd(), "log", "--no-graph", "-r", "trunk()", "--limit", "1", "-T", `local_bookmarks.join(",")`)
	if err != nil {
		// Fall back to common defaults
		return "main", nil
	}

	branches := strings.Split(strings.TrimSpace(output), ",")
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		// Clean bookmark markers (* for dirty, @ for remote)
		branch = cleanBookmarkMarkers(branch)
		if branch == "main" || branch == "master" || branch == "trunk" {
			return branch, nil
		}
	}

	// If trunk() resolved but no standard branch name, use first bookmark
	if len(branches) > 0 && branches[0] != "" {
		return cleanBookmarkMarkers(branches[0]), nil
	}

	return "main", nil
}

// cleanBookmarkMarkers removes jj display markers from bookmark names.
func cleanBookmarkMarkers(name string) string {
	name = strings.TrimSuffix(name, "*")
	if idx := strings.Index(name, "@"); idx > 0 {
		name = name[:idx]
	}
	return name
}

// TrunkInfo contains information about the trunk branch.
type TrunkInfo struct {
	BranchName string // The branch name (main, master, trunk, etc.)
	ChangeID   string // The change ID of the trunk commit
	CommitID   string // The commit ID of the trunk commit
}

// GetTrunkInfo returns detailed information about the trunk branch.
// This is useful for sync operations that need to know both the branch name
// and the current trunk commit.
func (j *jjFunctions) GetTrunkInfo(ctx context.Context) (*TrunkInfo, error) {
	// Get the branch name first
	branchName, err := j.GetDefaultBranch(ctx)
	if err != nil {
		return nil, err
	}

	// Get trunk commit info
	entries, err := j.GetLog(ctx, "trunk()", 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get trunk info: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("trunk() returned no commits")
	}

	return &TrunkInfo{
		BranchName: branchName,
		ChangeID:   entries[0].ChangeID,
		CommitID:   entries[0].CommitID,
	}, nil
}

// GetLog retrieves changes matching the revset.
func (j *jjFunctions) GetLog(ctx context.Context, revset string, limit int) ([]LogEntry, error) {
	args := []string{"log", "--no-graph", "-r", revset, "-T", logEntryTemplate}
	if limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", limit))
	}

	output, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return nil, &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}

	return parseLogEntries(output)
}

// GetChange retrieves a single change by change ID.
func (j *jjFunctions) GetChange(ctx context.Context, changeID string) (*LogEntry, error) {
	entries, err := j.GetLog(ctx, changeID, 1)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, &apperrors.ValidationError{
			Field:   "change_id",
			Message: fmt.Sprintf("change %s not found", changeID),
		}
	}
	return &entries[0], nil
}

// GetChangesInRange retrieves changes between two revisions.
// Uses the revset `from::to` to get all changes in the range.
func (j *jjFunctions) GetChangesInRange(ctx context.Context, from, to string) ([]LogEntry, error) {
	revset := fmt.Sprintf("%s::%s", from, to)
	return j.GetLog(ctx, revset, 0)
}

// parseLogEntries parses the JSON output from jj log.
func parseLogEntries(output string) ([]LogEntry, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var entries []LogEntry
	lines := strings.Split(output, "\n")

	// Accumulate JSON across lines (each entry ends with "}")
	var jsonBuf strings.Builder
	braceCount := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		jsonBuf.WriteString(line)

		// Count braces to find complete JSON objects
		for _, ch := range line {
			if ch == '{' {
				braceCount++
			} else if ch == '}' {
				braceCount--
			}
		}

		if braceCount == 0 && jsonBuf.Len() > 0 {
			var entry LogEntry
			if err := json.Unmarshal([]byte(jsonBuf.String()), &entry); err != nil {
				return nil, fmt.Errorf("failed to parse log entry: %w\njson: %s", err, jsonBuf.String())
			}
			entries = append(entries, entry)
			jsonBuf.Reset()
		}
	}

	return entries, nil
}

// parseBookmarks parses the JSON output from jj bookmark list.
func parseBookmarks(output string) ([]Bookmark, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var bookmarks []Bookmark
	var jsonBuf strings.Builder
	braceCount := 0

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		jsonBuf.WriteString(line)

		for _, ch := range line {
			if ch == '{' {
				braceCount++
			} else if ch == '}' {
				braceCount--
			}
		}

		if braceCount == 0 && jsonBuf.Len() > 0 {
			var bookmark Bookmark
			if err := json.Unmarshal([]byte(jsonBuf.String()), &bookmark); err != nil {
				return nil, fmt.Errorf("failed to parse bookmark: %w\njson: %s", err, jsonBuf.String())
			}
			bookmarks = append(bookmarks, bookmark)
			jsonBuf.Reset()
		}
	}

	return bookmarks, nil
}

// Abandon abandons (removes) one or more changes identified by a revset.
// This is used by the sync command to remove merged changes.
func (j *jjFunctions) Abandon(ctx context.Context, revset string) error {
	_, err := j.exec.Run(ctx, j.jjCmd(), "abandon", revset)
	if err != nil {
		return &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    []string{"abandon", revset},
			Err:     err,
		}
	}
	return nil
}

// Rebase rebases changes onto a new destination.
// source is a revset identifying what to rebase.
// destination is the new parent (typically trunk branch name or "trunk()").
func (j *jjFunctions) Rebase(ctx context.Context, source, destination string) error {
	args := []string{"rebase", "-s", source, "-d", destination}
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

// HasConflicts checks if the working copy has conflicts.
// Returns true if conflicts are present.
func (j *jjFunctions) HasConflicts(ctx context.Context) (bool, error) {
	output, err := j.exec.Run(ctx, j.jjCmd(), "log", "-r", "@", "--no-graph", "-T", `if(conflict, "true", "false")`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "true", nil
}
