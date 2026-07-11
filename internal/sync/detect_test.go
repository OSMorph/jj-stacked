package sync

import (
	"errors"
	"reflect"
	"testing"

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
