package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
			"arguments to launch the interactive TUI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Reject unrecognized subcommands/args instead of silently falling
		// through to the TUI launch below — `grok bogus` should error, not
		// try to start a chat session.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive()
		},
	}

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
			return runInteractive()
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
