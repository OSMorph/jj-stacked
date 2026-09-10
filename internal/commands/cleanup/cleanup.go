// Package cleanup exposes reviewed local cleanup and divergent-version selection.
package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cleanup"
	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/commands/common"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/repo"
	syncpkg "github.com/OSMorph/jj-stacked/internal/sync"
)

// NewPruneCommand reviews merged bookmarks and inactive local drafts.
func NewPruneCommand() *cobra.Command {
	var opts cleanup.Options
	var dryRun, yes, jsonOutput bool
	var age string
	cmd := &cobra.Command{
		Use: "prune", Short: "Review and clean up merged bookmarks or stale local changes",
		Long: `Review cleanup candidates with their reasons and affected revisions.
Merged, closed, and missing-remote candidates forget local bookmarks and preserve
changes. Stale candidates abandon only selected draft heads, or the explicitly
requested revision range. Age alone never proves that work is disposable.

Examples:
  jjk prune --merged --dry-run
  jjk prune --stale --older-than 30d
  jjk prune --stale --revision 'my-old-work::' --older-than 30d --dry-run

Without --yes, an interactive selection and confirmation are required.
--json implies --dry-run. Remote cleanup fetches only the selected remote.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicitScope := opts.Merged || opts.Stale || opts.Closed || opts.MissingRemote
			if !explicitScope {
				opts.Merged = true
			}
			if opts.Revision != "" && !opts.Stale {
				return fmt.Errorf("--revision requires --stale")
			}
			if yes && !explicitScope {
				return fmt.Errorf("--yes requires an explicit cleanup scope such as --merged")
			}
			var err error
			opts.OlderThan, err = parseAge(age)
			if err != nil {
				return err
			}
			executor := cmdexec.NewRealExecutor()
			jj := jjutils.NewJJFunctions(executor, "")
			if err := checkPending(cmd.Context(), jj); err != nil {
				return err
			}
			var plan *cleanup.Plan
			if opts.Merged || opts.Closed || opts.MissingRemote {
				r, err := repo.NewRepoContext(cmd.Context(), repo.RepoContextOptions{Remote: opts.Remote, Exec: executor, Logger: common.NewLogger(common.Debug(cmd))})
				if err != nil {
					return err
				}
				opts.Remote, opts.TrunkBranch = r.Remote, r.DefaultBranch
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Fetching %s before inspecting remote cleanup candidates...\n", r.Remote); err != nil {
					return err
				}
				if err := jj.Fetch(cmd.Context(), r.Remote); err != nil {
					return fmt.Errorf("fetch failed; remote cleanup requires current state: %w", err)
				}
				plan, err = cleanup.DiscoverPrune(cmd.Context(), jj, r.GitHub, r.Owner, r.Repo, opts, time.Now())
				if err != nil {
					return err
				}
			} else {
				plan, err = cleanup.DiscoverPrune(cmd.Context(), jj, nil, "", "", opts, time.Now())
				if err != nil {
					return err
				}
			}
			if err := addDiffSummaries(cmd.Context(), executor, plan); err != nil {
				return err
			}
			return reviewAndExecute(cmd, jj, plan, dryRun, jsonOutput, yes, true)
		},
	}
	cmd.Flags().BoolVar(&opts.Merged, "merged", false, "Offer bookmarks whose exact local commit was merged (default scope)")
	cmd.Flags().BoolVar(&opts.Closed, "closed", false, "Offer bookmarks with closed, unmerged PRs; preserve changes")
	cmd.Flags().BoolVar(&opts.MissingRemote, "missing-remote", false, "Offer bookmarks with a prior PR and a missing remote branch")
	cmd.Flags().BoolVar(&opts.Stale, "stale", false, "Offer old local draft heads without active remote or workspace descendants")
	cmd.Flags().StringVar(&age, "older-than", "30d", "Minimum draft age, e.g. 30d or 48h")
	cmd.Flags().StringVarP(&opts.Revision, "revision", "r", "", "Inspect this explicit stale draft revset instead of heads")
	cmd.Flags().StringVar(&opts.Remote, "remote", "", "GitHub remote (origin, or the only configured remote)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without cleaning up bookmarks or revisions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print candidate JSON without applying cleanup")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Apply the explicitly scoped candidates without prompts")
	return cmd
}

// NewAbandonCommand selects a keeper for each divergent change.
func NewAbandonCommand() *cobra.Command {
	var diverged, dryRun, yes, jsonOutput bool
	var keep string
	cmd := &cobra.Command{
		Use: "abandon --diverged", Short: "Choose a divergent version to keep and review abandoning its siblings",
		Long: `List divergent versions with their parents, references, and content differences.
Choose the version to keep; the other variants become abandonment candidates.
Descendants are reparented onto each abandoned version's parents, not the keeper.
Protected remote history, immutable commits, and workspaces cannot be affected.

Examples:
  jjk abandon --diverged
  jjk abandon --diverged --keep <commit-id> --dry-run
  jjk abandon --diverged --keep <commit-id> --yes

--keep selects only that change's group. --yes requires --keep.
Without a keeper, --dry-run and --json only report the divergent groups.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !diverged {
				return fmt.Errorf("use --diverged to select divergent changes")
			}
			if yes && keep == "" {
				return fmt.Errorf("--yes requires an explicit --keep commit ID")
			}
			executor := cmdexec.NewRealExecutor()
			jj := jjutils.NewJJFunctions(executor, "")
			if err := checkPending(cmd.Context(), jj); err != nil {
				return err
			}
			plan, groups, err := cleanup.DiscoverDivergence(cmd.Context(), jj)
			if err != nil {
				return err
			}
			if jsonOutput && keep == "" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(groups)
			}
			if len(groups) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No divergent changes."); err != nil {
					return err
				}
				return nil
			}
			if keep != "" {
				var matched *jjutils.LogEntry
				var groupIndex int
				for i, group := range groups {
					for versionIndex := range group.Versions {
						entry := &group.Versions[versionIndex]
						if strings.HasPrefix(entry.CommitID, keep) {
							if matched != nil {
								return fmt.Errorf("keeper %q is ambiguous; use a full commit ID", keep)
							}
							matched = entry
							groupIndex = i
						}
					}
				}
				if matched == nil {
					return fmt.Errorf("keeper %q is not a visible divergent commit", keep)
				}
				if err := cleanup.KeepVersion(cmd.Context(), jj, plan, groups[groupIndex], matched.CommitID); err != nil {
					return err
				}
			} else {
				if !dryRun && !common.Interactive(cmd.InOrStdin()) {
					return fmt.Errorf("noninteractive abandonment requires --keep and --yes, or --dry-run")
				}
				prompt := common.NewPrompt(cmd.InOrStdin(), cmd.OutOrStdout())
				for _, group := range groups {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\nChange %s:\n", short(group.ChangeID)); err != nil {
						return err
					}
					for i := range group.Versions {
						entry := &group.Versions[i]
						if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, formatRevision(entry)); err != nil {
							return err
						}
						if i > 0 {
							diff, err := executor.Run(cmd.Context(), jjutils.Binary(), "diff", "--summary", "--from", group.Versions[0].CommitID, "--to", entry.CommitID)
							if err != nil {
								return fmt.Errorf("compare divergent versions: %w", err)
							}
							if strings.TrimSpace(diff) == "" {
								diff = "(same content; metadata or parents differ)\n"
							}
							if _, err := fmt.Fprintf(cmd.OutOrStdout(), "     Content difference from version 1:\n%s", diff); err != nil {
								return err
							}
						}
					}
					if dryRun {
						continue
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose the version to KEEP:"); err != nil {
						return err
					}
					indices, err := prompt.Select(len(group.Versions), false)
					if err != nil {
						return err
					}
					if len(indices) == 0 {
						continue
					}
					if err := cleanup.KeepVersion(cmd.Context(), jj, plan, group, group.Versions[indices[0]].CommitID); err != nil {
						return err
					}
				}
				if dryRun {
					return nil
				}
			}
			if err := addDiffSummaries(cmd.Context(), executor, plan); err != nil {
				return err
			}
			return reviewAndExecute(cmd, jj, plan, dryRun, jsonOutput, yes, false)
		},
	}
	cmd.Flags().BoolVar(&diverged, "diverged", false, "Review divergent changes")
	cmd.Flags().StringVar(&keep, "keep", "", "Commit ID of the version to retain (one change group)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without abandoning revisions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON without applying cleanup")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Apply the selected keeper without confirmation")
	return cmd
}

func checkPending(ctx context.Context, jj jjutils.JJFunctions) error {
	pending, err := syncpkg.HasPendingSync(ctx, jj)
	if err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("finish or abort the pending sync before cleanup")
	}
	return nil
}

func addDiffSummaries(ctx context.Context, executor cmdexec.CommandExecutor, plan *cleanup.Plan) error {
	for i := range plan.Candidates {
		candidate := &plan.Candidates[i]
		if candidate.Action != "abandon" {
			continue
		}
		summary, err := executor.Run(ctx, jjutils.Binary(), "diff", "--summary", "-r", candidate.Revision.CommitID)
		if err != nil {
			return fmt.Errorf("preview changes for %s: %w", short(candidate.Revision.CommitID), err)
		}
		candidate.DiffSummary = strings.TrimSpace(summary)
	}
	return nil
}

func reviewAndExecute(cmd *cobra.Command, jj jjutils.JJFunctions, plan *cleanup.Plan, dryRun, jsonOutput, yes, selectCandidates bool) error {
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
	}
	if err := printPlan(cmd.OutOrStdout(), plan); err != nil {
		return err
	}
	if len(plan.Candidates) == 0 || dryRun {
		return nil
	}
	if !yes {
		if !common.Interactive(cmd.InOrStdin()) {
			return fmt.Errorf("review with --dry-run, or use an explicit scope and --yes")
		}
		prompt := common.NewPrompt(cmd.InOrStdin(), cmd.OutOrStdout())
		if selectCandidates {
			indices, err := prompt.Select(len(plan.Candidates), true)
			if err != nil {
				return err
			}
			selected := make([]cleanup.Candidate, 0, len(indices))
			for _, i := range indices {
				selected = append(selected, plan.Candidates[i])
			}
			plan.Candidates = selected
			if len(selected) == 0 {
				return nil
			}
			if err := printPlan(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
		}
		if err := cleanup.Validate(cmd.Context(), jj, plan); err != nil {
			return err
		}
		confirmed, err := prompt.Confirm("Apply these local cleanup actions?")
		if err != nil {
			return err
		}
		if !confirmed {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Cancelled."); err != nil {
				return err
			}
			return nil
		}
	}
	if err := cleanup.Validate(cmd.Context(), jj, plan); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Recovery operation: %s\nTo restore local state: jj op restore %s\n", plan.OperationID, plan.OperationID); err != nil {
		return err
	}
	if err := cleanup.Execute(cmd.Context(), jj, plan); err != nil {
		return fmt.Errorf("cleanup stopped; recovery operation %s: %w", plan.OperationID, err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Completed %d local cleanup action(s).\n", len(plan.Candidates)); err != nil {
		return err
	}
	return nil
}

func printPlan(out io.Writer, plan *cleanup.Plan) error {
	var text strings.Builder
	for _, warning := range plan.Warnings {
		fmt.Fprintf(&text, "Warning: %s\n", warning)
	}
	if len(plan.Candidates) == 0 {
		text.WriteString("No cleanup candidates.\n")
	}
	for i := range plan.Candidates {
		candidate := &plan.Candidates[i]
		fmt.Fprintf(&text, "%d. %s %s %s\n   %s\n", i+1, candidate.Action, candidate.Bookmark, formatRevision(&candidate.Revision), candidate.Reason)
		if candidate.Action == "abandon" {
			if candidate.DiffSummary != "" {
				fmt.Fprintf(&text, "   Changes:\n%s\n", candidate.DiffSummary)
			}
			for j := range candidate.Descendants {
				descendant := &candidate.Descendants[j]
				if descendant.CommitID != candidate.Revision.CommitID {
					fmt.Fprintf(&text, "   Reparent descendant: %s\n", formatRevision(descendant))
				}
			}
		}
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func formatRevision(entry *jjutils.LogEntry) string {
	labels := append(append([]string{}, entry.LocalBookmarks...), entry.RemoteBookmarks...)
	if entry.Immutable {
		labels = append(labels, "immutable")
	}
	if entry.HasWorkingCopy {
		labels = append(labels, "working copy")
	}
	parents := make([]string, len(entry.Parents))
	for i, id := range entry.Parents {
		parents[i] = short(id)
	}
	return fmt.Sprintf("%s %s [parents: %s; refs: %s]", short(entry.CommitID), entry.DescriptionFirstLine, strings.Join(parents, ","), strings.Join(labels, ","))
}

func parseAge(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(value, "d"), 64)
		if err != nil || days <= 0 || days > 36500 {
			return 0, fmt.Errorf("invalid age %q", value)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	age, err := time.ParseDuration(value)
	if err != nil || age <= 0 {
		return 0, fmt.Errorf("invalid age %q; use e.g. 30d or 48h", value)
	}
	return age, nil
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
