package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// AIDEV-NOTE: This file implements the structured comment format for stack navigation.
// Comments are placed on each PR to show where it fits in the stack.

const (
	// CommentSignature is the footer signature for jj-stacked comments.
	CommentSignature = "Created with [jj-stacked](https://github.com/OSMorph/jj-stacked)"

	// MetadataPrefix marks the start of embedded metadata.
	MetadataPrefix = "<!--- JJ-STACK_INFO: "

	// MetadataSuffix marks the end of embedded metadata.
	MetadataSuffix = " --->"

	// StackCommentVersion is the current version of the comment format.
	// Version 2 adds MergedHistory for tracking merged PRs from the stack.
	StackCommentVersion = 2
)

// StackCommentData is embedded in comments as base64-encoded JSON.
type StackCommentData struct {
	Version   int               `json:"version"`
	Bookmarks []string          `json:"bookmarks"`
	PRNumbers map[string]int    `json:"pr_numbers"`
	PRURLs    map[string]string `json:"pr_urls"`
	// MergedHistory tracks PRs that were merged from this stack.
	// Added in version 2.
	MergedHistory []MergedPRInfo `json:"merged_history,omitempty"`
}

// MergedPRInfo represents a PR that was merged from this stack.
type MergedPRInfo struct {
	Bookmark   string `json:"bookmark"`
	PRNumber   int    `json:"pr_number"`
	PRURL      string `json:"pr_url"`
	MergedInto string `json:"merged_into"` // The base branch it merged into
}

// StackEntry represents a single entry in the stack for comment generation.
type StackEntry struct {
	Bookmark string
	PRNumber int
	PRURL    string
	IsMerged bool
}

// BuildStackComment creates the full comment body for a PR.
// currentBookmark indicates which PR this comment is being placed on.
// mergedHistory contains PRs that were previously merged from this stack.
func BuildStackComment(entries []StackEntry, currentBookmark, baseBranch string, mergedHistory []MergedPRInfo) string {
	var sb strings.Builder

	// Build metadata
	data := StackCommentData{
		Version:       StackCommentVersion,
		Bookmarks:     make([]string, len(entries)),
		PRNumbers:     make(map[string]int),
		PRURLs:        make(map[string]string),
		MergedHistory: mergedHistory,
	}

	for i, e := range entries {
		data.Bookmarks[i] = e.Bookmark
		if e.PRNumber > 0 {
			data.PRNumbers[e.Bookmark] = e.PRNumber
		}
		if e.PRURL != "" {
			data.PRURLs[e.Bookmark] = e.PRURL
		}
	}

	// Encode metadata as base64 JSON
	metadataJSON, _ := json.Marshal(data)
	metadataB64 := base64.StdEncoding.EncodeToString(metadataJSON)

	// Write hidden metadata
	sb.WriteString(MetadataPrefix)
	sb.WriteString(metadataB64)
	sb.WriteString(MetadataSuffix)
	sb.WriteString("\n\n")

	// Write human-readable header
	sb.WriteString(fmt.Sprintf("This PR is part of a stack of %d bookmark(s):\n\n", len(entries)))

	// Write base branch
	sb.WriteString(fmt.Sprintf("1. `%s` (base)\n", baseBranch))

	// Write each entry
	for i, e := range entries {
		idx := i + 2 // 1-indexed, starting after base

		var line string
		if e.PRNumber > 0 {
			// Has PR
			if e.IsMerged {
				line = fmt.Sprintf("%d. ~~[%s](%s)~~ (merged)", idx, e.Bookmark, e.PRURL)
			} else if e.Bookmark == currentBookmark {
				line = fmt.Sprintf("%d. **[%s](%s) ← this PR**", idx, e.Bookmark, e.PRURL)
			} else {
				line = fmt.Sprintf("%d. [%s](%s)", idx, e.Bookmark, e.PRURL)
			}
		} else {
			// No PR yet
			if e.Bookmark == currentBookmark {
				line = fmt.Sprintf("%d. **`%s` ← this PR**", idx, e.Bookmark)
			} else {
				line = fmt.Sprintf("%d. `%s` (not yet submitted)", idx, e.Bookmark)
			}
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// Write merged history section if there are merged PRs
	if len(mergedHistory) > 0 {
		sb.WriteString("\n### Merged\n\n")
		for _, m := range mergedHistory {
			sb.WriteString(fmt.Sprintf("- ~~[%s](%s)~~ → merged into `%s`\n",
				m.Bookmark, m.PRURL, m.MergedInto))
		}
	}

	// Write footer
	sb.WriteString("\n---\n")
	sb.WriteString("*")
	sb.WriteString(CommentSignature)
	sb.WriteString("*\n")

	return sb.String()
}

// ParseStackComment extracts metadata from an existing comment.
// Returns nil if the comment doesn't contain valid jj-stacked metadata.
func ParseStackComment(body string) (*StackCommentData, error) {
	// Find metadata markers
	startIdx := strings.Index(body, MetadataPrefix)
	if startIdx == -1 {
		return nil, nil
	}

	startIdx += len(MetadataPrefix)
	endIdx := strings.Index(body[startIdx:], MetadataSuffix)
	if endIdx == -1 {
		return nil, nil
	}

	// Extract and decode base64
	b64Data := body[startIdx : startIdx+endIdx]
	jsonData, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	// Parse JSON
	var data StackCommentData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &data, nil
}

// IsStackComment checks if a comment was created by jj-stacked.
func IsStackComment(body string) bool {
	return strings.Contains(body, MetadataPrefix) && strings.Contains(body, CommentSignature)
}
