// Package analyze implements the analyze command for jj-stacked.
package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	apperrors "github.com/OSMorph/jj-stacked/internal/errors"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
	"github.com/OSMorph/jj-stacked/internal/ui"
)

// Options configures the analyze command.
type Options struct {
	JSON    bool
	NoFetch bool
	Debug   bool
}

// NewCommand creates the analyze command.
func NewCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze and display bookmark stacks",
		Long: `Analyze the repository and display bookmark stacks.

By default, launches an interactive UI showing all bookmark stacks with their
sync status. Navigate with arrow keys, press Enter to select a bookmark for
submission, or 'q' to quit.

The display shows:
  • Bookmark names and their parent relationships
  • Number of changes in each segment
  • Sync status (whether the bookmark needs pushing)
  • Any bookmarks excluded due to merge commits

EXAMPLES:
  # Launch interactive stack viewer
  jj-stacked analyze

  # Output stack information as JSON (for scripting)
  jj-stacked analyze --json

  # Skip fetching from remotes (faster, but may be outdated)
  jj-stacked analyze --no-fetch

JSON OUTPUT:
  The --json flag outputs structured data including:
  • stacks: Array of bookmark stacks with segments
  • excluded_count: Number of bookmarks excluded due to merges
  • warnings: Any issues detected`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON instead of interactive UI")
	cmd.Flags().BoolVar(&opts.NoFetch, "no-fetch", false, "Skip fetching from remotes (faster but may be outdated)")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "Enable debug output for troubleshooting")

	return cmd
}

// AnalyzeOutput is the JSON output format for the analyze command.
type AnalyzeOutput struct {
	Stacks        []StackOutput `json:"stacks"`
	ExcludedCount int           `json:"excluded_count"`
	Warnings      []string      `json:"warnings,omitempty"`
}

// StackOutput represents a stack in JSON output.
type StackOutput struct {
	Bookmarks []string        `json:"bookmarks"`
	Segments  []SegmentOutput `json:"segments"`
}

// SegmentOutput represents a segment in JSON output.
type SegmentOutput struct {
	Bookmark    string `json:"bookmark"`
	ChangeCount int    `json:"change_count"`
	IsSynced    bool   `json:"is_synced"`
	NeedsPush   bool   `json:"needs_push"`
	Parent      string `json:"parent,omitempty"`
}

func runAnalyze(ctx context.Context, opts *Options) error {
	exec := cmdexec.NewRealExecutor()
	jj := jjutils.NewJJFunctions(exec, "")

	// Optionally fetch from remotes
	if !opts.NoFetch {
		fmt.Fprintf(os.Stderr, "Fetching from remotes...\n")
		if err := jj.FetchAllRemotes(ctx); err != nil {
			// Non-fatal - continue with local state
			fmt.Fprintf(os.Stderr, "Warning: Could not fetch from remotes (continuing with local state)\n")
			if opts.Debug {
				fmt.Fprintf(os.Stderr, "  Detail: %v\n", err)
			}
		}
	}

	// Build change graph
	fmt.Fprintf(os.Stderr, "Analyzing bookmark stacks...\n")
	graph, err := jj.BuildChangeGraph(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %s\n", apperrors.FormatErrorWithHint(err))
		return fmt.Errorf("failed to analyze repository")
	}

	// Clear status line
	fmt.Fprintf(os.Stderr, "\033[2K\r")

	// Output JSON or launch UI
	if opts.JSON {
		return outputJSON(graph)
	}

	return launchUI(ctx, graph, jj)
}

func outputJSON(graph *jjutils.ChangeGraph) error {
	output := AnalyzeOutput{
		Stacks:        make([]StackOutput, 0, len(graph.Stacks)),
		ExcludedCount: graph.ExcludedCount,
	}

	for _, stack := range graph.Stacks {
		stackOut := StackOutput{
			Bookmarks: stack.AllBookmarks(),
			Segments:  make([]SegmentOutput, 0, len(stack.Segments)),
		}

		for _, seg := range stack.Segments {
			segOut := SegmentOutput{
				Bookmark:    seg.Bookmark.Name,
				ChangeCount: len(seg.Changes),
				IsSynced:    seg.Bookmark.IsSynced,
				NeedsPush:   seg.Bookmark.NeedsPush(),
				Parent:      seg.Parent,
			}
			stackOut.Segments = append(stackOut.Segments, segOut)
		}

		output.Stacks = append(output.Stacks, stackOut)
	}

	// Add warnings
	if graph.ExcludedCount > 0 {
		output.Warnings = append(output.Warnings,
			fmt.Sprintf("%d bookmark(s) excluded due to merge commits", graph.ExcludedCount))
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func launchUI(ctx context.Context, graph *jjutils.ChangeGraph, jj jjutils.JJFunctions) error {
	// Create refresh function for the UI
	refreshFn := func() (*jjutils.ChangeGraph, error) {
		return jj.BuildChangeGraph(ctx)
	}

	selectedBookmark, err := ui.RunGraphViewWithRefresh(graph, refreshFn)
	if err != nil {
		return fmt.Errorf("UI error: %w", err)
	}

	if selectedBookmark != "" {
		// User selected a bookmark - print it for the caller to use
		fmt.Printf("Selected: %s\n", selectedBookmark)
		fmt.Printf("Run: jj-stacked submit %s\n", selectedBookmark)
	}

	return nil
}

// RunDefault is called when jj-stacked is run without a subcommand.
// It launches the interactive graph view.
func RunDefault(ctx context.Context) error {
	return runAnalyze(ctx, &Options{
		JSON:    false,
		NoFetch: false,
	})
}
