// Package jjutils provides Jujutsu integration for jj-stacked.
package jjutils

// LogEntry represents a single change/commit in the Jujutsu graph.
// AIDEV-NOTE: JSON tags match the template output from jj log.
type LogEntry struct {
	CommitID             string   `json:"commit_id"`
	ChangeID             string   `json:"change_id"`
	AuthorName           string   `json:"author_name"`
	AuthorEmail          string   `json:"author_email"`
	DescriptionFirstLine string   `json:"description_first_line"`
	Description          string   `json:"description"`
	Parents              []string `json:"parents"`
	LocalBookmarks       []string `json:"local_bookmarks"`
	RemoteBookmarks      []string `json:"remote_bookmarks"`
	IsWorkingCopy        bool     `json:"is_working_copy"`
	IsEmpty              bool     `json:"is_empty"`
	Conflict             bool     `json:"conflict"`
}

// IsMergeCommit returns true if this change has multiple parents.
func (e *LogEntry) IsMergeCommit() bool {
	return len(e.Parents) > 1
}

// HasBookmarks returns true if this change has any local bookmarks.
func (e *LogEntry) HasBookmarks() bool {
	return len(e.LocalBookmarks) > 0
}

// HasRemoteBookmarks returns true if this change has any remote bookmarks.
func (e *LogEntry) HasRemoteBookmarks() bool {
	return len(e.RemoteBookmarks) > 0
}

// Bookmark represents a jj bookmark with sync status.
type Bookmark struct {
	Name       string `json:"name"`
	CommitID   string `json:"commit_id"`
	ChangeID   string `json:"change_id"`
	HasRemote  bool   `json:"has_remote"`
	IsSynced   bool   `json:"is_synced"`
	RemoteName string `json:"remote_name"`
}

// PushOptions controls the safety exceptions used for a bookmark push.
type PushOptions struct {
	// AllowEmptyDescription opts into pushing changes without descriptions.
	// Callers should only set this when they provide a meaningful metadata
	// fallback, such as submit's bookmark-derived pull request title.
	AllowEmptyDescription bool
}

// NeedsPush returns true if this bookmark needs to be pushed to remote.
func (b *Bookmark) NeedsPush() bool {
	return !b.HasRemote || !b.IsSynced
}

// NeedsPushTo reports sync status for a specific remote.
func (b *Bookmark) NeedsPushTo(remote string) bool {
	return !b.HasRemote || b.RemoteName != remote || !b.IsSynced
}

// BookmarkSegment represents a contiguous section of changes between bookmarks.
// AIDEV-NOTE: A segment contains all changes from one bookmark down to
// either the parent bookmark or trunk.
type BookmarkSegment struct {
	Bookmark Bookmark   // The bookmark at the top of this segment
	Changes  []LogEntry // Changes in this segment (oldest first)
	Parent   string     // Parent bookmark name (empty if trunk is parent)
}

// BranchStack represents a complete stack from trunk to a leaf bookmark.
type BranchStack struct {
	Segments []BookmarkSegment // Ordered from base (near trunk) to top (leaf)
}

// TopBookmark returns the name of the top-most bookmark in this stack.
func (s *BranchStack) TopBookmark() string {
	if len(s.Segments) == 0 {
		return ""
	}
	return s.Segments[len(s.Segments)-1].Bookmark.Name
}

// AllBookmarks returns all bookmark names in this stack, ordered base to top.
func (s *BranchStack) AllBookmarks() []string {
	names := make([]string, len(s.Segments))
	for i, seg := range s.Segments {
		names[i] = seg.Bookmark.Name
	}
	return names
}

// ChangeGraph holds the complete analyzed graph of bookmarks and their relationships.
// AIDEV-NOTE: This is the primary data structure used by the submit workflow.
type ChangeGraph struct {
	// Bookmarks is all bookmarks indexed by name
	Bookmarks map[string]Bookmark

	// Segments is all segments indexed by bookmark name
	Segments map[string]*BookmarkSegment

	// ChildToParent maps child bookmark name to parent bookmark name
	ChildToParent map[string]string

	// ParentToChildren maps parent bookmark name to child bookmark names
	ParentToChildren map[string][]string

	// Roots are bookmarks that are directly on trunk (no parent bookmark)
	Roots []string

	// Leaves are bookmarks with no children
	Leaves []string

	// Stacks are all complete stacks from roots to leaves
	Stacks []BranchStack

	// TaintedBookmarks are bookmarks that contain or descend from merge commits
	TaintedBookmarks map[string]bool

	// ExcludedCount is the number of bookmarks excluded due to merge tainting
	ExcludedCount int
}

// NewChangeGraph creates an empty ChangeGraph.
func NewChangeGraph() *ChangeGraph {
	return &ChangeGraph{
		Bookmarks:        make(map[string]Bookmark),
		Segments:         make(map[string]*BookmarkSegment),
		ChildToParent:    make(map[string]string),
		ParentToChildren: make(map[string][]string),
		TaintedBookmarks: make(map[string]bool),
	}
}

// GetStack returns the stack that contains the given bookmark, or nil if not found.
func (g *ChangeGraph) GetStack(bookmarkName string) *BranchStack {
	for i := range g.Stacks {
		for _, seg := range g.Stacks[i].Segments {
			if seg.Bookmark.Name == bookmarkName {
				return &g.Stacks[i]
			}
		}
	}
	return nil
}

// GetStackUpTo returns a stack from the root up to and including the given bookmark.
// Returns nil if the bookmark is not found.
func (g *ChangeGraph) GetStackUpTo(bookmarkName string) *BranchStack {
	for i := range g.Stacks {
		for j, seg := range g.Stacks[i].Segments {
			if seg.Bookmark.Name == bookmarkName {
				return &BranchStack{
					Segments: g.Stacks[i].Segments[:j+1],
				}
			}
		}
	}
	return nil
}

// IsRoot returns true if the bookmark is a root (directly on trunk).
func (g *ChangeGraph) IsRoot(bookmarkName string) bool {
	for _, root := range g.Roots {
		if root == bookmarkName {
			return true
		}
	}
	return false
}

// IsLeaf returns true if the bookmark is a leaf (has no children).
func (g *ChangeGraph) IsLeaf(bookmarkName string) bool {
	for _, leaf := range g.Leaves {
		if leaf == bookmarkName {
			return true
		}
	}
	return false
}

// IsTainted returns true if the bookmark is tainted (contains or descends from merge).
func (g *ChangeGraph) IsTainted(bookmarkName string) bool {
	return g.TaintedBookmarks[bookmarkName]
}

// Remote represents a git remote.
type Remote struct {
	Name string
	URL  string
}
