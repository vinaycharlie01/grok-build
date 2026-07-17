package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewRootCmdHasExpectedSubcommands(t *testing.T) {
	root := newRootCmd()

	if root.Use != "grok" {
		t.Fatalf("Use = %q, want %q", root.Use, "grok")
	}
	if root.RunE == nil {
		t.Fatal("root.RunE is nil, want bare `grok` (no subcommand) to launch the TUI")
	}

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"version", "run"} {
		if !names[want] {
			t.Errorf("missing subcommand %q, have %v", want, names)
		}
	}
}

func TestVersionCommandPrintsVersionInfo(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, Version) {
		t.Fatalf("version output = %q, want it to contain Version %q", got, Version)
	}
}

func TestRunSubcommandRunEIsSetWithoutLaunchingTUI(t *testing.T) {
	root := newRootCmd()

	var runCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "run" {
			runCmd = c
		}
	}
	if runCmd == nil {
		t.Fatal("no run subcommand found")
	}
	if runCmd.RunE == nil {
		t.Fatal("run subcommand's RunE is nil")
	}
}

func TestUnknownSubcommandErrors(t *testing.T) {
	root := newRootCmd()
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{"bogus-subcommand"})

	// Must reject unrecognized subcommands rather than silently falling
	// through to root's own RunE (which would try to launch the TUI for
	// any typo).
	if err := root.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error for an unrecognized subcommand")
	}
}
