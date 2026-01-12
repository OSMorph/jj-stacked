package jjutils

import (
	"testing"
)

func TestLogEntry_IsMergeCommit(t *testing.T) {
	tests := []struct {
		name     string
		parents  []string
		expected bool
	}{
		{
			name:     "no parents",
			parents:  nil,
			expected: false,
		},
		{
			name:     "single parent",
			parents:  []string{"abc123"},
			expected: false,
		},
		{
			name:     "two parents (merge)",
			parents:  []string{"abc123", "def456"},
			expected: true,
		},
		{
			name:     "three parents",
			parents:  []string{"abc123", "def456", "ghi789"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := LogEntry{Parents: tt.parents}
			if got := entry.IsMergeCommit(); got != tt.expected {
				t.Errorf("IsMergeCommit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLogEntry_HasBookmarks(t *testing.T) {
	tests := []struct {
		name           string
		localBookmarks []string
		expected       bool
	}{
		{
			name:           "no bookmarks",
			localBookmarks: nil,
			expected:       false,
		},
		{
			name:           "empty slice",
			localBookmarks: []string{},
			expected:       false,
		},
		{
			name:           "one bookmark",
			localBookmarks: []string{"feature-a"},
			expected:       true,
		},
		{
			name:           "multiple bookmarks",
			localBookmarks: []string{"feature-a", "feature-b"},
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := LogEntry{LocalBookmarks: tt.localBookmarks}
			if got := entry.HasBookmarks(); got != tt.expected {
				t.Errorf("HasBookmarks() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBookmark_NeedsPush(t *testing.T) {
	tests := []struct {
		name      string
		hasRemote bool
		isSynced  bool
		expected  bool
	}{
		{
			name:      "no remote",
			hasRemote: false,
			isSynced:  false,
			expected:  true,
		},
		{
			name:      "has remote, synced",
			hasRemote: true,
			isSynced:  true,
			expected:  false,
		},
		{
			name:      "has remote, not synced",
			hasRemote: true,
			isSynced:  false,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := Bookmark{HasRemote: tt.hasRemote, IsSynced: tt.isSynced}
			if got := bm.NeedsPush(); got != tt.expected {
				t.Errorf("NeedsPush() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBranchStack_TopBookmark(t *testing.T) {
	tests := []struct {
		name     string
		segments []BookmarkSegment
		expected string
	}{
		{
			name:     "empty stack",
			segments: nil,
			expected: "",
		},
		{
			name: "single segment",
			segments: []BookmarkSegment{
				{Bookmark: Bookmark{Name: "feature-a"}},
			},
			expected: "feature-a",
		},
		{
			name: "multiple segments",
			segments: []BookmarkSegment{
				{Bookmark: Bookmark{Name: "feature-a"}},
				{Bookmark: Bookmark{Name: "feature-b"}},
				{Bookmark: Bookmark{Name: "feature-c"}},
			},
			expected: "feature-c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := BranchStack{Segments: tt.segments}
			if got := stack.TopBookmark(); got != tt.expected {
				t.Errorf("TopBookmark() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBranchStack_AllBookmarks(t *testing.T) {
	tests := []struct {
		name     string
		segments []BookmarkSegment
		expected []string
	}{
		{
			name:     "empty stack",
			segments: nil,
			expected: []string{},
		},
		{
			name: "single segment",
			segments: []BookmarkSegment{
				{Bookmark: Bookmark{Name: "feature-a"}},
			},
			expected: []string{"feature-a"},
		},
		{
			name: "multiple segments",
			segments: []BookmarkSegment{
				{Bookmark: Bookmark{Name: "feature-a"}},
				{Bookmark: Bookmark{Name: "feature-b"}},
				{Bookmark: Bookmark{Name: "feature-c"}},
			},
			expected: []string{"feature-a", "feature-b", "feature-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := BranchStack{Segments: tt.segments}
			got := stack.AllBookmarks()
			if len(got) != len(tt.expected) {
				t.Errorf("AllBookmarks() len = %v, want %v", len(got), len(tt.expected))
				return
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("AllBookmarks()[%d] = %v, want %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestChangeGraph_Methods(t *testing.T) {
	// Create a test graph
	graph := NewChangeGraph()
	graph.Bookmarks["feature-a"] = Bookmark{Name: "feature-a"}
	graph.Bookmarks["feature-b"] = Bookmark{Name: "feature-b"}
	graph.Bookmarks["tainted"] = Bookmark{Name: "tainted"}

	graph.Roots = []string{"feature-a"}
	graph.Leaves = []string{"feature-b"}
	graph.TaintedBookmarks["tainted"] = true

	graph.Stacks = []BranchStack{
		{
			Segments: []BookmarkSegment{
				{Bookmark: Bookmark{Name: "feature-a"}},
				{Bookmark: Bookmark{Name: "feature-b"}},
			},
		},
	}

	t.Run("IsRoot", func(t *testing.T) {
		if !graph.IsRoot("feature-a") {
			t.Error("feature-a should be a root")
		}
		if graph.IsRoot("feature-b") {
			t.Error("feature-b should not be a root")
		}
	})

	t.Run("IsLeaf", func(t *testing.T) {
		if !graph.IsLeaf("feature-b") {
			t.Error("feature-b should be a leaf")
		}
		if graph.IsLeaf("feature-a") {
			t.Error("feature-a should not be a leaf")
		}
	})

	t.Run("IsTainted", func(t *testing.T) {
		if !graph.IsTainted("tainted") {
			t.Error("tainted should be tainted")
		}
		if graph.IsTainted("feature-a") {
			t.Error("feature-a should not be tainted")
		}
	})

	t.Run("GetStack", func(t *testing.T) {
		stack := graph.GetStack("feature-a")
		if stack == nil {
			t.Error("GetStack should return a stack for feature-a")
		}

		stack = graph.GetStack("nonexistent")
		if stack != nil {
			t.Error("GetStack should return nil for nonexistent bookmark")
		}
	})

	t.Run("GetStackUpTo", func(t *testing.T) {
		stack := graph.GetStackUpTo("feature-a")
		if stack == nil {
			t.Fatal("GetStackUpTo should return a stack for feature-a")
		}
		if len(stack.Segments) != 1 {
			t.Errorf("GetStackUpTo(feature-a) should have 1 segment, got %d", len(stack.Segments))
		}

		stack = graph.GetStackUpTo("feature-b")
		if stack == nil {
			t.Fatal("GetStackUpTo should return a stack for feature-b")
		}
		if len(stack.Segments) != 2 {
			t.Errorf("GetStackUpTo(feature-b) should have 2 segments, got %d", len(stack.Segments))
		}
	})
}
