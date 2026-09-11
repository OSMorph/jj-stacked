// Package status implements a noninteractive overview of local bookmark stacks.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/commands/common"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/repo"
)

type row struct {
	Bookmark         string `json:"bookmark"`
	CommitID         string `json:"commit_id"`
	Base             string `json:"base"`
	Changes          int    `json:"changes"`
	NeedsPush        bool   `json:"needs_push"`
	BookmarkConflict bool   `json:"bookmark_conflict"`
	Conflict         bool   `json:"conflict"`
	Divergent        bool   `json:"divergent"`
	Excluded         bool   `json:"excluded"`
	PRNumber         int    `json:"pr_number,omitempty"`
	PRState          string `json:"pr_state,omitempty"`
	PRURL            string `json:"pr_url,omitempty"`
	PRBase           string `json:"pr_base,omitempty"`
	CleanupReason    string `json:"cleanup_reason,omitempty"`
}
type report struct {
	Remote        string `json:"remote"`
	Trunk         string `json:"trunk"`
	Fetched       bool   `json:"fetched"`
	GitHubChecked bool   `json:"github_checked"`
	Bookmarks     []row  `json:"bookmarks"`
}

// NewCommand creates status. GitHub lookups and fetching are explicit options.
func NewCommand() *cobra.Command {
	var remote string
	var asJSON, withGitHub, fetch bool
	cmd := &cobra.Command{Use: "status", Short: "Show a compact bookmark and PR status table", Long: `Show local bookmark stacks without authentication or network requests.
Remote status uses existing tracking data. Add --fetch to update the selected
remote, or --github to include PR state, base, URL, and merged cleanup evidence.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		opts := repo.RepoContextOptions{Remote: remote, Logger: common.NewLogger(common.Debug(cmd))}
		var repository *repo.RepoContext
		var err error
		if withGitHub {
			repository, err = repo.NewRepoContext(cmd.Context(), opts)
		} else {
			repository, err = repo.Discover(cmd.Context(), opts)
		}
		if err != nil {
			return err
		}
		if fetch {
			if err := repository.JJ.Fetch(cmd.Context(), repository.Remote); err != nil {
				return fmt.Errorf("fetch selected remote: %w", err)
			}
		}
		result, err := collect(cmd.Context(), repository, fetch)
		if err != nil {
			return err
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printReport(cmd.OutOrStdout(), result)
	}}
	cmd.Flags().StringVar(&remote, "remote", "", "Remote whose tracking state to inspect")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON")
	cmd.Flags().BoolVar(&withGitHub, "github", false, "Look up PR state and merged cleanup evidence on GitHub")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "Fetch the selected remote before inspecting status")
	return cmd
}

func collect(ctx context.Context, repository *repo.RepoContext, fetched bool) (*report, error) {
	base := jjutils.RemoteBookmarkRevset(repository.DefaultBranch, repository.Remote)
	graph, err := repository.JJ.BuildChangeGraphForBase(ctx, base)
	if err != nil {
		return nil, err
	}
	bookmarks, err := repository.JJ.ListBookmarksForRemote(ctx, repository.Remote)
	if err != nil {
		return nil, err
	}
	entries, err := repository.JJ.GetLog(ctx, "bookmarks()", 0)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool)
	for _, bookmark := range bookmarks {
		known[bookmark.Name] = true
	}
	counts := make(map[string]int)
	for i := range entries {
		for _, name := range entries[i].LocalBookmarks {
			counts[name]++
			if !known[name] {
				bookmarks = append(bookmarks, jjutils.Bookmark{Name: name, CommitID: entries[i].CommitID, ChangeID: entries[i].ChangeID})
			}
		}
	}
	byCommit := map[string]*jjutils.LogEntry{}
	for i := range entries {
		byCommit[entries[i].CommitID] = &entries[i]
	}
	sort.Slice(bookmarks, func(i, j int) bool {
		if bookmarks[i].Name == bookmarks[j].Name {
			return bookmarks[i].CommitID < bookmarks[j].CommitID
		}
		return bookmarks[i].Name < bookmarks[j].Name
	})
	result := &report{Remote: repository.Remote, Trunk: repository.DefaultBranch, Fetched: fetched, GitHubChecked: repository.GitHub != nil, Bookmarks: []row{}}
	for _, bookmark := range bookmarks {
		if bookmark.Name == repository.DefaultBranch {
			continue
		}
		item := row{Bookmark: bookmark.Name, CommitID: bookmark.CommitID, Base: repository.DefaultBranch, NeedsPush: bookmark.NeedsPushTo(repository.Remote), Excluded: graph.IsTainted(bookmark.Name) || counts[bookmark.Name] > 1, BookmarkConflict: counts[bookmark.Name] > 1}
		if parent := graph.ChildToParent[bookmark.Name]; parent != "" {
			item.Base = parent
		}
		if segment := graph.Segments[bookmark.Name]; segment != nil {
			item.Changes = len(segment.Changes)
			for i := range segment.Changes {
				item.Conflict = item.Conflict || segment.Changes[i].Conflict
				item.Divergent = item.Divergent || segment.Changes[i].Divergent
			}
		}
		if entry := byCommit[bookmark.CommitID]; entry != nil {
			item.Conflict = item.Conflict || entry.Conflict
			item.Divergent = item.Divergent || entry.Divergent
		}
		if repository.GitHub != nil {
			pr, err := repository.GitHub.FindPRByHeadAllStates(ctx, repository.Owner, repository.Repo, bookmark.Name)
			if err != nil {
				return nil, fmt.Errorf("look up PR for %s: %w", bookmark.Name, err)
			}
			if pr != nil {
				item.PRNumber = pr.Number
				item.PRState = pr.State
				item.PRURL = pr.URL
				item.PRBase = pr.Base
				if pr.Merged {
					item.PRState = "merged"
					if pr.MatchesMergedHead(bookmark.CommitID) {
						item.CleanupReason = "merged PR matches local head; local bookmark can be reviewed for pruning"
					} else {
						item.CleanupReason = "merged PR head differs; preserve local work"
					}
				}
			}
		}
		result.Bookmarks = append(result.Bookmarks, item)
	}
	return result, nil
}

func printReport(out io.Writer, result *report) error {
	freshness := "existing tracking data; use --fetch to refresh"
	if result.Fetched {
		freshness = "fetched"
	}
	if _, err := fmt.Fprintf(out, "Remote: %s (%s)\n", result.Remote, freshness); err != nil {
		return err
	}
	if len(result.Bookmarks) == 0 {
		_, err := fmt.Fprintln(out, "No feature bookmarks.")
		return err
	}
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "BOOKMARK\tBASE\tPUSH\tPR\tISSUES"); err != nil {
		return err
	}
	for i := range result.Bookmarks {
		item := &result.Bookmarks[i]
		push := "synced"
		if item.NeedsPush {
			push = "needed"
		}
		if item.BookmarkConflict {
			push = "blocked"
		}
		pr := "unchecked"
		if result.GitHubChecked {
			pr = "none"
		}
		if item.PRNumber > 0 {
			pr = fmt.Sprintf("#%d %s", item.PRNumber, item.PRState)
			if item.PRBase != item.Base {
				pr += " (base " + item.PRBase + ")"
			}
		}
		var issues []string
		if item.BookmarkConflict {
			issues = append(issues, "conflicted bookmark reference")
		}
		if item.Conflict {
			issues = append(issues, "conflict")
		}
		if item.Divergent {
			issues = append(issues, "divergent")
		}
		if item.Excluded {
			issues = append(issues, "excluded from stack")
		}
		if item.CleanupReason != "" {
			issues = append(issues, item.CleanupReason)
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", item.Bookmark, item.Base, push, pr, strings.Join(issues, ", ")); err != nil {
			return err
		}
	}
	return writer.Flush()
}
