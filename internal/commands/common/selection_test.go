package common

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type selectionJJ struct {
	jjutils.JJFunctions
	entries map[string][]jjutils.LogEntry
}

func (j *selectionJJ) GetLog(_ context.Context, revset string, _ int) ([]jjutils.LogEntry, error) {
	return j.entries[revset], nil
}
func TestBookmarkSelectionPreservesExplicitChoiceAndRequiresDisambiguation(t *testing.T) {
	graph := jjutils.NewChangeGraph()
	graph.Bookmarks["a"] = jjutils.Bookmark{Name: "a"}
	graph.Bookmarks["b"] = jjutils.Bookmark{Name: "b"}
	jj := &selectionJJ{entries: map[string][]jjutils.LogEntry{"@": {{LocalBookmarks: []string{"b", "a", "main"}}}}}
	input := strings.NewReader("")
	var output bytes.Buffer
	name, err := ResolveBookmark(context.Background(), nil, nil, "literal:*", input, &output)
	if err != nil || name != "literal:*" {
		t.Fatalf("explicit argument changed: %q %v", name, err)
	}
	names, err := BookmarkCandidates(context.Background(), jj, graph)
	if err != nil || !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatalf("candidates=%v error=%v", names, err)
	}
	if _, err := ResolveBookmark(context.Background(), jj, graph, "", input, &output); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("noninteractive ambiguity accepted: %v", err)
	}
}
func TestBookmarkSelectionPrefersClosestDescendantWhenEditingSegment(t *testing.T) {
	graph := jjutils.NewChangeGraph()
	graph.Bookmarks["a"] = jjutils.Bookmark{Name: "a"}
	graph.Bookmarks["b"] = jjutils.Bookmark{Name: "b"}
	jj := &selectionJJ{entries: map[string][]jjutils.LogEntry{"roots(@:: & bookmarks())": {{LocalBookmarks: []string{"b"}}}, "heads(::@ & bookmarks())": {{LocalBookmarks: []string{"a"}}}}}
	name, err := ResolveBookmark(context.Background(), jj, graph, "", strings.NewReader(""), &bytes.Buffer{})
	if err != nil || name != "b" {
		t.Fatalf("selected=%q error=%v", name, err)
	}
}
