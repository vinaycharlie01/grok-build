package main

// Set via -ldflags at build time. nava's `versionPkg` build option
// (see go.yaml, wired up in ROADMAP.md Phase 11) auto-injects these three
// exact names — Version/Commit/BuildDate — via `git`, so don't rename
// them without updating that wiring too.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)
