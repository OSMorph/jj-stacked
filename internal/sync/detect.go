package sync

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: DetectMergedBookmarks queries GitHub to find which local bookmarks
// have corresponding PRs that have been merged. This is the first step in the
// sync process - identifying what work has been completed upstream.

// DetectMergedBookmarks finds bookmarks whose PRs have been merged on GitHub.
// It queries the GitHub API for each bookmark to check if its PR was merged.
//
// Parameters:
//   - ctx: context for cancellation
//   - bookmarks: list of local bookmarks to check
//   - gh: GitHub client for API calls
//   - owner: repository owner
//   - repo: repository name
//
// Returns merged bookmarks and any errors encountered.
func DetectMergedBookmarks(
	ctx context.Context,
	bookmarks []jjutils.Bookmark,
	gh github.GitHubClient,
	owner, repo string,
) ([]MergedBookmark, []error) {
	var merged []MergedBookmark
	var errors []error

	for _, bm := range bookmarks {
		// Find PR by head branch (bookmark name)
		pr, err := gh.FindPRByHeadAllStates(ctx, owner, repo, bm.Name)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to check PR for bookmark %s: %w", bm.Name, err))
			continue
		}

		// No PR found for this bookmark
		if pr == nil {
			continue
		}

		// Check if PR was merged (not just closed)
		if pr.Merged && pr.MergedAt != nil {
			merged = append(merged, MergedBookmark{
				Name:     bm.Name,
				ChangeID: bm.ChangeID,
				CommitID: bm.CommitID,
				PRNumber: pr.Number,
				PRTitle:  pr.Title,
				MergedAt: *pr.MergedAt,
				MergedBy: pr.MergedBy,
			})
		}
	}

	return merged, errors
}

// FilterMergedFromBottom returns only the merged bookmarks that are at the
// bottom of their stack (contiguous from trunk). This ensures we only abandon
// bookmarks that have been properly merged in order.
//
// For example, if a stack is: trunk -> A -> B -> C
// And B was merged but A wasn't, we should NOT include B because it would
// leave a gap in the stack.
func FilterMergedFromBottom(
	merged []MergedBookmark,
	graph *jjutils.ChangeGraph,
) (contiguousMerged []MergedBookmark, warnings []string) {
	// Build a set of merged bookmark names for quick lookup
	mergedSet := make(map[string]MergedBookmark)
	for _, m := range merged {
		mergedSet[m.Name] = m
	}

	// For each stack, find contiguous merged bookmarks from the root
	for _, stack := range graph.Stacks {
		for _, segment := range stack.Segments {
			bmName := segment.Bookmark.Name

			if m, isMerged := mergedSet[bmName]; isMerged {
				// This bookmark is merged, add it to contiguous list
				contiguousMerged = append(contiguousMerged, m)
			} else {
				// First non-merged bookmark - stop processing this stack
				// Any merged bookmarks after this point are "gaps"
				for i := len(stack.Segments) - 1; i >= 0; i-- {
					laterBm := stack.Segments[i].Bookmark.Name
					if _, isLaterMerged := mergedSet[laterBm]; isLaterMerged {
						// Check if we haven't already added it
						found := false
						for _, cm := range contiguousMerged {
							if cm.Name == laterBm {
								found = true
								break
							}
						}
						if !found {
							warnings = append(warnings,
								fmt.Sprintf("bookmark %s was merged out of order (before %s)", laterBm, bmName))
						}
					}
				}
				break
			}
		}
	}

	return contiguousMerged, warnings
}

// GetRemainingBookmarks returns bookmarks that are not in the merged list.
func GetRemainingBookmarks(
	allBookmarks []jjutils.Bookmark,
	merged []MergedBookmark,
) []string {
	mergedSet := make(map[string]bool)
	for _, m := range merged {
		mergedSet[m.Name] = true
	}

	var remaining []string
	for _, bm := range allBookmarks {
		if !mergedSet[bm.Name] {
			remaining = append(remaining, bm.Name)
		}
	}

	return remaining
}
