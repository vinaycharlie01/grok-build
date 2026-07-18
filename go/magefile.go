//go:build mage

// This file is the entire build interface for the Go port of grok-build:
// `mage -l` lists targets, `mage go:build` builds the binary, etc. Most
// targets delegate to github.com/nirantaraai/nava's typed Go runners,
// which themselves shell out to the `go` toolchain — there is no .sh file
// anywhere in this tree, by design (see the root go/README.md). Generate/
// GenerateCheck below are the two exceptions: nava has no go:generate
// support today, so they shell out to `go`/`git` directly via os/exec,
// the same underlying mechanism nava's own runners use — still no shell
// script, just no nava wrapper to delegate to yet.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
	gomagex "github.com/nirantaraai/nava/mage/golang"
)

// init loads go.yaml once before any target runs.
func init() {
	_ = gomagex.LoadConfig("go.yaml")
}

// Go namespaces every target under `mage go:<target>`.
type Go mg.Namespace

// Setup downloads and tidies module dependencies.
func (Go) Setup() error { return gomagex.Setup() }

// Build compiles the grok binary to bin/grok.
func (Go) Build() error { return gomagex.Build() }

// Run builds and runs the grok binary.
func (Go) Run() error { return gomagex.Run() }

// Test runs the unit test suite.
func (Go) Test() error { return gomagex.Test() }

// Race runs the test suite with the race detector enabled.
func (Go) Race() error { return gomagex.Race() }

// Integration runs the integration test suite (tests/integration/ —
// today: MongoDB via testcontainers-go). Requires a reachable Docker
// daemon; not part of Test/Race/Vet/Build above.
func (Go) Integration() error { return gomagex.Integration() }

// Coverage runs tests with coverage profiling.
func (Go) Coverage() error { return gomagex.Coverage() }

// Vet runs go vet.
func (Go) Vet() error { return gomagex.Vet() }

// Lint runs golangci-lint.
func (Go) Lint() error { return gomagex.Lint() }

// Format runs gofmt.
func (Go) Format() error { return gomagex.Format() }

// Generate runs `go generate ./...` — today, regenerating the
// counterfeiter fakes under internal/domain/ports/portsfakes from the
// //go:generate directives on each port interface (LLMProvider, Tool,
// ConfigStore, CredentialStore, SessionStore).
func (Go) Generate() error {
	cmd := exec.Command("go", "generate", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go generate ./...: %w", err)
	}
	return nil
}

// GenerateCheck regenerates and fails if that leaves anything uncommitted
// under internal/domain/ports/portsfakes, so a fake that's drifted from
// its source interface (someone hand-edited a fake, or changed an
// interface without regenerating) — or was never committed at all — is
// caught in CI instead of silently going stale. Deliberately uses `git
// status --porcelain`, not `git diff --exit-code`: the latter only shows
// changes to already-tracked files and would silently pass on a brand
// new, never-`git add`ed fake — exactly the bug this check exists to
// catch, so it can't quietly not-catch it too. Mirrors the shape of a
// docs-sync check, applied to generated code instead of docs.
func (Go) GenerateCheck() error {
	if err := (Go{}).Generate(); err != nil {
		return err
	}

	var out bytes.Buffer
	cmd := exec.Command("git", "status", "--porcelain", "--", "internal/domain/ports/portsfakes")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git status --porcelain: %w", err)
	}
	if out.Len() > 0 {
		return fmt.Errorf("generated fakes are stale or uncommitted — run `mage go:generate` and commit the result:\n%s", out.String())
	}
	return nil
}
