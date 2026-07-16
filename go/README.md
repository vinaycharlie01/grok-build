# grok-build (Go port)

This directory is the start of a Go rewrite of `grok-build`'s TUI coding
agent, living side-by-side with the existing `crates/` Rust tree while the
port grows. It uses:

- **[Charm Bubble Tea](https://github.com/charmbracelet/bubbletea)** for the
  terminal UI (the Go analogue of the Rust `xai-grok-pager` crate).
- **Hexagonal architecture** (ports & adapters) to keep the agent/chat
  domain logic independent of any specific model backend, storage, or UI —
  see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
- **[nava](https://github.com/nirantaraai/nava)** (a Mage-based build
  toolkit) as the *only* build/test/lint path. There are no `.sh` files
  anywhere in this tree — every command below is a typed Go function.

## Requirements

- Go 1.25+
- [Mage](https://magefile.org/): `go install github.com/magefile/mage@latest`
- An xAI API key exported as `XAI_API_KEY` to actually talk to a model.

## Build & test — always via `mage`, never a shell script

```bash
mage -l              # list every available target
mage go:setup         # go mod download + tidy
mage go:build          # builds bin/grok
mage go:run             # builds and launches the TUI
mage go:test              # go test ./...
mage go:race                # go test -race ./...
mage go:coverage              # go test -cover ./..., writes coverage.out
mage go:vet                     # go vet ./...
mage go:lint                      # golangci-lint run ./...
mage go:format                      # gofmt ./...
```

Every target's actual flags (packages, build output path, ldflags, ...) live
in [`go.yaml`](go.yaml), not in `magefile.go` — see nava's own README for the
config-driven pattern this follows.

Plain `go build ./...` / `go test ./...` also work directly (nava just wraps
them); `mage` is what CI and contributors should use so the command surface
stays identical for every tool this repo will eventually wrap (`ko`, `helm`,
...).

## Running it

```bash
export XAI_API_KEY=sk-...
mage go:run
```

On start it loads `$GROK_HOME/config.yaml` (falling back to
`~/.grok/config.yaml`), or built-in defaults if neither exists.

## Layout

```
go/
  cmd/grok/                composition root (main.go) — the only place
                            concrete adapters get wired together
  internal/domain/         chat entities + ports (LLMProvider, Tool,
                            ConfigStore, CredentialStore) — zero external deps
  internal/application/    chatservice: the model/tool-call loop, depends
                            only on domain ports
  internal/adapters/driven/    xai (LLM client), config/file, credentials/env,
                                tools/{shellexec,readfile}
  internal/adapters/driving/   tui (Bubble Tea)
  magefile.go, go.yaml     the nava/Mage build definition
```

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how this maps onto
the ~65-crate Rust closure under `../crates/codegen/` and what's ported so
far vs. what's still Rust-only.
