package completion

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type fakeBookmarkLister struct {
	bookmarks []jjutils.Bookmark
	err       error
}

func (f fakeBookmarkLister) ListUserBookmarks(context.Context) ([]jjutils.Bookmark, error) {
	return f.bookmarks, f.err
}

func TestCompleteUserBookmarks(t *testing.T) {
	got, err := CompleteUserBookmarks(context.Background(), fakeBookmarkLister{bookmarks: []jjutils.Bookmark{
		{Name: "feature-a"}, {Name: "feature-b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"feature-a", "feature-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCompleteUserBookmarksPropagatesError(t *testing.T) {
	want := errors.New("not a jj repo")
	_, err := CompleteUserBookmarks(context.Background(), fakeBookmarkLister{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}
