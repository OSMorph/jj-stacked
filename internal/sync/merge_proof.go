package sync

import (
	"context"
	"fmt"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// landedInTrunk checks the actual post-merge commit, not the PR's branch name.
// It also handles a merge observed after the command's initial fetch safely.
func landedInTrunk(ctx context.Context, jj jjutils.JJFunctions, commitID, trunk string) (bool, error) {
	if !fullCommitID(commitID) {
		return false, nil
	}
	return jj.IsAncestor(ctx, commitID, trunk)
}

func proveMergedDestinations(ctx context.Context, jj jjutils.JJFunctions, merged []MergedBookmark, trunkBranch, trunk string) ([]MergedBookmark, []error) {
	byName := make(map[string]*MergedBookmark, len(merged))
	var errors []error
	for i := range merged {
		candidate := &merged[i]
		byName[candidate.Name] = candidate
		if candidate.InTrunk {
			candidate.LandedCommitID = candidate.CommitID
			continue
		}
		if candidate.BaseBranch != trunkBranch {
			continue
		}
		landed, err := landedInTrunk(ctx, jj, candidate.MergeCommitID, trunk)
		if err != nil {
			errors = append(errors, fmt.Errorf("verify merge destination for %s: %w", candidate.Name, err))
			continue
		}
		if landed {
			candidate.LandedCommitID = candidate.MergeCommitID
		}
	}
	// A dependent PR is covered only when its exact head is an ancestor of
	// the exact reviewed head of its already-proven destination PR. Old PRs
	// sharing branch names or merge timestamps cannot establish this relation.
	for changed := true; changed; {
		changed = false
		for i := range merged {
			candidate := &merged[i]
			if candidate.LandedCommitID != "" || candidate.BaseBranch == "" {
				continue
			}
			base := byName[candidate.BaseBranch]
			if base == nil || base.LandedCommitID == "" {
				continue
			}
			included, err := jj.IsAncestor(ctx, candidate.CommitID, base.CommitID)
			if err != nil {
				return nil, []error{fmt.Errorf("verify merged dependency %s into %s: %w", candidate.Name, base.Name, err)}
			}
			if included {
				candidate.LandedCommitID = base.LandedCommitID
				changed = true
			}
		}
	}
	var verified []MergedBookmark
	for i := range merged {
		if merged[i].LandedCommitID == "" {
			errors = append(errors, fmt.Errorf("cannot prove merged bookmark %s (PR base %q) landed in fetched %s; preserving its segment for review", merged[i].Name, merged[i].BaseBranch, trunkBranch))
			continue
		}
		verified = append(verified, merged[i])
	}
	return verified, errors
}
