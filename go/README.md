# grok-build (Go port)

This directory is a **pure-Go, goroutine-native, multi-provider** rewrite of
`grok-build`'s TUI coding agent, living side-by-side with the existing
`crates/` Rust tree while the port grows. It uses:

- **[Charm Bubble Tea](https://github.com/charmbracelet/bubbletea)** for the
  terminal UI (the Go analogue of the Rust `xai-grok-pager` crate).
- **Hexagonal architecture** (ports & adapters) to keep the agent/chat
  domain logic independent of any specific model backend, storage, or UI —
  see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
- **Official SDKs, never a hand-rolled HTTP client.** Every LLM provider is
  a thin `ports.LLMProvider` adapter around that vendor's official Go SDK
  (or the closest wire-compatible one — see below). There is no
  hand-written `net/http`/SSE parsing anywhere in this tree.
- **[nava](https://github.com/nirantaraai/nava)** (a Mage-based build
  toolkit) as the *only* build/test/lint path. There are no `.sh` files
  anywhere in this tree — every command below is a typed Go function.

This is **not** an xAI-only client. `ports.LLMProvider` is provider-agnostic
by design. Built today: **xAI, OpenAI, and any OpenAI-compatible endpoint**
(via [`github.com/openai/openai-go`](https://github.com/openai/openai-go) —
xAI has no SDK of its own, but its API is OpenAI-wire-compatible, so it
needs none) and **Anthropic** (via
[`github.com/anthropics/anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go),
a genuinely different wire format, not a base-URL variant of the OpenAI
client). Gemini and a native Ollama client are next. See
[`docs/ROADMAP.md`](docs/ROADMAP.md) for the full phased plan (remaining
providers, goroutine/concurrency hardening, tool parity, MCP/ACP,
sandboxing, and more) with a task checklist per phase.

**`crates/` (the Rust source) is kept exactly as-is and is never modified by
this port** — it's the reference implementation to check behavior against
when a Go port task is ambiguous. See "Repository layout & reference
policy" in the roadmap.

## Requirements

- Go 1.25+
- [Mage](https://magefile.org/): `go install github.com/magefile/mage@latest`
- An API key for whichever provider you want to talk to — `XAI_API_KEY` by
  default, or see "Running it against a different provider" below for
  OpenAI/Anthropic/OpenAI-compatible (including local servers that need no
  paid key at all).

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
`llm/providers/{openai,anthropic}` adapters (neither ever makes a real
network call in tests), `t.TempDir()` for filesystem adapters — so none of
this needs a live API key or network access. Testing against a *real* API
is a separate thing from the unit tests: see "Running it against a
different provider" below for that — it works today (xAI, OpenAI,
Anthropic, and any OpenAI-compatible endpoint are all wired into
`cmd/grok/main.go` already), it just needs a real key and network access,
which the unit tests deliberately avoid needing.

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

That's the same as `grok` or `grok run` on the built binary — bare
invocation launches the TUI. `grok version` prints version info without
touching any of the chat/provider machinery, and `grok --help` lists every
subcommand:

```bash
$ bin/grok version
grok dev (commit unknown, built unknown)
```

(`dev`/`unknown` until Phase 11 wires up build-time `-ldflags` injection —
see `ROADMAP.md`.)

On start it loads `$GROK_HOME/config.yaml` (falling back to
`~/.grok/config.yaml`), or built-in defaults if neither exists.

### Running it against a different provider

Every provider — which endpoint, which model, which credential — is a
named entry in your config file's `providers:` list, not a pile of env
vars. Selecting *which one* is active is the only thing a flag/env var
controls: `--provider <name>` (falls back to `GROK_PROVIDER`, falls back
to the config's `defaultProvider`). Adding a new provider — even one
speaking to a service nobody's heard of — means adding an entry to the
config file, never changing Go code, as long as it speaks one of the two
wire formats already implemented (`kind: openai` or `kind: anthropic`).

If no config file exists yet, `settings.Default()` pre-populates three
entries — `xai` (active by default), `openai`, `anthropic` — each pointed
at its real API with the matching env var name (`XAI_API_KEY`,
`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`). Edit `$GROK_HOME/config.yaml` (or
`~/.grok/config.yaml`) to change models, add your own endpoints, or change
the default:

```yaml
defaultProvider: xai
systemPrompt: "You are Grok Build, a terminal coding agent."
providers:
  - name: xai
    kind: openai       # wire-format family, not vendor - see below
    baseURL: https://api.x.ai/v1
    model: grok-4
    apiKeyEnvVar: XAI_API_KEY

  - name: openai
    kind: openai
    baseURL: https://api.openai.com/v1
    model: gpt-4o
    apiKeyEnvVar: OPENAI_API_KEY

  - name: anthropic
    kind: anthropic     # Claude's Messages API - a genuinely different
    baseURL: https://api.anthropic.com  # wire format, not just a base URL
    model: claude-sonnet-5
    apiKeyEnvVar: ANTHROPIC_API_KEY

  # Anything OpenAI-wire-compatible works the same way - no code change,
  # just another entry. A local Ollama server:
  - name: home-ollama
    kind: openai
    baseURL: http://localhost:11434/v1
    model: llama3
    apiKeyEnvVar: ""    # empty = no credential needed (env.NoAuth) -
                        # most local servers don't check auth at all

  # Or OpenRouter:
  - name: openrouter
    kind: openai
    baseURL: https://openrouter.ai/api/v1
    model: openai/gpt-4o-mini   # or any model OpenRouter serves
    apiKeyEnvVar: OPENROUTER_API_KEY
```

`kind: openai` covers xAI, OpenAI itself, and any OpenAI-compatible
endpoint (OpenRouter, Groq, a local Ollama/vLLM server, ...) through the
one SDK-backed `providers/openai.Client`. `kind: anthropic` uses
`providers/anthropic.Client` — Claude's Messages API isn't OpenAI-wire-compatible, so it gets its own client, not a base-URL variant of the
OpenAI one. There is no hand-rolled HTTP client for any provider; see
ROADMAP.md's "Library & framework choices" for why.

Then pick one:

```bash
export XAI_API_KEY=sk-...
mage go:run                          # uses defaultProvider (xai)

grok run --provider home-ollama      # no API key needed at all
GROK_PROVIDER=anthropic grok run     # env var works the same as the flag
```

If you don't have an xAI key and just want to confirm the binary itself
works, a local `home-ollama`-style entry needs no paid API key at all.

The selection logic (`resolveProviderName` in `cmd/grok/provider.go`,
`Config.Provider` in `internal/domain/settings`) is unit tested —
`go test ./cmd/grok/... ./internal/domain/settings/... -v` — against fake
env vars and an in-memory config, so you don't need any of these servers
running to verify the *logic* is correct; you only need one running to
actually talk to a model. An unconfigured `--provider` name fails fast
with a clear error naming what you asked for, not a generic "no
credentials" message three layers down.

## Layout

```
go/
  cmd/grok/                composition root (main.go: runInteractive wires
                            every adapter together) — the only place
                            concrete adapters get wired together;
                            provider.go is just resolveProviderName
                            (--provider flag > GROK_PROVIDER > config
                            default — see "Running it"); cli.go is the
                            Cobra command tree (run, version); version.go
                            holds the ldflags-injectable vars
  internal/domain/         chat entities + ports (LLMProvider, Tool,
                            ConfigStore, CredentialStore) + settings
                            (Config.Providers, Config.Provider(name)) —
                            zero external deps
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
