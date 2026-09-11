package common

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// ResolveBookmark infers the closest bookmarked work around @. Ambiguity is
// explicit: a terminal offers a choice and scripts must supply a bookmark.
func ResolveBookmark(ctx context.Context, jj jjutils.JJFunctions, graph *jjutils.ChangeGraph, explicit string, input io.Reader, output io.Writer) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	candidates, err := BookmarkCandidates(ctx, jj, graph)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no bookmarked stack found around @; specify a bookmark explicitly")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if !Interactive(input) {
		return "", fmt.Errorf("ambiguous bookmark around @ (%s); specify one explicitly", strings.Join(candidates, ", "))
	}
	for i, name := range candidates {
		_, _ = fmt.Fprintf(output, "  %d. %s\n", i+1, name)
	}
	indices, err := NewPrompt(input, output).Select(len(candidates), false)
	if err != nil {
		return "", err
	}
	if len(indices) == 0 {
		return "", fmt.Errorf("bookmark selection cancelled")
	}
	return candidates[indices[0]], nil
}

// BookmarkCandidates prefers references at @, then the next bookmarked
// descendants when editing within a segment, then the closest ancestors.
func BookmarkCandidates(ctx context.Context, jj jjutils.JJFunctions, graph *jjutils.ChangeGraph) ([]string, error) {
	for _, revset := range []string{"@", "roots(@:: & bookmarks())", "heads(::@ & bookmarks())"} {
		entries, err := jj.GetLog(ctx, revset, 0)
		if err != nil {
			return nil, fmt.Errorf("infer bookmark around @: %w", err)
		}
		seen := map[string]bool{}
		for i := range entries {
			for _, name := range entries[i].LocalBookmarks {
				if _, ok := graph.Bookmarks[name]; ok {
					seen[name] = true
				}
			}
		}
		if len(seen) > 0 {
			names := make([]string, 0, len(seen))
			for name := range seen {
				names = append(names, name)
			}
			sort.Strings(names)
			return names, nil
		}
	}
	return nil, nil
}
