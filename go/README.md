# grok-build (Go port)

This directory is a **pure-Go, goroutine-native, multi-provider** rewrite of
`grok-build`'s TUI coding agent, living side-by-side with the existing
`crates/` Rust tree while the port grows. It uses:

- **[Charm Bubble Tea](https://github.com/charmbracelet/bubbletea)** for the
  terminal UI (the Go analogue of the Rust `xai-grok-pager` crate).
- **Hexagonal architecture** (ports & adapters) to keep the agent/chat
  domain logic independent of any specific model backend, storage, or UI —
  see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
- **[nava](https://github.com/nirantaraai/nava)** (a Mage-based build
  toolkit) as the *only* build/test/lint path. There are no `.sh` files
  anywhere in this tree — every command below is a typed Go function.

This is **not** an xAI-only client. `ports.LLMProvider` is provider-agnostic
by design; xAI's Grok is the first backend behind it, with OpenAI,
Anthropic, Gemini, and OpenAI-compatible/local backends planned next. See
[`docs/ROADMAP.md`](docs/ROADMAP.md) for the full phased plan (multi-provider
support, goroutine/concurrency hardening, tool parity, MCP/ACP, sandboxing,
and more) with a task checklist per phase.

**`crates/` (the Rust source) is kept exactly as-is and is never modified by
this port** — it's the reference implementation to check behavior against
when a Go port task is ambiguous. See "Repository layout & reference
policy" in the roadmap.

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

### Running a subset of tests

`mage go:test` always runs `./...`; `go.yaml`'s `test.packages` isn't the
right place to iterate on one package while you're working. Use plain `go
test` directly for anything narrower — it reads the same `go.mod` and needs
no config changes:

```bash
# One package
go test ./internal/adapters/driven/llm/providers/openai/... -v

# One test by name (regex-matched against the func name)
go test ./internal/adapters/driven/llm/providers/openai/... -run TestStreamChatAssemblesToolCallAcrossChunks -v

# Race + coverage for one package while iterating
go test -race -cover ./internal/application/chatservice/...
```

Every adapter's tests are self-contained — `httptest.Server` fakes for HTTP
adapters (`llm/providers/*`, none of them make a real network call),
`t.TempDir()` for filesystem adapters — so none of this needs a live API key
or network access. Testing an LLM provider adapter against the real API
(not just its wire format) isn't wired up yet: today only `providers/xai`
is built into `cmd/grok/main.go`'s composition root; `providers/openai` and
the rest get a real end-to-end path once Phase 1's `llm/router` +
multi-provider config land (see `docs/ROADMAP.md`).

**Every unit test in this repo is TDD, not test-added-after**: write the
test against the behavior you're about to add, watch it fail for the right
reason, then write the minimum code to turn it green. See "Definition of
done" in [`docs/ROADMAP.md`](docs/ROADMAP.md) — this is a hard requirement
for every task in the roadmap, not a suggestion.

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
far vs. what's still Rust-only, and [`docs/ROADMAP.md`](docs/ROADMAP.md) for
the phased plan (multi-provider support, concurrency/performance hardening,
tool parity, MCP/ACP, sandboxing, telemetry, distribution) that gets it
there.
