// Package cleanup plans explicit local cleanup using concrete commit identities.
package cleanup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// Candidate explains one proposed local mutation and its affected descendants.
type Candidate struct {
	Kind        string             `json:"kind"`
	Action      string             `json:"action"`
	Bookmark    string             `json:"bookmark,omitempty"`
	Revision    jjutils.LogEntry   `json:"revision"`
	Reason      string             `json:"reason"`
	DiffSummary string             `json:"diff_summary,omitempty"`
	PRNumber    int                `json:"pr_number,omitempty"`
	Descendants []jjutils.LogEntry `json:"descendants,omitempty"`
}

// Plan records the repository operation whose state was reviewed.
type Plan struct {
	OperationID string      `json:"operation_id"`
	Candidates  []Candidate `json:"candidates"`
	Keep        []string    `json:"keep,omitempty"`
	Warnings    []string    `json:"warnings,omitempty"`
}

// Options chooses the reasons to inspect. Old age only produces candidates.
type Options struct {
	Merged        bool
	Closed        bool
	MissingRemote bool
	Stale         bool
	OlderThan     time.Duration
	Revision      string
	Remote        string
	TrunkBranch   string
}

// Begin snapshots the current workspace before recording the reviewed operation.
func Begin(ctx context.Context, jj jjutils.JJFunctions) (*Plan, error) {
	if _, err := jj.GetLog(ctx, "@", 1); err != nil {
		return nil, err
	}
	id, err := jj.GetOperationID(ctx)
	return &Plan{OperationID: id, Candidates: []Candidate{}}, err
}

// CheckOperation rejects plans invalidated by an intervening jj operation.
func CheckOperation(ctx context.Context, jj jjutils.JJFunctions, plan *Plan) error {
	if _, err := jj.GetLog(ctx, "@", 1); err != nil {
		return err
	}
	id, err := jj.GetOperationID(ctx)
	if err != nil {
		return err
	}
	if plan.OperationID == "" || id != plan.OperationID {
		return fmt.Errorf("repository changed after the cleanup preview; run the command again")
	}
	return nil
}

// DiscoverPrune identifies candidates without mutating revisions or bookmarks.
// Callers fetch the selected remote before invoking it when remote facts are needed.
func DiscoverPrune(ctx context.Context, jj jjutils.JJFunctions, gh github.GitHubClient, owner, repository string, opts Options, now time.Time) (*Plan, error) {
	plan, err := Begin(ctx, jj)
	if err != nil {
		return nil, err
	}
	if opts.Merged || opts.Closed || opts.MissingRemote {
		if gh == nil {
			return nil, fmt.Errorf("GitHub is required for remote cleanup candidates")
		}
		bookmarks, err := jj.ListBookmarksForRemote(ctx, opts.Remote)
		if err != nil {
			return nil, err
		}
		for _, bookmark := range bookmarks {
			if bookmark.Name == opts.TrunkBranch {
				continue
			}
			pr, err := gh.FindPRByHeadAllStates(ctx, owner, repository, bookmark.Name)
			if err != nil {
				return nil, fmt.Errorf("inspect PR for %s: %w", bookmark.Name, err)
			}
			kind, reason := "", ""
			switch {
			case pr != nil && pr.Merged && !pr.MatchesMergedHead(bookmark.CommitID):
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s moved since PR #%d merged; preserving local work", bookmark.Name, pr.Number))
				continue
			case opts.Merged && pr != nil && pr.MatchesMergedHead(bookmark.CommitID):
				kind, reason = "merged", fmt.Sprintf("PR #%d merged this exact commit; forget local bookmark", pr.Number)
			case opts.Closed && pr != nil && pr.State == "closed" && !pr.Merged:
				kind, reason = "closed", fmt.Sprintf("PR #%d closed without merging; forget bookmark and preserve changes", pr.Number)
			case opts.MissingRemote:
				exists, err := gh.BranchExists(ctx, owner, repository, bookmark.Name)
				if err != nil {
					return nil, err
				}
				if !exists && pr != nil {
					kind, reason = "missing-remote", "previous PR exists but remote branch is gone; forget bookmark and preserve changes"
				}
			}
			if kind == "" {
				continue
			}
			revision, err := jj.GetChange(ctx, bookmark.CommitID)
			if err != nil {
				return nil, err
			}
			candidate := Candidate{Kind: kind, Action: "forget-bookmark", Bookmark: bookmark.Name, Revision: *revision, Reason: reason}
			if pr != nil {
				candidate.PRNumber = pr.Number
			}
			plan.Candidates = append(plan.Candidates, candidate)
		}
	}
	if opts.Stale {
		if opts.OlderThan <= 0 {
			return nil, fmt.Errorf("--older-than must be positive")
		}
		revset := "heads(mutable())"
		if opts.Revision != "" {
			revset = "(" + opts.Revision + ") & mutable()"
		}
		// Active remote history and every workspace are outside stale cleanup scope.
		revset = "(" + revset + ") ~ ::working_copies() ~ ::remote_bookmarks()"
		entries, err := jj.GetLog(ctx, revset, 0)
		if err != nil {
			return nil, err
		}
		for i := range entries {
			entry := &entries[i]
			if entry.CommittedAt.IsZero() || !entry.CommittedAt.Before(now.Add(-opts.OlderThan)) {
				continue
			}
			if entry.Divergent {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is divergent; use abandon --diverged to select a version", short(entry.ChangeID)))
				continue
			}
			descendants, err := jj.GetLog(ctx, entry.CommitID+"::", 0)
			if err != nil {
				return nil, err
			}
			plan.Candidates = append(plan.Candidates, Candidate{
				Kind: "stale", Action: "abandon", Revision: *entry, Descendants: descendants,
				Reason: fmt.Sprintf("draft last committed %s; review before abandoning", entry.CommittedAt.Format("2006-01-02")),
			})
		}
	}
	sort.Slice(plan.Candidates, func(i, j int) bool {
		a, b := plan.Candidates[i], plan.Candidates[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Bookmark != b.Bookmark {
			return a.Bookmark < b.Bookmark
		}
		return a.Revision.CommitID < b.Revision.CommitID
	})
	return plan, CheckOperation(ctx, jj, plan)
}

// Divergence groups visible versions of a single change.
type Divergence struct {
	ChangeID string             `json:"change_id"`
	Versions []jjutils.LogEntry `json:"versions"`
}

// DiscoverDivergence includes protected versions so the user can retain them.
func DiscoverDivergence(ctx context.Context, jj jjutils.JJFunctions) (*Plan, []Divergence, error) {
	plan, err := Begin(ctx, jj)
	if err != nil {
		return nil, nil, err
	}
	entries, err := jj.ListDivergentChanges(ctx)
	if err != nil {
		return nil, nil, err
	}
	byChange := map[string][]jjutils.LogEntry{}
	for i := range entries {
		entry := &entries[i]
		byChange[entry.ChangeID] = append(byChange[entry.ChangeID], *entry)
	}
	groups := make([]Divergence, 0, len(byChange))
	for change, versions := range byChange {
		sort.Slice(versions, func(i, j int) bool { return versions[i].CommitID < versions[j].CommitID })
		groups = append(groups, Divergence{ChangeID: change, Versions: versions})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ChangeID < groups[j].ChangeID })
	return plan, groups, CheckOperation(ctx, jj, plan)
}

// KeepVersion adds only the other variants of this group to the plan.
func KeepVersion(ctx context.Context, jj jjutils.JJFunctions, plan *Plan, group Divergence, keep string) error {
	found := false
	for i := range group.Versions {
		entry := &group.Versions[i]
		if entry.CommitID == keep {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("keeper is not a version of change %s", group.ChangeID)
	}
	plan.Keep = append(plan.Keep, keep)
	for i := range group.Versions {
		entry := &group.Versions[i]
		if entry.CommitID == keep {
			continue
		}
		descendants, err := jj.GetLog(ctx, entry.CommitID+"::", 0)
		if err != nil {
			return err
		}
		plan.Candidates = append(plan.Candidates, Candidate{
			Kind: "diverged", Action: "abandon", Revision: *entry, Descendants: descendants,
			Reason: "retain version " + short(keep) + "; abandon this version and reparent its descendants",
		})
	}
	return nil
}

// Validate protects workspaces, remote history, keepers, and changed bookmark heads.
func Validate(ctx context.Context, jj jjutils.JJFunctions, plan *Plan) error {
	if err := CheckOperation(ctx, jj, plan); err != nil {
		return err
	}
	var commits []string
	for i := range plan.Candidates {
		candidate := &plan.Candidates[i]
		switch candidate.Action {
		case "forget-bookmark":
			entries, err := jj.GetLog(ctx, jjutils.BookmarkRevset(candidate.Bookmark), 0)
			if err != nil {
				return err
			}
			if len(entries) != 1 || entries[0].CommitID != candidate.Revision.CommitID {
				return fmt.Errorf("bookmark %s changed after review", candidate.Bookmark)
			}
		case "abandon":
			if candidate.Revision.CommitID == "" {
				return fmt.Errorf("cleanup candidate has no commit ID")
			}
			commits = append(commits, candidate.Revision.CommitID)
		default:
			return fmt.Errorf("unknown cleanup action %q", candidate.Action)
		}
	}
	if len(commits) == 0 {
		return nil
	}
	selected := "(" + strings.Join(commits, " | ") + ")"
	protected := "immutable() | working_copies() | remote_bookmarks()"
	if len(plan.Keep) > 0 {
		protected += " | " + strings.Join(plan.Keep, " | ")
	}
	entries, err := jj.GetLog(ctx, selected+":: & ("+protected+")", 0)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("abandonment would affect protected commit %s (immutable, remote, workspace, or retained version); keep that branch intact", short(entries[0].CommitID))
	}
	return CheckOperation(ctx, jj, plan)
}

// Execute revalidates the preview, then applies local-only cleanup.
// The caller prints OperationID before mutation for recovery, including failures.
func Execute(ctx context.Context, jj jjutils.JJFunctions, plan *Plan) error {
	if err := Validate(ctx, jj, plan); err != nil {
		return err
	}
	var commits []string
	seen := map[string]bool{}
	for i := range plan.Candidates {
		candidate := &plan.Candidates[i]
		if candidate.Action == "abandon" && !seen[candidate.Revision.CommitID] {
			commits = append(commits, candidate.Revision.CommitID)
			seen[candidate.Revision.CommitID] = true
		}
	}
	// Forget first while the reviewed bookmark targets still exist. Abandon all
	// selected revisions together so one rewrite cannot change a later target.
	for i := range plan.Candidates {
		candidate := &plan.Candidates[i]
		if candidate.Action == "forget-bookmark" {
			if err := jj.ForgetBookmark(ctx, candidate.Bookmark); err != nil {
				return err
			}
		}
	}
	if len(commits) > 0 {
		return jj.Abandon(ctx, "("+strings.Join(commits, " | ")+")")
	}
	return nil
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
