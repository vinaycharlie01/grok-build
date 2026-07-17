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

Every adapter's tests are self-contained — `httptest.Server` fakes for the
`llm/providers/openai` adapter (it never makes a real network call in
tests), `t.TempDir()` for filesystem adapters — so none of this needs a
live API key or network access. Testing against a *real* API is a separate
thing from the unit tests: see "Running it against a different provider"
below for that — it works today (xAI, OpenAI, and any OpenAI-compatible
endpoint are all wired into `cmd/grok/main.go` already), it just needs a
real key and network access, which the unit tests deliberately avoid
needing.

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

### Running it against a different provider

`cmd/grok` defaults to xAI, but reads `GROK_PROVIDER` to pick a different
backend for manual testing. This is a stopgap ahead of Phase 1's real
`llm/router` + multi-provider config (see `docs/ROADMAP.md`) — it selects
exactly one provider from an env var, it doesn't route across several.

`xai`, `openai`, and `openaicompat` all build the exact same
`providers/openai.Client` (backed by the official `openai-go` SDK) — only
the base URL, model, and credential differ, since all three speak the same
wire format. `anthropic` builds a separate `providers/anthropic.Client`
(backed by the official `anthropic-sdk-go` SDK) because Claude's Messages
API is a genuinely different wire format, not just a different base URL.
There is no hand-rolled HTTP client for any provider; see ROADMAP.md's
"Library & framework choices" for why.

| `GROK_PROVIDER` | Required env vars | Base URL / notes |
|---|---|---|
| `xai` (default) | `XAI_API_KEY` | from config (`https://api.x.ai/v1` by default) |
| `openai` | `OPENAI_API_KEY`, optional `GROK_MODEL` (default `gpt-4o`) | `https://api.openai.com/v1` |
| `openaicompat` | `GROK_BASE_URL`, `GROK_MODEL`, `GROK_API_KEY` | **your** base URL — this is how you point it at OpenRouter, Groq, a local Ollama/vLLM server, or anything else that speaks the OpenAI chat-completions wire format. |
| `anthropic` | `ANTHROPIC_API_KEY`, optional `GROK_MODEL` (default `claude-sonnet-5`) | `https://api.anthropic.com` |

Example: a local Ollama server (`ollama serve`, OpenAI-compatible mode is
built in at `/v1`):

```bash
export GROK_PROVIDER=openaicompat
export GROK_BASE_URL=http://localhost:11434/v1
export GROK_MODEL=llama3
export GROK_API_KEY=ollama   # most local servers ignore the value but still require the header to be present
mage go:run
```

Example: OpenRouter:

```bash
export GROK_PROVIDER=openaicompat
export GROK_BASE_URL=https://openrouter.ai/api/v1
export GROK_MODEL=openai/gpt-4o-mini   # or any model OpenRouter serves
export GROK_API_KEY=sk-or-...
mage go:run
```

Example: Anthropic:

```bash
export GROK_PROVIDER=anthropic
export ANTHROPIC_API_KEY=sk-ant-...
mage go:run
```

If you don't have an xAI key and just want to confirm the binary itself
works, `openaicompat` against a local server needs no paid API key at all.

The selection logic (`resolveProviderChoice` in `cmd/grok/provider.go`) is
unit tested — `go test ./cmd/grok/... -v` — against fake env vars, so you
don't need any of these servers running to verify the *logic* is correct;
you only need one running to actually talk to a model.

## Layout

```
go/
  cmd/grok/                composition root (main.go) — the only place
                            concrete adapters get wired together;
                            provider.go picks xai/openai/openaicompat/
                            anthropic from GROK_PROVIDER (see "Running it")
  internal/domain/         chat entities + ports (LLMProvider, Tool,
                            ConfigStore, CredentialStore) — zero external deps
  internal/application/    chatservice: the model/tool-call loop, depends
                            only on domain ports
  internal/adapters/driven/    llm/providers/{openai,anthropic} — openai is
                                SDK-backed and also serves xAI and any
                                OpenAI-compatible endpoint (no separate xai
                                package); anthropic is its own SDK-backed
                                client (different wire format) —
                                config/file, credentials/env,
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
