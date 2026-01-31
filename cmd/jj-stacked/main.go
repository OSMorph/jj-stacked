package main

import (
	"os"

	"github.com/spf13/cobra"

	analyzecmd "github.com/OSMorph/jj-stacked/internal/commands/analyze"
	authcmd "github.com/OSMorph/jj-stacked/internal/commands/auth"
	completioncmd "github.com/OSMorph/jj-stacked/internal/commands/completion"
	submitcmd "github.com/OSMorph/jj-stacked/internal/commands/submit"
	synccmd "github.com/OSMorph/jj-stacked/internal/commands/sync"
	"github.com/OSMorph/jj-stacked/internal/ui"
)

// version is set at build time via -ldflags
var version = "dev"

// Global flags
var (
	debug   bool
	noColor bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "jj-stacked",
	Short: "Manage stacked pull requests for Jujutsu repositories",
	Long: `jj-stacked is a CLI tool for creating and managing stacked pull requests
on GitHub for developers using Jujutsu (jj) version control.

When run without a subcommand, it launches an interactive graph view showing
your bookmark stacks and their sync status with GitHub.

WORKFLOW:
  1. Create changes and bookmarks in jj as normal
  2. Run 'jj-stacked' to visualize your stacks
  3. Run 'jj-stacked submit <bookmark>' to create PRs

EXAMPLES:
  # View your bookmark stacks interactively
  jj-stacked

  # Submit a bookmark and all its downstack PRs
  jj-stacked submit my-feature

  # Preview what would happen without making changes
  jj-stacked submit my-feature --dry-run

  # Check your GitHub authentication
  jj-stacked auth test`,
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		// When run without subcommands, launch the interactive graph UI
		// Handle --no-color flag
		if noColor {
			ui.DisableColors()
		}
		return analyzecmd.RunDefault(cmd.Context())
	},
}

func init() {
	// Setup Flags
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	// Add subcommands
	rootCmd.AddCommand(analyzecmd.NewCommand())
	rootCmd.AddCommand(submitcmd.NewCommand())
	rootCmd.AddCommand(authcmd.NewCommand())
	rootCmd.AddCommand(synccmd.NewCommand())
	rootCmd.AddCommand(completioncmd.NewCommand(rootCmd))
}
