// Package sync implements the sync command for jj-stacked.
package sync

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/logger"
	"github.com/OSMorph/jj-stacked/internal/repo"
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
}

// NewCommand creates the sync command.
func NewCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "sync [bookmark]",
		Short: "Sync local stack with remote (fetch, rebase, push)",
		Long: `Synchronize your local stack with the remote repository.

This command performs the following steps:
1. Fetches the latest changes from all remotes
2. Abandons any bookmarks whose PRs have been merged (if detected)
3. Rebases the stack onto the updated trunk (e.g., main@origin)
4. Pushes bookmarks that are ahead of origin

If a bookmark is specified, only that bookmark's stack will be synced.
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

WORKFLOW:
  After merging PRs on GitHub, run this command to sync your local
  stack. It will fetch the latest trunk, rebase your stack onto it,
  and push the updated bookmarks.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.Bookmark = args[0]
			}
			return runSync(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.Flags().BoolVar(&opts.Continue, "continue", false, "Continue sync after resolving conflicts")
	cmd.Flags().BoolVar(&opts.Abort, "abort", false, "Abort sync in progress")
	cmd.Flags().BoolVar(&opts.NoResubmit, "no-resubmit", false, "Don't automatically re-submit remaining PRs")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Enable debug output for troubleshooting")

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
		return handleContinue(ctx, jj, opts, log)
	}

	// Check for existing sync in progress
	hasPending, err := sync.HasPendingSync(ctx, jj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check for pending sync: %v\n", err)
	} else if hasPending {
		fmt.Fprintf(os.Stderr, "Error: A sync operation is already in progress.\n")
		fmt.Fprintf(os.Stderr, "Run 'jj-stacked sync --continue' to resume or 'jj-stacked sync --abort' to cancel.\n")
		return fmt.Errorf("sync already in progress")
	}

	// Set up repo context
	repoOpts := repo.RepoContextOptions{
		Logger: log,
	}

	fmt.Printf("Initializing repository context...\n")
	repoCtx, err := repo.NewRepoContext(ctx, repoOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("failed to initialize repository")
	}

	// Phase 1: Analysis
	if opts.Bookmark != "" {
		fmt.Printf("Analyzing sync state for stack: %s...\n", opts.Bookmark)
	} else {
		fmt.Printf("Analyzing sync state...\n")
	}
	analysisOpts := sync.AnalyzeOptions{
		Bookmark: opts.Bookmark,
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
	if !analysis.HasMergedBookmarks() && !analysis.HasBookmarksToPush() {
		fmt.Printf("\nNo merged PRs found and no bookmarks need pushing. Stack is already up to date.\n")
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
		fmt.Printf("(dry-run mode - no changes made)\n")
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
	state := sync.CreateInitialState(plan)
	if err := sync.SaveSyncState(ctx, jj, state); err != nil {
		log.Debug("failed to save sync state", "error", err)
	}

	callbacks := &sync.SyncCallbacks{
		OnPushStart: func(bookmark string) {
			fmt.Printf("  Pushing %s...\n", bookmark)
		},
		OnPushComplete: func(bookmark string, err error) {
			if err != nil {
				fmt.Printf("    Failed: %v\n", err)
			}
		},
		OnFetchStart: func() {
			fmt.Printf("  Fetching from remotes...\n")
		},
		OnFetchComplete: func(err error) {
			if err != nil {
				fmt.Printf("  Fetch failed: %v\n", err)
			} else {
				fmt.Printf("  Fetch complete.\n")
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

	result := sync.ExecuteSync(ctx, plan, jj, callbacks)

	// Clear state on success
	if result.Success {
		if err := sync.ClearSyncState(ctx, jj); err != nil {
			log.Debug("failed to clear sync state", "error", err)
		}
	}

	// Handle conflicts
	if result.HasConflicts {
		conflictInfo := &sync.ConflictInfo{
			HasConflicts: true,
			Files:        result.ConflictFiles,
		}
		fmt.Printf("\n%s", sync.FormatConflictInstructions(conflictInfo))
		return fmt.Errorf("sync paused due to conflicts")
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

	// Clear state
	if err := sync.ClearSyncState(ctx, jj); err != nil {
		fmt.Fprintf(os.Stderr, "Error clearing sync state: %v\n", err)
		return err
	}

	fmt.Printf("Sync aborted.\n")
	fmt.Printf("\n%s", sync.FormatAbortInstructions())

	return nil
}

func handleContinue(ctx context.Context, jj jjutils.JJFunctions, opts *Options, log *logger.Logger) error {
	state, err := sync.LoadSyncState(ctx, jj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sync state: %v\n", err)
		return err
	}

	if err := sync.ValidateCanContinue(ctx, jj, state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	// Continue with the remaining steps
	fmt.Printf("Continuing sync...\n")

	callbacks := &sync.SyncCallbacks{
		OnRebaseStart: func(bookmarks []string) {
			fmt.Printf("  Completing rebase...\n")
		},
		OnRebaseComplete: func(err error) {
			if err != nil {
				fmt.Printf("  Rebase failed: %v\n", err)
			}
		},
	}

	// Re-execute remaining steps
	result := sync.ExecuteSync(ctx, state.Plan, jj, callbacks)

	// Clear state on success
	if result.Success {
		if err := sync.ClearSyncState(ctx, jj); err != nil {
			log.Debug("failed to clear sync state", "error", err)
		}
	}

	// Handle conflicts
	if result.HasConflicts {
		conflictInfo := &sync.ConflictInfo{
			HasConflicts: true,
			Files:        result.ConflictFiles,
		}
		fmt.Printf("\n%s", sync.FormatConflictInstructions(conflictInfo))
		return fmt.Errorf("sync paused due to conflicts")
	}

	// Show result
	fmt.Printf("\n%s\n", sync.FormatResult(result))

	if !result.Success {
		return fmt.Errorf("sync failed")
	}

	return nil
}
