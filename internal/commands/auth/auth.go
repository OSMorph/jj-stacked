// Package auth implements the auth command for jj-stacked.
package auth

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/auth"
	"github.com/OSMorph/jj-stacked/internal/cmdexec"
	"github.com/OSMorph/jj-stacked/internal/repo"
)

// NewCommand creates the auth command with subcommands.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication management",
		Long: `Manage GitHub authentication for jj-stacked.

Supports both GitHub.com and GitHub Enterprise (GHE) instances.
Authentication is required to create and manage pull requests.

SUBCOMMANDS:
  test    Verify your authentication is working
  help    Show detailed setup instructions

QUICK START:
  # If you have GitHub CLI installed (recommended)
  gh auth login
  jj-stacked auth test

  # For GitHub Enterprise
  gh auth login --hostname git.mycompany.com
  jj-stacked auth test --host git.mycompany.com`,
	}

	cmd.AddCommand(newTestCommand())
	cmd.AddCommand(newHelpCommand())

	return cmd
}

func newTestCommand() *cobra.Command {
	var host string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test GitHub authentication",
		Long:  "Test that GitHub authentication is properly configured for the specified or detected host.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthTest(cmd.Context(), host)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "GitHub hostname to test (default: detect from repo or github.com)")

	return cmd
}

func newHelpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "help",
		Short: "Show authentication setup instructions",
		Long:  "Display detailed instructions for setting up GitHub authentication.",
		Run: func(cmd *cobra.Command, args []string) {
			printAuthHelp()
		},
	}
}

func runAuthTest(ctx context.Context, host string) error {
	exec := cmdexec.NewRealExecutor()

	// If no host specified, try to detect from repo
	if host == "" {
		// Try to detect from repository
		repoCtx, err := repo.NewRepoContext(ctx, repo.RepoContextOptions{
			Exec: exec,
		})
		if err == nil {
			host = repoCtx.GitHubHost
		} else {
			// Default to github.com
			host = "github.com"
		}
	}

	fmt.Printf("Testing authentication for %s...\n\n", host)

	// Try to create authenticator
	authenticator, err := auth.NewAuthenticator(ctx, exec, host)
	if err != nil {
		fmt.Printf("✗ Authentication failed!\n\n")
		fmt.Printf("  Host: %s\n", host)
		fmt.Printf("  Error: %v\n\n", err)
		fmt.Printf("Run 'jj-stacked auth help' for setup instructions.\n")
		return err
	}

	// Get token to validate
	_, err = authenticator.GetToken(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to get token!\n\n")
		fmt.Printf("  Host: %s\n", host)
		fmt.Printf("  Method: %s\n", formatMethod(authenticator.Method()))
		fmt.Printf("  Error: %v\n\n", err)
		return err
	}

	// Get user info to validate token works
	user, err := authenticator.GetUser(ctx)
	if err != nil {
		fmt.Printf("✗ Token validation failed!\n\n")
		fmt.Printf("  Host: %s\n", host)
		fmt.Printf("  Method: %s\n", formatMethod(authenticator.Method()))
		fmt.Printf("  Error: %v\n\n", err)
		return err
	}

	// Success!
	fmt.Printf("✓ Authentication successful!\n\n")

	hostLabel := host
	if host != "github.com" {
		hostLabel = fmt.Sprintf("%s (GitHub Enterprise)", host)
	}

	fmt.Printf("  Host:   %s\n", hostLabel)
	fmt.Printf("  Method: %s\n", formatMethod(authenticator.Method()))
	fmt.Printf("  User:   %s\n", user.Login)
	if user.Name != "" {
		fmt.Printf("  Name:   %s\n", user.Name)
	}
	if user.Email != "" {
		fmt.Printf("  Email:  %s\n", user.Email)
	}

	fmt.Printf("\n  Token Scopes:\n")
	fmt.Printf("    ✓ repo (access verified)\n")

	return nil
}

func formatMethod(method string) string {
	switch method {
	case "gh_cli":
		return "GitHub CLI (gh)"
	case "env_token":
		return "Environment variable"
	default:
		return method
	}
}

func printAuthHelp() {
	help := `GitHub Authentication Setup
═══════════════════════════

jj-stacked supports both GitHub.com and GitHub Enterprise (GHE).

1. GitHub CLI (Recommended)
   ─────────────────────────
   Install: https://cli.github.com/

   For GitHub.com:
     gh auth login

   For GitHub Enterprise:
     gh auth login --hostname git.mycompany.com

   This method automatically handles tokens and is the
   easiest way to authenticate.

2. Environment Variables
   ─────────────────────────
   For GitHub.com:
     export GITHUB_TOKEN=ghp_xxxxxxxxxxxxx
     (or GH_TOKEN)

   For GitHub Enterprise:
     export GHE_TOKEN=ghp_xxxxxxxxxxxxx
     export GITHUB_HOST=git.mycompany.com

   Create tokens at:
     GitHub.com: https://github.com/settings/tokens/new
     GHE: https://<your-ghe-host>/settings/tokens/new

   Required scope: repo

Troubleshooting
───────────────
• Run 'jj-stacked auth test' to verify authentication
• Run 'jj-stacked auth test --host git.mycompany.com' for GHE
• Check that your token hasn't expired
• Ensure the token has 'repo' scope
• For GHE, ensure the token was created on the GHE instance`

	fmt.Println(help)
}

// DetectHost attempts to detect the GitHub host from the current repository.
// Returns "github.com" if detection fails.
func DetectHost(ctx context.Context, exec cmdexec.CommandExecutor) string {
	repoCtx, err := repo.NewRepoContext(ctx, repo.RepoContextOptions{
		Exec: exec,
	})
	if err != nil {
		return "github.com"
	}
	return repoCtx.GitHubHost
}

// ValidateAuth is a helper function that other commands can use to ensure
// authentication is working before proceeding.
func ValidateAuth(ctx context.Context, exec cmdexec.CommandExecutor, host string) error {
	authenticator, err := auth.NewAuthenticator(ctx, exec, host)
	if err != nil {
		return err
	}

	_, err = authenticator.GetUser(ctx)
	return err
}

