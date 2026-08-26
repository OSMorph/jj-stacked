package submit

import (
	"context"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

func TestAnalyzeSubmission_ValidBookmark(t *testing.T) {
	graph := createTestGraph()

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TargetBookmark != "feature-b" {
		t.Errorf("TargetBookmark = %q, want %q", result.TargetBookmark, "feature-b")
	}

	if result.HasErrors() {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Should have 2 bookmarks in stack: feature-a, feature-b
	if len(result.Stack) != 2 {
		t.Errorf("Stack length = %d, want 2", len(result.Stack))
	}

	// Verify order: feature-a first (bottom), then feature-b
	if len(result.Stack) >= 2 {
		if result.Stack[0].Bookmark.Name != "feature-a" {
			t.Errorf("Stack[0].Name = %q, want %q", result.Stack[0].Bookmark.Name, "feature-a")
		}
		if result.Stack[1].Bookmark.Name != "feature-b" {
			t.Errorf("Stack[1].Name = %q, want %q", result.Stack[1].Bookmark.Name, "feature-b")
		}
	}
}

func TestAnalyzeSubmission_NonexistentBookmark(t *testing.T) {
	graph := createTestGraph()

	result, err := AnalyzeSubmission(context.Background(), graph, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.HasErrors() {
		t.Error("expected errors for nonexistent bookmark")
	}

	// Check error message
	foundError := false
	for _, e := range result.Errors {
		if e.Error() == "bookmark 'nonexistent' not found" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Errorf("expected 'bookmark not found' error, got: %v", result.Errors)
	}
}

func TestAnalyzeSubmission_TaintedBookmark(t *testing.T) {
	graph := createTestGraph()
	// Mark feature-b as tainted
	graph.TaintedBookmarks["feature-b"] = true

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.HasErrors() {
		t.Error("expected errors for tainted bookmark")
	}
}

func TestAnalyzeSubmission_SingleBookmark(t *testing.T) {
	graph := createTestGraph()

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.HasErrors() {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Should have 1 bookmark in stack
	if len(result.Stack) != 1 {
		t.Errorf("Stack length = %d, want 1", len(result.Stack))
	}
}

func TestAnalyzeSubmission_EmptyDescription(t *testing.T) {
	graph := createTestGraph()
	// Clear the description
	segment := graph.Segments["feature-a"]
	if len(segment.Changes) > 0 {
		segment.Changes[0].DescriptionFirstLine = ""
		segment.Changes[0].Description = ""
	}

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.HasErrors() {
		t.Fatal("expected an error for empty description")
	}
	want := "change 'change-a' in bookmark 'feature-a' has no description; run `jj describe -r change-a` before submitting"
	if got := result.Errors[0].Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestAnalyzeSubmission_EmptyIntermediateDescription(t *testing.T) {
	graph := createTestGraph()
	segment := graph.Segments["feature-a"]
	segment.Changes = append([]jjutils.LogEntry{
		{
			ChangeID: "undescribed-change",
		},
	}, segment.Changes...)
	graph.Stacks[0].Segments[0].Changes = segment.Changes

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-a")
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasErrors() {
		t.Fatal("expected an error for undescribed intermediate change")
	}
	if got := result.Errors[0].Error(); !strings.Contains(got, "undescribed-change") {
		t.Fatalf("error = %q, want change ID", got)
	}
}

func TestAnalyzeSubmission_EmptyChangeDoesNotRequireDescription(t *testing.T) {
	graph := createTestGraph()
	segment := graph.Segments["feature-a"]
	segment.Changes = append([]jjutils.LogEntry{
		{
			ChangeID: "empty-change",
			IsEmpty:  true,
		},
	}, segment.Changes...)
	graph.Stacks[0].Segments[0].Changes = segment.Changes

	result, err := AnalyzeSubmission(context.Background(), graph, "feature-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestExtractPRContent(t *testing.T) {
	tests := []struct {
		name          string
		changes       []jjutils.LogEntry
		expectedTitle string
		expectedBody  string
	}{
		{
			name:          "empty changes",
			changes:       nil,
			expectedTitle: "",
			expectedBody:  "",
		},
		{
			name: "single line description",
			changes: []jjutils.LogEntry{
				{
					DescriptionFirstLine: "Add feature",
					Description:          "Add feature",
				},
			},
			expectedTitle: "Add feature",
			expectedBody:  "",
		},
		{
			name: "multi-line description",
			changes: []jjutils.LogEntry{
				{
					DescriptionFirstLine: "Add feature",
					Description:          "Add feature\n\nThis adds a new feature.\nWith more details.",
				},
			},
			expectedTitle: "Add feature",
			expectedBody:  "This adds a new feature.\nWith more details.",
		},
		{
			name: "uses latest change",
			changes: []jjutils.LogEntry{
				{
					DescriptionFirstLine: "First commit",
					Description:          "First commit",
				},
				{
					DescriptionFirstLine: "Latest commit",
					Description:          "Latest commit",
				},
			},
			expectedTitle: "Latest commit",
			expectedBody:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segment := jjutils.BookmarkSegment{
				Changes: tt.changes,
			}
			title, body := extractPRContent(&segment)
			if title != tt.expectedTitle {
				t.Errorf("title = %q, want %q", title, tt.expectedTitle)
			}
			if body != tt.expectedBody {
				t.Errorf("body = %q, want %q", body, tt.expectedBody)
			}
		})
	}
}

func TestGetStackSummary(t *testing.T) {
	result := &AnalysisResult{
		Stack: []StackBookmark{
			{Bookmark: jjutils.Bookmark{Name: "feature-a"}},
			{Bookmark: jjutils.Bookmark{Name: "feature-b"}},
			{Bookmark: jjutils.Bookmark{Name: "feature-c"}},
		},
	}

	summary := GetStackSummary(result)
	expected := "feature-c → feature-b → feature-a → main"
	if summary != expected {
		t.Errorf("GetStackSummary() = %q, want %q", summary, expected)
	}
}

func TestGetStackSummary_Empty(t *testing.T) {
	result := &AnalysisResult{}

	summary := GetStackSummary(result)
	if summary != "empty stack" {
		t.Errorf("GetStackSummary() = %q, want %q", summary, "empty stack")
	}
}

// createTestGraph creates a test graph for testing.
// Structure:
//
//	feature-b → feature-a → main
func createTestGraph() *jjutils.ChangeGraph {
	graph := jjutils.NewChangeGraph()

	// Add bookmarks
	graph.Bookmarks["feature-a"] = jjutils.Bookmark{
		Name:      "feature-a",
		ChangeID:  "change-a",
		HasRemote: false,
		IsSynced:  false,
	}
	graph.Bookmarks["feature-b"] = jjutils.Bookmark{
		Name:      "feature-b",
		ChangeID:  "change-b",
		HasRemote: true,
		IsSynced:  true,
	}

	// Add segments
	graph.Segments["feature-a"] = &jjutils.BookmarkSegment{
		Bookmark: graph.Bookmarks["feature-a"],
		Changes: []jjutils.LogEntry{
			{
				ChangeID:             "change-a",
				DescriptionFirstLine: "Add feature A",
				Description:          "Add feature A\n\nThis is the body.",
			},
		},
		Parent: "",
	}
	graph.Segments["feature-b"] = &jjutils.BookmarkSegment{
		Bookmark: graph.Bookmarks["feature-b"],
		Changes: []jjutils.LogEntry{
			{
				ChangeID:             "change-b",
				DescriptionFirstLine: "Add feature B",
				Description:          "Add feature B",
			},
		},
		Parent: "feature-a",
	}

	// Set up relationships
	graph.ChildToParent["feature-b"] = "feature-a"
	graph.ParentToChildren["feature-a"] = []string{"feature-b"}

	graph.Roots = []string{"feature-a"}
	graph.Leaves = []string{"feature-b"}

	// Build stacks
	graph.Stacks = []jjutils.BranchStack{
		{
			Segments: []jjutils.BookmarkSegment{
				*graph.Segments["feature-a"],
				*graph.Segments["feature-b"],
			},
		},
	}

	return graph
}
