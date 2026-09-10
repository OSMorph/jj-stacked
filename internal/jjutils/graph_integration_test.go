package jjutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

func TestRemoteStatusIncludesDownstackBookmarks(t *testing.T) {
	_, _, work := setupPushTestRepo(t)
	runCommand(t, work, "jj", "describe", "-m", "Base")
	runCommand(t, work, "jj", "bookmark", "rename", "feature", "main")
	jj := NewJJFunctions(cmdexec.NewRealExecutorInDir(work), "jj")
	previous := "main"
	for _, name := range []string{"main", "lower", "upper"} {
		if name != "main" {
			runCommand(t, work, "jj", "new", "-r", previous, "-m", name)
			if err := os.WriteFile(filepath.Join(work, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
			runCommand(t, work, "jj", "bookmark", "create", name)
		}
		if err := jj.Push(t.Context(), "origin", name); err != nil {
			t.Fatal(err)
		}
		previous = name
	}
	bookmarks, err := jj.ListBookmarksForRemote(t.Context(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(bookmarks) != 3 {
		t.Fatalf("bookmarks = %v", bookmarks)
	}
	for _, bookmark := range bookmarks {
		if bookmark.NeedsPushTo("origin") {
			t.Errorf("fully pushed bookmark needs push: %+v", bookmark)
		}
	}
	bookmarks, err = jj.ListBookmarksForRemote(t.Context(), "other")
	if err != nil {
		t.Fatal(err)
	}
	for _, bookmark := range bookmarks {
		if bookmark.HasRemote {
			t.Errorf("another remote supplied status: %+v", bookmark)
		}
	}
}

func TestGraphRetainsLongSegments(t *testing.T) {
	_, _, work := setupPushTestRepo(t)
	runCommand(t, work, "jj", "describe", "-m", "Lower")
	jj := NewJJFunctions(cmdexec.NewRealExecutorInDir(work), "jj")
	for range 105 {
		runCommand(t, work, "jj", "new", "-m", "Intermediate")
	}
	runCommand(t, work, "jj", "bookmark", "create", "upper")
	graph, err := jj.BuildChangeGraphForBookmark(t.Context(), "upper", "root()")
	if err != nil {
		t.Fatal(err)
	}
	if graph.ChildToParent["upper"] != "feature" || len(graph.Segments["upper"].Changes) != 105 {
		t.Fatalf("long segment lost its parent or changes: parents=%v segment=%+v", graph.ChildToParent, graph.Segments["upper"])
	}
}
