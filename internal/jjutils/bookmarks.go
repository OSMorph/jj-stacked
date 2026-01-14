package jjutils

import (
	"context"
	"fmt"
	"strings"

	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
)

// ListBookmarks returns all local bookmarks.
func (j *jjFunctions) ListBookmarks(ctx context.Context) ([]Bookmark, error) {
	bookmarks, err := j.listLocalBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	remoteBookmarks, err := j.getRemoteBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	// Merge remote tracking info into bookmark list
	for i, bm := range bookmarks {
		if remote, ok := remoteBookmarks[bm.Name]; ok {
			bookmarks[i].HasRemote = true
			bookmarks[i].RemoteName = remote.Remote
			if remote.ChangeID == bm.ChangeID {
				bookmarks[i].IsSynced = true
			}
		}
	}

	return bookmarks, nil
}

// ListUserBookmarks returns bookmarks that are not on trunk.
// AIDEV-NOTE: We list all bookmarks first, then filter out those on trunk.
// This is more robust than using mine() ~ trunk() which can miss bookmarks
// after merges when commits become hidden/obsolete.
func (j *jjFunctions) ListUserBookmarks(ctx context.Context) ([]Bookmark, error) {
	// Get all local bookmarks
	allBookmarks, err := j.ListBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	if len(allBookmarks) == 0 {
		return nil, nil
	}

	// Get trunk commit ID to filter out trunk bookmark
	trunkEntries, err := j.GetLog(ctx, "trunk()", 1)
	if err != nil {
		return nil, err
	}

	var trunkChangeID string
	if len(trunkEntries) > 0 {
		trunkChangeID = trunkEntries[0].ChangeID
	}

	// Filter bookmarks: exclude those pointing at trunk or that are trunk-tracking bookmarks
	var userBookmarks []Bookmark
	for _, bm := range allBookmarks {
		// Skip if this bookmark IS trunk
		if bm.ChangeID == trunkChangeID {
			continue
		}

		// Skip common trunk branch names
		if bm.Name == "main" || bm.Name == "master" || bm.Name == "trunk" {
			continue
		}

		// First, verify the bookmark's change still exists
		// This handles cases where bookmarks point to abandoned/obsolete changes
		_, err = j.GetChange(ctx, bm.ChangeID)
		if err != nil {
			// Change doesn't exist (abandoned/hidden) - skip this bookmark
			continue
		}

		// Check if this bookmark's change is an ancestor of trunk (already merged)
		// Use a revset to check: if the bookmark's change is in trunk's ancestors, skip it
		revset := fmt.Sprintf("%s & ::trunk()", bm.ChangeID)
		entries, err := j.GetLog(ctx, revset, 1)
		if err != nil {
			// Query failed - skip this bookmark to be safe
			continue
		}

		// If no entries returned, the change is NOT in trunk's ancestors - include it
		if len(entries) == 0 {
			userBookmarks = append(userBookmarks, bm)
		}
		// If entries returned, the change IS in trunk's ancestors - skip it (already merged)
	}

	return userBookmarks, nil
}

// GetBookmarksForChange returns all bookmarks pointing at a specific change.
func (j *jjFunctions) GetBookmarksForChange(ctx context.Context, changeID string) ([]Bookmark, error) {
	// Get the change to find its bookmarks
	entry, err := j.GetChange(ctx, changeID)
	if err != nil {
		return nil, err
	}

	if len(entry.LocalBookmarks) == 0 {
		return nil, nil
	}

	// Get full bookmark info
	allBookmarks, err := j.ListBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	// Build map for quick lookup
	bookmarkMap := make(map[string]Bookmark)
	for _, bm := range allBookmarks {
		bookmarkMap[bm.Name] = bm
	}

	// Return bookmarks for this change
	var result []Bookmark
	for _, name := range entry.LocalBookmarks {
		if bm, ok := bookmarkMap[name]; ok {
			result = append(result, bm)
		}
	}

	return result, nil
}

// GetBookmarkByName returns a specific bookmark by name.
func (j *jjFunctions) GetBookmarkByName(ctx context.Context, name string) (*Bookmark, error) {
	bookmarks, err := j.ListBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	for _, bm := range bookmarks {
		if bm.Name == name {
			return &bm, nil
		}
	}

	return nil, &apperrors.ValidationError{
		Field:   "bookmark",
		Message: fmt.Sprintf("bookmark %q not found", name),
	}
}

// listLocalBookmarks returns bookmarks using the template (no remote tracking info).
func (j *jjFunctions) listLocalBookmarks(ctx context.Context) ([]Bookmark, error) {
	args := []string{"bookmark", "list", "-T", bookmarkTemplate}
	output, err := j.exec.Run(ctx, j.jjCmd(), args...)
	if err != nil {
		return nil, &apperrors.JJError{
			Command: j.jjCmd(),
			Args:    args,
			Err:     err,
		}
	}

	return parseBookmarks(output)
}

type remoteBookmarkInfo struct {
	ChangeID string
	Remote   string
}

// getRemoteBookmarks returns a map of bookmark name -> remote info from remote bookmarks.
func (j *jjFunctions) getRemoteBookmarks(ctx context.Context) (map[string]remoteBookmarkInfo, error) {
	entries, err := j.GetLog(ctx, "remote_bookmarks()", 0)
	if err != nil {
		return nil, err
	}

	result := make(map[string]remoteBookmarkInfo)
	for _, entry := range entries {
		for _, rb := range entry.RemoteBookmarks {
			// remote bookmarks are in the form name@remote
			parts := strings.SplitN(rb, "@", 2)
			if len(parts) != 2 {
				continue
			}
			name := parts[0]
			remote := parts[1]
			// Keep first occurrence (most recent in log is fine)
			if _, exists := result[name]; !exists {
				result[name] = remoteBookmarkInfo{
					ChangeID: entry.ChangeID,
					Remote:   remote,
				}
			}
		}
	}

	return result, nil
}
