package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// providerFlag is the name of the one flag governing provider selection.
// Its GROK_PROVIDER env var equivalent is handled in resolveProviderName
// (provider.go); everything else about a provider — endpoint, model,
// credential — lives in the config file, not in flags.
const providerFlag = "provider"

// newRootCmd builds the grok command tree. Bare `grok` (no subcommand)
// launches the interactive TUI — same as `grok run` — so existing muscle
// memory and scripts keep working; `grok version` and future subcommands
// (a headless mode, an mcp-server mode — see ROADMAP.md Phase 5) live
// alongside it without disturbing that default.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "grok",
		Short: "Grok Build — a terminal AI coding agent",
		Long: "Grok Build is a terminal-based AI coding agent. Run it with no\n" +
			"arguments to launch the interactive TUI.\n\n" +
			"Which model backend it talks to is configured in the config file's\n" +
			"providers: list (see README.md), selected by name via --provider or\n" +
			"GROK_PROVIDER, falling back to the config's defaultProvider.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Reject unrecognized subcommands/args instead of silently falling
		// through to the TUI launch below — `grok bogus` should error, not
		// try to start a chat session.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(providerFlagValue(cmd))
		},
	}
	root.PersistentFlags().String(providerFlag, "", "provider to use (by name, from the config file's providers: list); overrides GROK_PROVIDER")

	root.AddCommand(newRunCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Launch the interactive TUI (same as running grok with no subcommand)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(providerFlagValue(cmd))
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "grok %s (commit %s, built %s)\n", Version, Commit, BuildDate)
			return nil
		},
	}
}

// providerFlagValue reads --provider off cmd (a persistent flag, so this
// works from any subcommand, not just root). The flag is always defined
// on the root command, so a lookup miss here would be a programming error,
// not a user-facing one — an empty string is a safe zero value either way.
func providerFlagValue(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString(providerFlag)
	return v
}
