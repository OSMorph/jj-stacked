// Package completion provides shell completion generation for jj-stacked.
package completion

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCommand creates the completion command.
func NewCommand(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for jj-stacked (and jjk alias).

To load completions:

Bash:
  $ source <(jjk completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ jjk completion bash > /etc/bash_completion.d/jjk
  # macOS:
  $ jjk completion bash > $(brew --prefix)/etc/bash_completion.d/jjk

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ jjk completion zsh > "${fpath[1]}/_jjk"
  # You may need to start a new shell for this setup to take effect.

Fish:
  $ jjk completion fish | source
  # To load completions for each session, execute once:
  $ jjk completion fish > ~/.config/fish/completions/jjk.fish

PowerShell:
  PS> jjk completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> jjk completion powershell > jjk.ps1
  # and source this file from your PowerShell profile.

Generate the script using the command name you want to complete. For example,
'jjk completion zsh' registers jjk, while 'jj-stacked completion zsh' registers
jj-stacked.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	}

	return cmd
}
