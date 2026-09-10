// Package doctor reports effective configuration and repository recovery issues.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/commands/common"
	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/repo"
	syncpkg "github.com/OSMorph/jj-stacked/internal/sync"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// NewCommand creates doctor. --github explicitly validates credentials online.
func NewCommand() *cobra.Command {
	var remote string
	var asJSON, withGitHub bool
	cmd := &cobra.Command{Use: "doctor", Short: "Explain repository settings and recovery problems", Long: `Check the effective jj executable, repository, remote, trunk, conflicts,
divergent changes, and pending sync recovery. The default checks are local.
Use --github to validate GitHub authentication and access to the repository.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		executor := cmdexec.NewRealExecutor()
		jj := jjutils.NewJJFunctions(executor, "")
		var checks []check
		jjPath := os.Getenv("JJ_PATH")
		if jjPath == "" {
			jjPath = "jj"
		}
		version, err := executor.Run(cmd.Context(), jjPath, "--version")
		if err != nil {
			checks = append(checks, check{"jj", "error", err.Error()})
		} else {
			checks = append(checks, check{"jj", "ok", jjPath + ": " + strings.TrimSpace(version)})
		}
		root, err := jj.GetRepoRoot(cmd.Context())
		if err != nil {
			checks = append(checks, check{"repository", "error", err.Error()})
		} else {
			checks = append(checks, check{"repository", "ok", root})
			checks = append(checks, localChecks(cmd.Context(), jj)...)
		}
		opts := repo.RepoContextOptions{Remote: remote, Exec: executor, Logger: common.NewLogger(common.Debug(cmd))}
		var repository *repo.RepoContext
		if withGitHub {
			repository, err = repo.NewRepoContext(cmd.Context(), opts)
		} else {
			repository, err = repo.Discover(cmd.Context(), opts)
		}
		if err != nil {
			checks = append(checks, check{"remote configuration", "error", err.Error()})
		} else {
			checks = append(checks, check{"remote", "ok", repository.Remote + " → " + repository.GitHubHost + "/" + repository.Owner + "/" + repository.Repo})
			base := jjutils.RemoteBookmarkRevset(repository.DefaultBranch, repository.Remote)
			entries, err := repository.JJ.GetLog(cmd.Context(), base, 1)
			if err != nil || len(entries) == 0 {
				checks = append(checks, check{"trunk", "error", repository.DefaultBranch + "@" + repository.Remote + " is unavailable; fetch this remote or set TRUNK_BRANCH"})
			} else {
				checks = append(checks, check{"trunk", "ok", repository.DefaultBranch + "@" + repository.Remote})
			}
			checks = append(checks, authenticationCheck(cmd.Context(), repository.GitHub))
			if withGitHub {
				_, accessErr := repository.GitHub.GetDefaultBranch(cmd.Context(), repository.Owner, repository.Repo)
				if accessErr != nil {
					checks = append(checks, check{"repository access", "error", accessErr.Error()})
				} else {
					checks = append(checks, check{"repository access", "ok", "GitHub repository is accessible"})
				}
			}
		}
		if err := printChecks(cmd.OutOrStdout(), checks, asJSON); err != nil {
			return err
		}
		for _, item := range checks {
			if item.Status == "error" {
				return fmt.Errorf("doctor found issues requiring attention")
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&remote, "remote", "", "Remote to inspect")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON")
	cmd.Flags().BoolVar(&withGitHub, "github", false, "Validate authentication and repository access on GitHub")
	return cmd
}

func localChecks(ctx context.Context, jj jjutils.JJFunctions) []check {
	var checks []check
	state, err := syncpkg.LoadSyncState(ctx, jj)
	switch {
	case err != nil:
		checks = append(checks, check{"sync recovery", "error", err.Error()})
	case state != nil:
		checks = append(checks, check{"sync recovery", "warning", "pending phase " + state.Phase + "; inspect before sync --continue or sync --abort"})
	default:
		checks = append(checks, check{"sync recovery", "ok", "no pending sync"})
	}
	conflicts, err := jj.HasConflicts(ctx)
	switch {
	case err != nil:
		checks = append(checks, check{"conflicts", "error", err.Error()})
	case conflicts:
		checks = append(checks, check{"conflicts", "warning", "conflicted changes found; inspect with jj log -r 'conflicts()'"})
	default:
		checks = append(checks, check{"conflicts", "ok", "none"})
	}
	divergent, err := jj.ListDivergentChanges(ctx)
	switch {
	case err != nil:
		checks = append(checks, check{"divergence", "error", err.Error()})
	case len(divergent) > 0:
		checks = append(checks, check{"divergence", "warning", fmt.Sprintf("%d divergent commit variants; review with jjk abandon --diverged", len(divergent))})
	default:
		checks = append(checks, check{"divergence", "ok", "none"})
	}
	entries, err := jj.GetLog(ctx, "bookmarks()", 0)
	if err != nil {
		checks = append(checks, check{"bookmark references", "error", err.Error()})
	} else {
		counts := make(map[string]int)
		for i := range entries {
			for _, name := range entries[i].LocalBookmarks {
				counts[name]++
			}
		}
		var conflicted []string
		for name, count := range counts {
			if count > 1 {
				conflicted = append(conflicted, name)
			}
		}
		sort.Strings(conflicted)
		if len(conflicted) > 0 {
			checks = append(checks, check{"bookmark references", "warning", "conflicted targets: " + strings.Join(conflicted, ", ") + "; inspect with jj bookmark list"})
		} else {
			checks = append(checks, check{"bookmark references", "ok", "no conflicted targets"})
		}
	}
	return checks
}

func authenticationCheck(ctx context.Context, client github.GitHubClient) check {
	if client == nil {
		return check{"authentication", "unchecked", "use --github to validate credentials and repository access"}
	}
	user, err := client.GetAuthenticatedUser(ctx)
	if err != nil {
		return check{"authentication", "error", err.Error()}
	}
	return check{"authentication", "ok", user}
}

func printChecks(out io.Writer, checks []check, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(struct {
			Checks []check `json:"checks"`
		}{checks})
	}
	for _, item := range checks {
		if _, err := fmt.Fprintf(out, "%s: %s — %s\n", item.Name, item.Status, item.Detail); err != nil {
			return err
		}
	}
	return nil
}
