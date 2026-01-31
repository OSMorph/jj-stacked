package submit

import (
	"context"
	"fmt"
	"strings"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AnalyzeSubmission analyzes the change graph to determine what needs to be submitted.
// This is a pure function with no side effects - it only reads from the graph.
// AIDEV-NOTE: The analysis phase identifies the stack from trunk to target bookmark
// and extracts metadata needed for PR creation.
func AnalyzeSubmission(
	ctx context.Context,
	graph *jjutils.ChangeGraph,
	targetBookmark string,
) (*AnalysisResult, error) {
	result := &AnalysisResult{
		TargetBookmark: targetBookmark,
	}

	// 1. Validate target bookmark exists
	bookmark, exists := graph.Bookmarks[targetBookmark]
	if !exists {
		result.Errors = append(result.Errors,
			fmt.Errorf("bookmark '%s' not found", targetBookmark))
		return result, nil
	}

	// 2. Check bookmark is not tainted (merge commits)
	if graph.IsTainted(targetBookmark) {
		result.Errors = append(result.Errors,
			fmt.Errorf("bookmark '%s' contains or descends from merge commits and cannot be submitted as a stack", targetBookmark))
		return result, nil
	}

	// 3. Get the stack up to this bookmark
	stack := graph.GetStackUpTo(targetBookmark)
	if stack == nil {
		// This shouldn't happen if the bookmark exists and isn't tainted
		result.Errors = append(result.Errors,
			fmt.Errorf("could not find stack for bookmark '%s'", targetBookmark))
		return result, nil
	}

	// 4. Build the StackBookmark list from trunk to target
	for _, segment := range stack.Segments {
		sb := StackBookmark{
			Bookmark:  segment.Bookmark,
			Segment:   graph.Segments[segment.Bookmark.Name],
			NeedsPush: segment.Bookmark.NeedsPush(),
		}

		// Extract title and body from the segment's changes
		sb.Title, sb.Body = extractPRContent(&segment)

		// Add warnings for potential issues
		if sb.Title == "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("bookmark '%s' has no description; PR title will use bookmark name", segment.Bookmark.Name))
			sb.Title = segment.Bookmark.Name
		}

		result.Stack = append(result.Stack, sb)
	}

	// Add warning for empty stack
	if len(result.Stack) == 0 {
		result.Warnings = append(result.Warnings, "no bookmarks found in stack")
	}

	// Validate the single bookmark case (direct on trunk)
	if len(result.Stack) == 1 && graph.IsRoot(targetBookmark) {
		// This is fine - single PR targeting default branch
		_ = bookmark // use bookmark
	}

	return result, nil
}

// extractPRContent extracts the PR title and body from a bookmark segment.
// The title comes from the first line of the latest commit description.
// The body comes from the full description (excluding first line).
func extractPRContent(segment *jjutils.BookmarkSegment) (title, body string) {
	// Use the latest change in the segment (last in array since oldest is first)
	if len(segment.Changes) == 0 {
		return "", ""
	}

	// Get the topmost change (the one at the bookmark)
	topChange := segment.Changes[len(segment.Changes)-1]

	// Title is the first line
	title = strings.TrimSpace(topChange.DescriptionFirstLine)

	// Body is the full description minus the first line
	fullDesc := strings.TrimSpace(topChange.Description)
	if fullDesc != "" && title != "" {
		// Remove the first line from the full description for the body
		lines := strings.SplitN(fullDesc, "\n", 2)
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
	}

	return title, body
}

// ValidateForSubmission performs additional validation on an analysis result.
// Call this before creating a submission plan.
func ValidateForSubmission(result *AnalysisResult) []error {
	var errors []error

	if result.HasErrors() {
		errors = append(errors, result.Errors...)
		return errors
	}

	if len(result.Stack) == 0 {
		errors = append(errors, fmt.Errorf("no bookmarks to submit"))
		return errors
	}

	return errors
}

// GetStackSummary returns a human-readable summary of the stack.
func GetStackSummary(result *AnalysisResult) string {
	if len(result.Stack) == 0 {
		return "empty stack"
	}

	var names []string
	for i := len(result.Stack) - 1; i >= 0; i-- {
		names = append(names, result.Stack[i].Bookmark.Name)
	}

	return strings.Join(names, " → ") + " → main"
}
