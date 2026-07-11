// Package sync implements the sync command for jj-stacked.
package sync

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	completioncmd "github.com/OSMorph/jj-stacked/internal/commands/completion"
	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/logger"
	"github.com/OSMorph/jj-stacked/internal/repo"
	submitpkg "github.com/OSMorph/jj-stacked/internal/submit"
	"github.com/OSMorph/jj-stacked/internal/sync"
)

// Options configures the sync command.
type Options struct {
	Bookmark   string // Optional: only sync this bookmark's stack
	DryRun     bool
	Continue   bool
	Abort      bool
	NoResubmit bool
	Yes        bool
	Debug      bool
	Remote     string
}

// NewCommand creates the sync command.
func NewCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "sync [bookmark]",
		Short: "Sync local stack with remote (fetch, rebase, push)",
		Long: `Synchronize your local stack with the remote repository.

This command performs the following steps:
1. Fetches the latest changes from the selected remote
2. Abandons any bookmarks whose PRs have been merged (if detected)
3. Rebases the stack onto the updated trunk (e.g., main@origin)
4. Pushes bookmarks that are ahead of the selected remote

If a bookmark is specified, its entire connected stack will be synced.
Otherwise, all stacks are synced.

EXAMPLES:
  # Preview what would be synced (recommended first)
  jj-stacked sync --dry-run

  # Sync all stacks
  jj-stacked sync

  # Sync only a specific bookmark's stack
  jj-stacked sync my-feature

  # Continue sync after resolving conflicts
  jj-stacked sync --continue

  # Abort a sync in progress
  jj-stacked sync --abort

  # Skip confirmation prompt
  jj-stacked sync --yes

  # Use a non-origin remote
  jj-stacked sync my-feature --remote upstream

WORKFLOW:
  After merging PRs on GitHub, run this command to sync your local
  stack. It will fetch the latest trunk, rebase your stack onto it,
  and push the updated bookmarks.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completioncmd.BookmarkValidArgsFunction,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (opts.Continue || opts.Abort) {
				return fmt.Errorf("bookmark cannot be used with --continue or --abort")
			}
			if len(args) > 0 {
				opts.Bookmark = args[0]
			}
			return runSync(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.Flags().BoolVar(&opts.Continue, "continue", false, "Continue sync after resolving conflicts")
	cmd.Flags().BoolVar(&opts.Abort, "abort", false, "Abort sync in progress")
	cmd.Flags().BoolVar(&opts.NoResubmit, "no-resubmit", false, "Skip refreshing existing PR bases and stack comments")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Enable debug output for troubleshooting")
	cmd.Flags().StringVar(&opts.Remote, "remote", "", "Remote to fetch from and push to (default: origin)")
	cmd.MarkFlagsMutuallyExclusive("continue", "abort", "dry-run")

	return cmd
}

func runSync(ctx context.Context, opts *Options) error {
	// Set up logger
	log := logger.New(logger.Options{
		Debug:  opts.Debug,
		Output: os.Stderr,
	})

	// Create JJ functions
	jj := jjutils.NewJJFunctions(cmdexec.NewRealExecutor(), "")

	// Handle --abort
	if opts.Abort {
		return handleAbort(ctx, jj)
	}

	// Handle --continue
	if opts.Continue {
		return handleContinue(ctx, jj, log)
	}

	// Check for existing sync in progress
	hasPending, err := sync.HasPendingSync(ctx, jj)
	if err != nil {
		return fmt.Errorf("check for pending sync state: %w", err)
	} else if hasPending {
		fmt.Fprintf(os.Stderr, "Error: A sync operation is already in progress.\n")
		fmt.Fprintf(os.Stderr, "Run 'jj-stacked sync --continue' to resume or 'jj-stacked sync --abort' to cancel.\n")
		return fmt.Errorf("sync already in progress")
	}

	// Set up repo context
	repoOpts := repo.RepoContextOptions{
		Remote: opts.Remote,
		Logger: log,
	}

	fmt.Printf("Initializing repository context...\n")
	repoCtx, err := repo.NewRepoContext(ctx, repoOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("failed to initialize repository")
	}
	opts.Remote = repoCtx.Remote

	// Fetch before analysis so merge detection, trunk ancestry, and push state
	// are based on current remote-tracking bookmarks. Dry-run intentionally
	// includes this fetch but performs no subsequent mutation.
	fmt.Printf("Fetching from %s...\n", repoCtx.Remote)
	if err := jj.Fetch(ctx, repoCtx.Remote); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("fetch failed")
	}

	// Phase 1: Analysis
	if opts.Bookmark != "" {
		fmt.Printf("Analyzing sync state for stack: %s...\n", opts.Bookmark)
	} else {
		fmt.Printf("Analyzing sync state...\n")
	}
	analysisOpts := sync.AnalyzeOptions{
		Bookmark: opts.Bookmark,
		Remote:   repoCtx.Remote,
	}
	analysis, err := sync.AnalyzeSyncWithOptions(ctx, jj, repoCtx.GitHub, repoCtx.Owner, repoCtx.Repo, analysisOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("analysis failed")
	}

	// Check for analysis errors
	if !analysis.CanProceed() {
		fmt.Fprintf(os.Stderr, "\nCannot sync:\n")
		for _, e := range analysis.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e.Error())
		}
		return fmt.Errorf("analysis found blocking errors")
	}

	// Show warnings
	for _, warning := range analysis.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}

	// Check if there's anything to sync
	if !analysis.HasMergedBookmarks() && !analysis.HasBookmarksToPush() && len(analysis.RebaseRoots) == 0 {
		fmt.Printf("\nRemote fetched; stack is already up to date.\n")
		return nil
	}

	// Phase 2: Planning
	fmt.Printf("Creating sync plan...\n")
	plan, err := sync.CreateSyncPlan(analysis)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("planning failed")
	}

	// Show plan
	fmt.Printf("\n%s\n", sync.FormatPlan(plan))

	// Dry run mode - show plan and exit
	if opts.DryRun {
		fmt.Printf("(dry-run mode - remote tracking was fetched; no history, remote branches, or PRs were changed)\n")
		return nil
	}

	// Confirm with user unless --yes
	if !opts.Yes {
		fmt.Printf("Proceed with sync? [y/N] ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil || (response != "y" && response != "Y" && response != "yes") {
			fmt.Printf("Sync cancelled.\n")
			return nil
		}
	}

	// Phase 3: Execution
	fmt.Printf("\nExecuting sync...\n")

	// Save state for potential recovery
	operationID, err := jj.GetOperationID(ctx)
	if err != nil {
		return fmt.Errorf("record pre-sync jj operation: %w", err)
	}
	state := sync.CreateInitialState(plan, operationID, opts.Bookmark, opts.NoResubmit)
	if err := sync.SaveSyncState(ctx, jj, state); err != nil {
		return fmt.Errorf("save sync recovery state before mutation: %w", err)
	}

	callbacks := &sync.SyncCallbacks{
		OnCheckpoint: func(state *sync.SyncState) error {
			return sync.SaveSyncState(ctx, jj, state)
		},
		OnPushStart: func(bookmark string) {
			fmt.Printf("  Pushing %s...\n", bookmark)
		},
		OnPushComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnDelete: func(bookmark string) {
			fmt.Printf("  Deleting merged bookmark %s (change already in trunk)...\n", bookmark)
		},
		OnDeleteComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnAbandon: func(bookmark string) {
			fmt.Printf("  Abandoning %s...\n", bookmark)
		},
		OnAbandonComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnRebaseStart: func(bookmarks []string) {
			fmt.Printf("  Rebasing %d bookmark(s) onto %s...\n", len(bookmarks), plan.RebaseTarget)
		},
		OnRebaseComplete: func(err error) {
			if err != nil {
				fmt.Printf("  Rebase failed: %v\n", err)
			}
		},
	}

	result := sync.ExecuteSyncWithState(ctx, plan, state, jj, callbacks)

	// Handle conflicts
	if result.HasConflicts {
		conflictInfo := &sync.ConflictInfo{
			HasConflicts: true,
			Files:        result.ConflictFiles,
		}
		fmt.Printf("\n%s", sync.FormatConflictInstructions(conflictInfo))
		return fmt.Errorf("sync paused due to conflicts")
	}

	if result.Success && !opts.NoResubmit {
		if err := refreshExistingPRs(ctx, jj, repoCtx, opts.Bookmark, log); err != nil {
			state.SetPhase("refresh-failed")
			_ = sync.SaveSyncState(ctx, jj, state)
			return fmt.Errorf("refresh existing pull requests: %w", err)
		}
		state.MarkStepComplete("refresh-prs")
	}

	if result.Success {
		if err := sync.ClearSyncState(ctx, jj); err != nil {
			return fmt.Errorf("clear completed sync state: %w", err)
		}
	}

	// Show result
	fmt.Printf("\n%s\n", sync.FormatResult(result))

	if !result.Success {
		return fmt.Errorf("sync failed")
	}

	return nil
}

func handleAbort(ctx context.Context, jj jjutils.JJFunctions) error {
	state, err := sync.LoadSyncState(ctx, jj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sync state: %v\n", err)
		return err
	}

	if err := sync.ValidateCanAbort(state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	if state.StartOperationID == "" {
		return fmt.Errorf("sync state has no recovery operation ID; refusing unsafe abort")
	}
	if err := jj.RestoreOperation(ctx, state.StartOperationID); err != nil {
		return fmt.Errorf("restore pre-sync operation %s: %w", state.StartOperationID, err)
	}

	if err := sync.ClearSyncState(ctx, jj); err != nil {
		fmt.Fprintf(os.Stderr, "Error clearing sync state: %v\n", err)
		return err
	}

	fmt.Printf("Sync aborted and local jj state restored.\n")
	fmt.Printf("\n%s", sync.FormatAbortInstructions())

	return nil
}

func handleContinue(ctx context.Context, jj jjutils.JJFunctions, log *logger.Logger) error {
	state, err := sync.LoadSyncState(ctx, jj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sync state: %v\n", err)
		return err
	}

	if err := sync.ValidateCanContinue(ctx, jj, state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	fmt.Printf("Continuing sync...\n")
	callbacks := &sync.SyncCallbacks{
		OnCheckpoint: func(state *sync.SyncState) error {
			return sync.SaveSyncState(ctx, jj, state)
		},
		OnPushStart: func(bookmark string) {
			fmt.Printf("  Pushing %s...\n", bookmark)
		},
		OnPushComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnDelete: func(bookmark string) {
			fmt.Printf("  Deleting merged bookmark %s (change already in trunk)...\n", bookmark)
		},
		OnDeleteComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnRebaseStart: func(bookmarks []string) {
			fmt.Printf("  Resuming %d pending rebase root(s)...\n", len(bookmarks))
		},
		OnRebaseComplete: func(err error) {
			if err != nil {
				fmt.Printf("  Rebase failed: %v\n", err)
			}
		},
	}

	result := sync.ExecuteSyncWithState(ctx, state.Plan, state, jj, callbacks)

	// Handle conflicts
	if result.HasConflicts {
		conflictInfo := &sync.ConflictInfo{
			HasConflicts: true,
			Files:        result.ConflictFiles,
		}
		fmt.Printf("\n%s", sync.FormatConflictInstructions(conflictInfo))
		return fmt.Errorf("sync paused due to conflicts")
	}
	if result.Success && !state.NoResubmit && !state.StepComplete("refresh-prs") {
		repoCtx, err := repo.NewRepoContext(ctx, repo.RepoContextOptions{Remote: state.Remote, Logger: log})
		if err != nil {
			return fmt.Errorf("initialize repository context: %w", err)
		}
		if err := refreshExistingPRs(ctx, jj, repoCtx, state.Bookmark, log); err != nil {
			state.SetPhase("refresh-failed")
			_ = sync.SaveSyncState(ctx, jj, state)
			return fmt.Errorf("refresh existing pull requests: %w", err)
		}
		state.MarkStepComplete("refresh-prs")
		if err := sync.SaveSyncState(ctx, jj, state); err != nil {
			return err
		}
	}
	if result.Success {
		if err := sync.ClearSyncState(ctx, jj); err != nil {
			return err
		}
	}

	// Show result
	fmt.Printf("\n%s\n", sync.FormatResult(result))

	if !result.Success {
		return fmt.Errorf("sync failed")
	}

	return nil
}

func refreshExistingPRs(ctx context.Context, jj jjutils.JJFunctions, repoCtx *repo.RepoContext, bookmark string, log *logger.Logger) error {
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		return err
	}
	var targets []string
	if bookmark != "" {
		component := graph.GetConnectedBookmarks(bookmark)
		selected := make(map[string]bool, len(component))
		for _, name := range component {
			selected[name] = true
		}
		for _, name := range component {
			leaf := true
			for _, child := range graph.ParentToChildren[name] {
				if selected[child] {
					leaf = false
					break
				}
			}
			if leaf {
				targets = append(targets, name)
			}
		}
	} else {
		targets = append(targets, graph.Leaves...)
	}

	currentUser, _ := repoCtx.GitHub.GetAuthenticatedUser(ctx)
	for _, target := range targets {
		analysis, err := submitpkg.AnalyzeSubmission(ctx, graph, target)
		if err != nil || analysis.HasErrors() {
			if err != nil {
				return err
			}
			return fmt.Errorf("cannot refresh stack %s: %s", target, submitpkg.FormatAnalysisErrors(analysis))
		}
		deps := &submitpkg.PlanningDeps{
			GitHub: repoCtx.GitHub, Owner: repoCtx.Owner, Repo: repoCtx.Repo,
			Remote: repoCtx.Remote, DefaultBranch: repoCtx.DefaultBranch, CurrentUser: currentUser,
		}
		plan, err := submitpkg.CreatePRRefreshPlan(ctx, analysis, deps, nil)
		if err != nil {
			return err
		}
		if len(plan.Actions) == 0 {
			continue
		}
		fmt.Printf("  Refreshing %d existing PR update(s) for %s...\n", len(plan.Actions), target)
		result, err := submitpkg.ExecuteSubmissionPlan(ctx, plan, &submitpkg.ActionDeps{
			JJ: jj, GitHub: repoCtx.GitHub, Owner: repoCtx.Owner, Repo: repoCtx.Repo, Remote: repoCtx.Remote,
		}, nil)
		if err != nil {
			return err
		}
		if result.Summary.Failed > 0 {
			return fmt.Errorf("%d PR refresh action(s) failed", result.Summary.Failed)
		}
		log.Debug("refreshed existing PR metadata", "stack", target, "actions", len(plan.Actions))
	}
	return nil
}
