//go:build mage

// This file is the entire build interface for the Go port of grok-build:
// `mage -l` lists targets, `mage go:build` builds the binary, etc. Every
// target delegates to github.com/nirantaraai/nava's typed Go runners,
// which themselves shell out to the `go` toolchain — there is no .sh file
// anywhere in this tree, by design (see the root go/README.md).
package main

import (
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
