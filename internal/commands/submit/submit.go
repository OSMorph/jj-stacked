// Package submit implements the submit command for jj-stacked.
package submit

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
	"github.com/OSMorph/jj-stacked/internal/submit"
)

// Options configures the submit command.
type Options struct {
	DryRun bool
	Remote string
	Draft  bool
	Debug  bool
}

// NewCommand creates the submit command.
func NewCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "submit <bookmark>",
		Short: "Submit a bookmark stack as pull requests",
		Long: `Submit a bookmark and all its downstack bookmarks as pull requests on GitHub.

This command creates or updates PRs for the specified bookmark and all bookmarks
in its ancestry chain (downstack). Each bookmark becomes a separate PR, with
proper base branches set to maintain the stack structure.

WORKFLOW:
  1. Analysis: Identifies all bookmarks from trunk to your target
  2. Planning: Checks GitHub for existing PRs and determines actions
  3. Execution: Pushes branches, creates/updates PRs, syncs stack comments

Stack comments are automatically added to each PR showing the full stack
structure with links to related PRs.

EXAMPLES:
  # Submit a feature and all its dependencies
  jj-stacked submit my-feature

  # Preview the plan without making changes
  jj-stacked submit my-feature --dry-run

  # Create PRs as drafts
  jj-stacked submit my-feature --draft

  # Use a specific remote (useful with multiple remotes)
  jj-stacked submit my-feature --remote upstream

PREREQUISITES:
  • GitHub authentication configured (run 'jj-stacked auth test')
  • Changes committed to bookmarks (not just working copy)
  • Bookmarks should be in a linear stack from trunk`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubmit(cmd.Context(), args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.Flags().StringVar(&opts.Remote, "remote", "", "Remote to push to (default: auto-detect from repository)")
	cmd.Flags().BoolVar(&opts.Draft, "draft", false, "Create PRs as drafts (ready for review later)")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Enable debug output for troubleshooting")

	return cmd
}

func runSubmit(ctx context.Context, bookmark string, opts *Options) error {
	// Set up logger
	log := logger.New(logger.Options{
		Debug:  opts.Debug,
		Output: os.Stderr,
	})

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

	// Create JJ functions
	jj := jjutils.NewJJFunctions(cmdexec.NewRealExecutor(), "")

	// Phase 1: Build change graph
	fmt.Printf("Building change graph...\n")
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("failed to build change graph")
	}

	// Phase 2: Analysis
	fmt.Printf("Analyzing submission for '%s'...\n", bookmark)
	analysis, err := submit.AnalyzeSubmission(ctx, graph, bookmark)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("analysis failed")
	}

	// Check for analysis errors
	if analysis.HasErrors() {
		fmt.Print(submit.FormatAnalysisErrors(analysis))
		return fmt.Errorf("cannot submit: analysis found errors")
	}

	// Show warnings
	warnings := submit.FormatWarnings(analysis)
	if warnings != "" {
		fmt.Print(warnings)
	}

	// Phase 3: Planning
	fmt.Printf("Creating submission plan...\n")
	planningDeps := &submit.PlanningDeps{
		GitHub:        repoCtx.GitHub,
		Owner:         repoCtx.Owner,
		Repo:          repoCtx.Repo,
		Remote:        repoCtx.Remote,
		DefaultBranch: repoCtx.DefaultBranch,
	}

	planCallbacks := &submit.PlanningCallbacks{
		OnProgress: func(msg string) {
			if opts.Debug {
				log.Debug(msg)
			}
		},
		OnBookmarkChecked: func(bm string, hasPR bool) {
			if opts.Debug {
				status := "no PR"
				if hasPR {
					status = "has PR"
				}
				log.Debug("checked bookmark", "bookmark", bm, "status", status)
			}
		},
	}

	plan, err := submit.CreateSubmissionPlan(ctx, analysis, planningDeps, planCallbacks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("planning failed")
	}

	// Apply draft flag if set
	if opts.Draft {
		submit.SetDraftFlag(plan, true)
	}

	// Dry run mode - show plan and exit
	if opts.DryRun {
		fmt.Print(submit.FormatDryRunOutput(analysis, plan))
		return nil
	}

	// Phase 4: Execution
	if len(plan.Actions) == 0 {
		fmt.Printf("\nNo actions required - everything is up to date.\n")
		// Still show existing PR links
		if len(plan.ExistingPRs) > 0 {
			fmt.Printf("\nPull Requests:\n")
			for _, pr := range plan.ExistingPRs {
				fmt.Printf("  • %s\n", pr.URL)
			}
		}
		return nil
	}

	fmt.Printf("\nExecuting %d action(s)...\n", len(plan.Actions))

	execDeps := &submit.ActionDeps{
		JJ:     jj,
		GitHub: repoCtx.GitHub,
		Owner:  repoCtx.Owner,
		Repo:   repoCtx.Repo,
		Remote: repoCtx.Remote,
	}

	execCallbacks := &submit.ExecutionCallbacks{
		OnActionStart: func(action submit.SubmissionAction) {
			fmt.Printf("  → %s\n", action.Description())
		},
		OnActionComplete: func(action submit.SubmissionAction, result submit.ActionResult) {
			if !result.Success {
				errMsg := apperrors.FormatErrorWithHint(result.Error)
				fmt.Printf("    ✗ Failed: %s\n", errMsg)
			} else if action.Type() == submit.ActionCreatePR {
				if url, ok := result.Details["pr_url"].(string); ok {
					fmt.Printf("    ✓ Created: %s\n", url)
				}
			} else {
				fmt.Printf("    ✓ Done\n")
			}
		},
		OnProgress: func(completed, total int) {
			// Progress is shown via action callbacks
		},
	}

	result, err := submit.ExecuteSubmissionPlan(ctx, plan, execDeps, execCallbacks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nExecution stopped: %s\n", apperrors.FormatErrorWithHint(err))
	}

	// Show results summary
	fmt.Printf("\n")
	fmt.Print(submit.FormatExecutionResult(result, plan))

	// Return error if there were failures
	if result.Summary.Failed > 0 {
		return fmt.Errorf("%d action(s) failed", result.Summary.Failed)
	}

	return nil
}
