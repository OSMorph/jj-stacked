package jjutils

import (
	"context"
	"fmt"
	"os"
	"strings"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// AIDEV-NOTE: These templates produce delimited output from jj commands.
// We use ASCII delimiters (0x1E record separator, 0x1F unit separator) to avoid
// issues with special characters in commit messages, author names, etc.
// JSON is constructed in Go where we have proper escaping control.

// Delimiters for parsing jj output - these are control characters unlikely to appear in content
const (
	recordSeparator = "\x1e" // ASCII Record Separator
	fieldSeparator  = "\x1f" // ASCII Unit Separator
)

// logEntryTemplate is the jj template for getting change information.
// Produces delimited output: fields separated by 0x1F, records by 0x1E.
// Field order: commit_id, change_id, author_name, author_email, description_first_line,
//
//	description, parents, local_bookmarks, remote_bookmarks, is_working_copy, is_empty, conflict
//
// Note: jj only interprets escape sequences in double quotes, so we use \"\\x1f\" for delimiters.
const logEntryTemplate = "concat(" +
	"commit_id.short(), \"\\x1f\"," +
	"change_id.short(), \"\\x1f\"," +
	"author.name(), \"\\x1f\"," +
	"author.email(), \"\\x1f\"," +
	"description.first_line(), \"\\x1f\"," +
	"description, \"\\x1f\"," +
	"parents.map(|p| p.commit_id().short()).join(\",\"), \"\\x1f\"," +
	"local_bookmarks.join(\",\"), \"\\x1f\"," +
	"remote_bookmarks.join(\",\"), \"\\x1f\"," +
	"if(current_working_copy, \"true\", \"false\"), \"\\x1f\"," +
	"if(empty, \"true\", \"false\"), \"\\x1f\"," +
	"if(conflict, \"true\", \"false\")," +
	"\"\\x1e\"" +
	")"

// bookmarkTemplate is the jj template for getting bookmark information.
// For jj 0.27+, bookmark list uses self.normal_target() to get commit info.
// We keep has_remote/is_synced as placeholders (computed in Go by comparing
// local bookmarks to remote bookmarks from jj log).
// Produces delimited output: fields separated by 0x1F, records by 0x1E.
// Field order: name, commit_id, change_id, has_remote, is_synced
// Note: jj only interprets escape sequences in double quotes, so we use \"\\x1f\" for delimiters.
const bookmarkTemplate = "if(" +
	"self.normal_target()," +
	"concat(" +
	"name, \"\\x1f\"," +
	"self.normal_target().commit_id().short(), \"\\x1f\"," +
	"self.normal_target().change_id().short(), \"\\x1f\"," +
	"\"false\", \"\\x1f\"," + // has_remote placeholder
	"\"false\"," + // is_synced placeholder
	"\"\\x1e\"" +
	")," +
	"\"\"" +
	")"

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

// parseLogEntries parses the delimited output from jj log.
// Format: fields separated by 0x1F, records separated by 0x1E.
func parseLogEntries(output string) ([]LogEntry, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var entries []LogEntry
	records := strings.Split(output, recordSeparator)

	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.Split(record, fieldSeparator)
		if len(fields) < 12 {
			return nil, fmt.Errorf("failed to parse log entry: expected 12 fields, got %d", len(fields))
		}

		// Parse comma-separated lists (parents, local_bookmarks, remote_bookmarks)
		var parents []string
		if fields[6] != "" {
			parents = strings.Split(fields[6], ",")
		}
		var localBookmarks []string
		if fields[7] != "" {
			localBookmarks = strings.Split(fields[7], ",")
		}
		var remoteBookmarks []string
		if fields[8] != "" {
			remoteBookmarks = strings.Split(fields[8], ",")
		}

		entry := LogEntry{
			CommitID:             fields[0],
			ChangeID:             fields[1],
			AuthorName:           fields[2],
			AuthorEmail:          fields[3],
			DescriptionFirstLine: fields[4],
			Description:          fields[5],
			Parents:              parents,
			LocalBookmarks:       localBookmarks,
			RemoteBookmarks:      remoteBookmarks,
			IsWorkingCopy:        fields[9] == "true",
			IsEmpty:              fields[10] == "true",
			Conflict:             fields[11] == "true",
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// parseBookmarks parses the delimited output from jj bookmark list.
// Format: fields separated by 0x1F, records separated by 0x1E.
func parseBookmarks(output string) ([]Bookmark, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var bookmarks []Bookmark
	records := strings.Split(output, recordSeparator)

	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.Split(record, fieldSeparator)
		if len(fields) < 5 {
			return nil, fmt.Errorf("failed to parse bookmark: expected 5 fields, got %d", len(fields))
		}

		bookmark := Bookmark{
			Name:      fields[0],
			CommitID:  fields[1],
			ChangeID:  fields[2],
			HasRemote: fields[3] == "true",
			IsSynced:  fields[4] == "true",
		}
		bookmarks = append(bookmarks, bookmark)
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

// DeleteBookmark removes a local bookmark without rewriting its target change.
func (j *jjFunctions) DeleteBookmark(ctx context.Context, name string) error {
	args := []string{"bookmark", "delete", name}
	_, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{Command: j.jjCmd(), Args: args, Err: err}
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
	output, err := j.exec.Run(ctx, j.jjCmd(), "log", "-r", "conflicts()", "--no-graph", "-T", `"true\n"`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// GetConflictFiles returns paths reported by jj resolve --list.
func (j *jjFunctions) GetConflictFiles(ctx context.Context) ([]string, error) {
	output, err := j.exec.Run(ctx, j.jjCmd(), "resolve", "--list")
	if err != nil {
		if strings.Contains(err.Error(), "No conflicts found") {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// IsAncestor reports whether ancestor is in descendant's ancestry.
func (j *jjFunctions) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	revset := fmt.Sprintf("%s & ::%s", ancestor, descendant)
	entries, err := j.GetLog(ctx, revset, 1)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// GetOperationID returns the current jj operation ID.
func (j *jjFunctions) GetOperationID(ctx context.Context) (string, error) {
	output, err := j.exec.Run(ctx, j.jjCmd(), "op", "log", "--no-graph", "--limit", "1", "-T", `self.id() ++ "\n"`)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(output)
	if id == "" {
		return "", fmt.Errorf("jj returned an empty operation ID")
	}
	return id, nil
}

// RestoreOperation restores repository state to an earlier jj operation.
func (j *jjFunctions) RestoreOperation(ctx context.Context, operationID string) error {
	args := []string{"op", "restore", operationID}
	_, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return &apperrors.JJError{Command: j.jjCmd(), Args: args, Err: err}
	}
	return nil
}
