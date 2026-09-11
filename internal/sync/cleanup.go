package sync

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

func pendingAbandon(plan *SyncPlan, state *SyncState) bool {
	for _, name := range plan.ToAbandon {
		if !state.StepComplete("abandon:" + name) {
			return true
		}
	}
	return false
}

// validateCleanup rechecks the reviewed identities before any history mutation.
// It deliberately refuses old pending plans which used bookmark names as proof.
func validateCleanup(ctx context.Context, plan *SyncPlan, state *SyncState, jj jjutils.JJFunctions) error {
	var pending []string
	for _, name := range plan.ToDelete {
		if !state.StepComplete("delete:" + name) {
			pending = append(pending, name)
		}
	}
	abandon := pendingAbandon(plan, state)
	if abandon {
		pending = append(pending, plan.ToAbandon...)
	}
	if len(pending) == 0 {
		return nil
	}
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		return fmt.Errorf("list bookmarks before merged cleanup: %w", err)
	}
	byName := make(map[string][]jjutils.Bookmark)
	for _, bookmark := range bookmarks {
		byName[bookmark.Name] = append(byName[bookmark.Name], bookmark)
	}
	for _, name := range pending {
		expected := plan.CleanupHeads[name]
		if !fullCommitID(expected) {
			return fmt.Errorf("saved cleanup plan for %s lacks an exact reviewed commit ID; run sync --abort and start a new sync", name)
		}
		current := byName[name]

		if len(current) == 0 && !contains(plan.ToAbandon, name) {
			continue // A missing local-only bookmark is already cleaned up.
		}
		if len(current) != 1 || current[0].CommitID != expected {
			return fmt.Errorf("bookmark %s moved, disappeared, or diverged since planning; preserving its work; run sync --abort and replan", name)
		}
	}
	if !abandon {
		return nil
	}
	if len(plan.AbandonCommits) == 0 {
		return fmt.Errorf("saved cleanup plan lacks exact merged segment commits; run sync --abort and start a new sync")
	}
	commits := make(map[string]bool, len(plan.AbandonCommits))
	for _, id := range plan.AbandonCommits {
		if !fullCommitID(id) {
			return fmt.Errorf("cleanup plan has an invalid commit ID; run sync --abort and replan")
		}
		commits[id] = true
	}
	for _, name := range plan.ToAbandon {
		landed, err := landedInTrunk(ctx, jj, plan.CleanupProofs[name], plan.RebaseTarget)
		if err != nil {
			return fmt.Errorf("recheck merged destination for %s: %w", name, err)
		}
		if !landed {
			return fmt.Errorf("cleanup plan for %s has no proven merge in fetched trunk; run sync --abort and replan", name)
		}
	}
	for _, name := range plan.ToAbandon {
		if state.StepComplete("abandon:"+name) || !commits[plan.CleanupHeads[name]] {
			return fmt.Errorf("partially completed or incomplete merged segment plan; run sync --abort and replan")
		}
	}
	for _, bookmark := range bookmarks {
		if commits[bookmark.CommitID] && !contains(plan.ToAbandon, bookmark.Name) {
			return fmt.Errorf("merged segment is also referenced by bookmark %s; preserving it for review", bookmark.Name)
		}
	}
	// Conflicted bookmark references have no normal target and may be absent
	// from ListBookmarks. Inspect commit metadata before abandoning their work.
	targetEntries, err := jj.GetLog(ctx, "("+strings.Join(plan.AbandonCommits, " | ")+")", 0)
	if err != nil {
		return fmt.Errorf("inspect references on merged targets: %w", err)
	}
	for i := range targetEntries {
		for _, name := range targetEntries[i].LocalBookmarks {
			if !contains(plan.ToAbandon, name) {
				return fmt.Errorf("merged segment is also referenced by bookmark %s; preserving it for review", name)
			}
		}
	}
	protected, err := jj.GetLog(ctx, "("+strings.Join(plan.AbandonCommits, " | ")+") & (::immutable_heads() | root() | working_copies())", 1)
	if err != nil {
		return fmt.Errorf("check immutable revisions and workspace targets before cleanup: %w", err)
	}
	if len(protected) > 0 {
		return fmt.Errorf("merged segment contains an immutable revision or workspace target; preserving it for review")
	}
	// Every affected descendant must remain beneath a surviving segment which
	// this plan will rebase onto the new trunk. Anonymous side branches and
	// workspaces directly on merged work otherwise lose that work's content.
	var survivingSubtrees []string
	for _, root := range plan.RebaseRoots {
		source := plan.RebaseSources[root]
		if source == "" {
			return fmt.Errorf("cleanup plan lacks the complete surviving segment for %s; abort and replan", root)
		}
		entries, err := jj.GetLog(ctx, source, 2)
		if err != nil || len(entries) != 1 {
			return fmt.Errorf("surviving segment for %s is missing or divergent; preserving merged work", root)
		}
		ancestor, err := jj.IsAncestor(ctx, entries[0].CommitID, jjutils.BookmarkRevset(root))
		if err != nil || !ancestor {
			return fmt.Errorf("bookmark %s moved outside its surviving segment; preserving merged work", root)
		}
		survivingSubtrees = append(survivingSubtrees, "("+entries[0].CommitID+")::")
	}
	merged := "(" + strings.Join(plan.AbandonCommits, " | ") + ")"
	unprotected := merged + ":: ~ " + merged
	if len(survivingSubtrees) > 0 {
		unprotected += " ~ (" + strings.Join(survivingSubtrees, " | ") + ")"
	}
	descendants, err := jj.GetLog(ctx, unprotected, 1)
	if err != nil {
		return fmt.Errorf("inspect descendants of merged work: %w", err)
	}
	if len(descendants) > 0 {
		return fmt.Errorf("merged work has descendants outside the planned surviving stacks (including anonymous changes or workspaces); preserving it for review")
	}
	return nil
}

func fullCommitID(id string) bool {
	if len(id) != 40 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}
