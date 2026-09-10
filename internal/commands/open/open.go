// Package open opens the pull request associated with a bookmark.
package open

import (
	"context"
	"fmt"
	"net/url"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/commands/common"
	completioncmd "github.com/OSMorph/jj-stacked/internal/commands/completion"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/repo"
)

// NewCommand creates open. --url prints a link without launching a browser.
func NewCommand() *cobra.Command {
	var remote string
	var urlOnly bool
	cmd := &cobra.Command{Use: "open [bookmark]", Short: "Open a bookmark's pull request in your browser", Long: `Open the latest pull request for a bookmark. Without an argument, infer the
closest bookmarked work around @, or ask when several bookmarks are possible.
Use --url to print the PR URL for scripts without opening a browser.`, Args: cobra.MaximumNArgs(1), ValidArgsFunction: completioncmd.BookmarkValidArgsFunction, RunE: func(cmd *cobra.Command, args []string) error {
		repository, err := repo.NewRepoContext(cmd.Context(), repo.RepoContextOptions{Remote: remote, Logger: common.NewLogger(common.Debug(cmd))})
		if err != nil {
			return err
		}
		bookmark := ""
		if len(args) > 0 {
			bookmark = args[0]
		}
		if bookmark == "" {
			graph, err := repository.JJ.BuildChangeGraphForBase(cmd.Context(), jjutils.RemoteBookmarkRevset(repository.DefaultBranch, repository.Remote))
			if err != nil {
				return err
			}
			bookmark, err = common.ResolveBookmark(cmd.Context(), repository.JJ, graph, "", cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
		}
		pr, err := repository.GitHub.FindPRByHeadAllStates(cmd.Context(), repository.Owner, repository.Repo, bookmark)
		if err != nil {
			return err
		}
		if pr == nil {
			return fmt.Errorf("no pull request found for bookmark %q", bookmark)
		}
		if err := validateURL(pr.URL, repository.GitHubHost); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), pr.URL); err != nil {
			return err
		}
		if urlOnly {
			return nil
		}
		return launchBrowser(cmd.Context(), repository.Exec, runtime.GOOS, pr.URL)
	}}
	cmd.Flags().StringVar(&remote, "remote", "", "Remote whose pull request to open")
	cmd.Flags().BoolVar(&urlOnly, "url", false, "Print the PR URL without opening a browser")
	return cmd
}

func validateURL(raw, host string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host != host || parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("GitHub returned an invalid PR URL")
	}
	return nil
}

func launchBrowser(ctx context.Context, executor cmdexec.CommandExecutor, system, link string) error {
	var name string
	var args []string
	switch system {
	case "darwin":
		name = "open"
		args = []string{link}
	case "linux":
		name = "xdg-open"
		args = []string{link}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", link}
	default:
		return fmt.Errorf("automatic browser opening is unavailable; use the printed URL")
	}
	if _, err := executor.Run(ctx, name, args...); err != nil {
		return fmt.Errorf("open browser (or use --url): %w", err)
	}
	return nil
}
