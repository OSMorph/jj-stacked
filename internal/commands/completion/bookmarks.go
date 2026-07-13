package completion

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

const bookmarkCompletionTimeout = 750 * time.Millisecond

type localBookmarkLister interface {
	ListLocalBookmarks(context.Context) ([]jjutils.Bookmark, error)
}

// CompleteUserBookmarks returns bookmarks accepted by submit and sync.
func CompleteUserBookmarks(ctx context.Context, jj localBookmarkLister) ([]string, error) {
	bookmarks, err := jj.ListLocalBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(bookmarks))
	for _, bookmark := range bookmarks {
		if bookmark.Name == "main" || bookmark.Name == "master" || bookmark.Name == "trunk" {
			continue
		}
		result = append(result, bookmark.Name)
	}
	return result, nil
}

// BookmarkValidArgsFunction dynamically completes local jj user bookmarks.
func BookmarkValidArgsFunction(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), bookmarkCompletionTimeout)
	defer cancel()
	jj := jjutils.NewJJFunctions(cmdexec.NewRealExecutor(), "")
	bookmarks, err := CompleteUserBookmarks(ctx, jj)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return bookmarks, cobra.ShellCompDirectiveNoFileComp
}
