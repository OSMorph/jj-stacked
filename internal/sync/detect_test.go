package sync

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

func TestFilterMergedFromBottom_DeduplicatesBranchesAndBlocksGaps(t *testing.T) {
	graph := jjutils.NewChangeGraph()
	graph.ChildToParent["b"] = "a"
	graph.ChildToParent["c"] = "b"
	graph.ChildToParent["d"] = "a"
	merged := []MergedBookmark{{Name: "a"}, {Name: "c"}, {Name: "d"}}

	got, errs := FilterMergedFromBottom(merged, graph)
	if names := mergedNames(got); !reflect.DeepEqual(names, []string{"a", "d"}) {
		t.Fatalf("merged = %v, want [a d]", names)
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	var gap *OutOfOrderMergeError
	if !errors.As(errs[0], &gap) || gap.Bookmark != "c" || gap.Parent != "b" {
		t.Fatalf("unexpected gap error: %v", errs[0])
	}
}

func TestFilterMergedFromBottom_BlocksDescendantsAboveGap(t *testing.T) {
	graph := jjutils.NewChangeGraph()
	graph.ChildToParent["b"] = "a"
	graph.ChildToParent["c"] = "b"
	merged := []MergedBookmark{{Name: "a"}, {Name: "c"}}

	got, errs := FilterMergedFromBottom(merged, graph)
	if names := mergedNames(got); !reflect.DeepEqual(names, []string{"a"}) {
		t.Fatalf("merged = %v, want [a]", names)
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one gap error", errs)
	}
	var gap *OutOfOrderMergeError
	if !errors.As(errs[0], &gap) || gap.Bookmark != "c" || gap.Parent != "b" {
		t.Fatalf("unexpected gap error: %v", errs[0])
	}
}

func mergedNames(values []MergedBookmark) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = values[i].Name
	}
	return result
}

func TestMergedDetectionPreservesReusedOrUnverifiedBookmarkHeads(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	reviewed := strings.Repeat("a", 40)
	client := &mergedGitHub{prs: map[string]*github.PullRequest{"feature": {Number: 1, HeadSHA: reviewed, Merged: true, MergedAt: &now}}}
	for _, id := range []string{reviewed, strings.Repeat("b", 40), "", reviewed[:12]} {
		merged, warnings := DetectMergedBookmarks(ctx, []jjutils.Bookmark{{Name: "feature", CommitID: id}}, client, "o", "r")
		if id == reviewed {
			if len(merged) != 1 || len(warnings) != 0 {
				t.Fatalf("verified head was not recognized: %v %v", merged, warnings)
			}
		} else if len(merged) != 0 || len(warnings) != 1 {
			t.Fatalf("unverified head %q accepted: %v %v", id, merged, warnings)
		}
	}
}
