package jjutils

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// cleanBookmarkName removes jj display markers from bookmark names.
// jj appends markers like * (dirty/unsynced) and @ (remote tracking) to bookmark
// names in log output, but these aren't part of the actual bookmark name.
func cleanBookmarkName(name string) string {
	// Remove trailing markers: *, @remote, etc.
	name = strings.TrimSuffix(name, "*")
	// Handle @remote suffix (e.g., "bookmark@origin")
	if idx := strings.Index(name, "@"); idx > 0 {
		name = name[:idx]
	}
	return name
}

// GetConnectedBookmarks returns a bookmark's entire connected stack in
// deterministic base-to-tip order, including branching descendants.
func (g *ChangeGraph) GetConnectedBookmarks(bookmarkName string) []string {
	if _, ok := g.Bookmarks[bookmarkName]; !ok {
		return nil
	}
	root := bookmarkName
	for g.ChildToParent[root] != "" {
		root = g.ChildToParent[root]
	}

	depth := map[string]int{root: 0}
	queue := []string{root}
	var result []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)
		children := append([]string(nil), g.ParentToChildren[name]...)
		sort.Strings(children)
		for _, child := range children {
			depth[child] = depth[name] + 1
			queue = append(queue, child)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if depth[result[i]] == depth[result[j]] {
			return result[i] < result[j]
		}
		return depth[result[i]] < depth[result[j]]
	})
	return result
}

// BuildChangeGraph builds a complete change graph from the user's bookmarks.
// AIDEV-NOTE: This is the core algorithm that determines stack relationships.
//
// Algorithm:
// 1. List all user bookmarks (mine() ~ trunk())
// 2. For each bookmark, traverse toward trunk collecting changes
// 3. Stop when hitting another bookmark or trunk
// 4. Build adjacency lists (child→parent, parent→children)
// 5. Identify roots (bookmarks directly on trunk) and leaves
// 6. Detect merge commits and mark tainted bookmarks
// 7. Build complete stacks from roots to leaves
func (j *jjFunctions) BuildChangeGraph(ctx context.Context) (*ChangeGraph, error) {
	return j.BuildChangeGraphForBase(ctx, "trunk()")
}

// BuildChangeGraphForBase builds a bookmark graph relative to base.
func (j *jjFunctions) BuildChangeGraphForBase(ctx context.Context, base string) (*ChangeGraph, error) {
	graph := NewChangeGraph()

	// Step 1: Get all user bookmarks
	userBookmarks, err := j.ListUserBookmarksForBase(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("failed to list user bookmarks: %w", err)
	}

	if len(userBookmarks) == 0 {
		return graph, nil
	}

	// Store bookmarks in graph
	for _, bm := range userBookmarks {
		graph.Bookmarks[bm.Name] = bm
	}

	// Step 2-3: For each bookmark, find its segment and parent
	for _, bm := range userBookmarks {
		segment, parentBookmark, err := j.buildSegment(ctx, bm, graph.Bookmarks, base)
		if err != nil {
			return nil, fmt.Errorf("failed to build segment for %s: %w", bm.Name, err)
		}

		graph.Segments[bm.Name] = segment

		// Step 4: Build adjacency
		if parentBookmark != "" {
			graph.ChildToParent[bm.Name] = parentBookmark
			graph.ParentToChildren[parentBookmark] = append(graph.ParentToChildren[parentBookmark], bm.Name)
		}
	}

	// Step 5: Identify roots and leaves
	for name := range graph.Bookmarks {
		// Root: no parent bookmark (directly on trunk)
		if _, hasParent := graph.ChildToParent[name]; !hasParent {
			graph.Roots = append(graph.Roots, name)
		}

		// Leaf: no children
		if _, hasChildren := graph.ParentToChildren[name]; !hasChildren {
			graph.Leaves = append(graph.Leaves, name)
		}
	}

	// Step 6: Detect merge commits and mark tainted bookmarks
	for name, segment := range graph.Segments {
		for i := range segment.Changes {
			if segment.Changes[i].IsMergeCommit() {
				j.markTainted(graph, name)
				break
			}
		}
	}

	// Count excluded bookmarks
	graph.ExcludedCount = len(graph.TaintedBookmarks)

	// Step 7: Build complete stacks from roots to leaves
	graph.Stacks = j.buildStacks(graph)

	return graph, nil
}

// buildSegment builds a BookmarkSegment by traversing from the bookmark toward trunk.
// Returns the segment and the name of the parent bookmark (empty if trunk is parent).
func (j *jjFunctions) buildSegment(ctx context.Context, bm Bookmark, allBookmarks map[string]Bookmark, base string) (*BookmarkSegment, string, error) {
	segment := &BookmarkSegment{
		Bookmark: bm,
	}

	// Get changes from this bookmark toward trunk
	// We use ancestors to traverse backward, stopping at trunk or another bookmark
	revset := fmt.Sprintf("ancestors(%s, 100) ~ ::%s", bm.ChangeID, base)

	entries, err := j.GetLog(ctx, revset, 100)
	if err != nil {
		return nil, "", err
	}

	// Find where to stop: either at another bookmark or at the end (trunk)
	var segmentChanges []LogEntry
	var parentBookmark string

	for i := range entries {
		// Skip the bookmark's own change if it has the bookmark
		// (The first entry should be the bookmark itself)
		isOwnChange := entries[i].ChangeID == bm.ChangeID

		// Check if this change has another user bookmark
		for _, localBm := range entries[i].LocalBookmarks {
			// Strip jj display markers (* for dirty, @ for remote tracking)
			// These appear in log output but not in bookmark names
			cleanBm := cleanBookmarkName(localBm)
			if cleanBm != bm.Name {
				if _, isUserBookmark := allBookmarks[cleanBm]; isUserBookmark {
					// Found parent bookmark
					if !isOwnChange {
						parentBookmark = cleanBm
						goto done
					}
				}
			}
		}

		segmentChanges = append(segmentChanges, entries[i])
	}

done:
	// Reverse to get oldest first
	for i, j := 0, len(segmentChanges)-1; i < j; i, j = i+1, j-1 {
		segmentChanges[i], segmentChanges[j] = segmentChanges[j], segmentChanges[i]
	}

	segment.Changes = segmentChanges
	segment.Parent = parentBookmark

	return segment, parentBookmark, nil
}

// markTainted marks a bookmark and all its descendants as tainted.
func (j *jjFunctions) markTainted(graph *ChangeGraph, name string) {
	if graph.TaintedBookmarks[name] {
		return // Already marked
	}

	graph.TaintedBookmarks[name] = true

	// Mark all descendants
	for _, child := range graph.ParentToChildren[name] {
		j.markTainted(graph, child)
	}
}

// buildStacks builds complete stacks from roots to leaves.
// Each stack is a path from a root bookmark to a leaf bookmark.
func (j *jjFunctions) buildStacks(graph *ChangeGraph) []BranchStack {
	var stacks []BranchStack

	// For each leaf, build the path back to root
	for _, leaf := range graph.Leaves {
		// Skip tainted bookmarks
		if graph.TaintedBookmarks[leaf] {
			continue
		}

		stack := j.buildStackToLeaf(graph, leaf)
		if len(stack.Segments) > 0 {
			stacks = append(stacks, stack)
		}
	}

	return stacks
}

// buildStackToLeaf builds a stack from root to the given leaf.
func (j *jjFunctions) buildStackToLeaf(graph *ChangeGraph, leaf string) BranchStack {
	// Build path from leaf to root
	var path []string
	current := leaf

	for current != "" {
		// Skip if tainted
		if graph.TaintedBookmarks[current] {
			return BranchStack{}
		}

		path = append(path, current)
		current = graph.ChildToParent[current]
	}

	// Reverse to get root-to-leaf order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	// Build segments
	var segments []BookmarkSegment
	for _, name := range path {
		if segment, ok := graph.Segments[name]; ok {
			segments = append(segments, *segment)
		}
	}

	return BranchStack{Segments: segments}
}
