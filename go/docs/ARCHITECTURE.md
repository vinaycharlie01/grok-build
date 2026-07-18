# Architecture: hexagonal layout + Rust → Go migration map

> This document explains the current shape of the code and how it maps to
> the Rust tree. For the phased plan to get from here to a pure-Go,
> goroutine-native, multi-provider agent — including the concurrency
> architecture and the full provider/tool/MCP/ACP task list — see
> [`ROADMAP.md`](ROADMAP.md).

## Why hexagonal

The Rust tree is 75 crates deep (`crates/codegen/`, `crates/common/`,
`crates/build/` — the exact count, verified via each crate's `Cargo.toml`;
see `ROADMAP.md`'s "Full Rust crate inventory" for the crate-by-crate port
status) covering the
TUI, agent runtime, tool execution, workspace/VCS, sandboxing, MCP, ACP,
hooks/plugins, memory, telemetry, and more. Porting that in one pass isn't
realistic or honest work. Hexagonal architecture (ports & adapters) is what
makes an *incremental* port possible: the domain and application layers
never import a concrete adapter, so any future PR can add or replace one
adapter (a real gRPC tool runtime instead of `shellexec`, an OAuth
`CredentialStore` instead of `env`, an ACP-driving adapter alongside `tui`)
without touching the layers that already work.

This is also why `internal/application/chatservice` is a hand-rolled loop
rather than built on a third-party Go agent framework (Google ADK Go, Eino,
OpenAI Agents Go): every one of those frameworks has its own opinion about
what a "tool" or "provider" is, and adopting one as the core would put that
framework's abstractions where `ports.LLMProvider`/`ports.Tool` are today —
the opposite of what this hexagon buys us. See `ROADMAP.md`'s "Library &
framework choices" section for the full reasoning and where those
frameworks' ideas *do* get used (as design references for later phases).

```
                     ┌─────────────────────────┐
   driving adapters  │   internal/adapters/     │  driven adapters
   (call in)         │       driving/           │  (called out to)
                      ─────────────────────────
        tui  ───────▶ │                          │ ◀─────── openai + anthropic
   (future: cli,      │   internal/application/  │          (both SDK-backed; openai
    headless, ACP)    │      chatservice         │          also serves xAI +
                      │        (use cases)        │          OpenAI-compat — ROADMAP.md)
                      └──────────┬────────────────┘          config/file, credentials/env
                                 │                             tools/shellexec
                      ┌──────────▼────────────────┐            tools/readfile
                      │   internal/domain/         │  (future: openai/
                      │  chat, ports, settings      │   anthropic/gemini
                      │  (no external dependencies) │   providers + router,
                      └─────────────────────────────┘   gRPC tool runtime,
                                                          OAuth, MCP)
                      │   internal/domain/         │
                      │  chat, ports, settings      │
                      │  (no external dependencies) │
                      └─────────────────────────────┘
```

- **`internal/domain`** — `chat.Message`/`chat.Session` (the conversation
  aggregate), `ports.*` (interfaces every adapter implements or calls
  through), `settings.Config`. No imports outside the standard library.
- **`internal/application/chatservice`** — the one use case ported so far:
  send a user message, stream the model's reply, execute any requested
  tools, feed results back, repeat until the model stops asking for tools
  or the hop limit trips. Depends only on `ports`.
- **`internal/adapters/driven/*`** — implement domain ports against the
  outside world (HTTP API, filesystem, env vars, subprocesses).
- **`internal/adapters/driving/tui`** — the only thing calling
  `chatservice.Service` today. Driving adapters call the application layer
  directly (not through a port — ports are for the things application code
  calls *out* to).

## What's ported vs. what's still Rust-only

This slice proves the architecture end-to-end (config → auth → model call →
tool-call loop → TUI) but covers a small fraction of the Rust surface.

| Go package | Rust crate(s) it stands in for | Status |
|---|---|---|
| `domain/chat`, `application/chatservice` | `xai-grok-agent`, `xai-chat-state`, `xai-prompt-queue` | Minimal vertical slice (single-session, optional persistence via `ports.SessionStore` — see the `sessionstore/mongo` row below — no subagents, no prompt queue yet) |
| `adapters/driven/llm/providers/openai` | `xai-grok-http`, model-facing parts of `xai-grok-shell` | Backed by the official `openai-go` SDK. Serves xAI, OpenAI, and any OpenAI-compatible endpoint (all speak the same wire format) by base URL alone — see `cmd/grok/provider.go`. |
| `adapters/driven/llm/providers/anthropic` | same Rust crates, Claude-specific path | Backed by the official `anthropic-sdk-go` SDK; a genuinely different `ports.LLMProvider` implementation, not a base-URL variant of the OpenAI one — Anthropic's Messages API has a different wire shape (system prompt as a top-level field, no "tool" role, `X-Api-Key` auth). Gemini/Ollama-native next (see `ROADMAP.md` Phase 1); no leader/relay/remote modes |
| `adapters/driven/config/file` | `xai-grok-config`, `xai-grok-config-types` | One YAML file with a `providers:` list (`settings.ProviderConfig` — name/kind/baseURL/model/credential-env-var, selectable by name) + `defaultProvider`; no managed-config layering, no TOML editing |
| `internal/domain/settings` (`ModelInfo`, `APIBackend`) | `xai-grok-models` (`default_models.json`) | Optional per-provider `models:` catalog — id/name/description/context window/temperature/top_p/api backend/supported-in-API, looked up via `ProviderConfig.ModelInfo(id)`; selected via `--model`/`GROK_MODEL` (same precedence shape as `--provider`/`GROK_PROVIDER`). Metadata only today — nothing consumes context window for auto-compact yet (Phase 4); no remote-settings layer, no CLI-flag>env>config>remote-settings chain (just flag>env>config) |
| `adapters/driven/credentials/env` | `xai-grok-auth`, `xai-grok-secrets` | Env-var API key only — **no OAuth device-code flow yet** |
| `adapters/driven/tools/shellexec`, `tools/readfile`, `tools/writefile`, `tools/search` | `xai-grok-tools`, `xai-grok-tools-api` (see its `grok-tools.proto`) | 4 of many tools; in-process `ports.Tool`, not the real gRPC `GrokToolsService`. `search` is a pure-Go grep (`regexp`/`filepath.WalkDir`), no external binary |
| `adapters/driven/sessionstore/mongo` | `xai-chat-state`'s persistence surface | Backed by the official `mongo-go-driver/v2`; opt-in via `settings.Config.SessionStore` (`sessionStore:` in the config file). `Save`/`Load`/`Delete` are integration-tested against a real `mongod` via `tests/integration/` (testcontainers-go) — **that suite is unexecuted in this repo's authoring environment** (Docker registry access was blocked there; see ROADMAP.md Phase 4's caveat) — run `mage go:integration` yourself before trusting it in production. BSON document mapping is separately unit-tested (no live server needed) and passing |
| `adapters/driving/tui` | `xai-grok-pager`, `xai-grok-pager-render`, `xai-ratatui-*` | Single scrollback + input line; no modals/slash-commands/scrollback search/themes |
| *(not started)* | `xai-acp-lib` (ACP), `xai-grok-mcp` (MCP), `xai-grok-sandbox`, `xai-grok-memory`, `xai-grok-telemetry`, `xai-grok-markdown*`/`xai-grok-mermaid`, `xai-hooks-plugins-types`, `xai-grok-plugin-marketplace`, `xai-grok-voice`, `xai-codebase-graph`, leader/relay/remote in `xai-grok-shell`, checkpoints/worktrees (`xai-fast-worktree`, `xai-hunk-tracker`) | Still Rust-only |

## Extending the port

Adding the next slice of functionality follows the same shape every time:

1. **Domain first**: if the feature needs new state, add it to
   `internal/domain` with no adapter imports.
2. **Port**: define the interface in `internal/domain/ports` describing what
   the application layer needs, not how it's implemented.
3. **Application**: wire the new port into `chatservice` (or a new use case
   package) using only the interface.
4. **Adapter(s)**: implement the port under `adapters/driven/<name>` (or add
   a new driving adapter under `adapters/driving/<name>`).
5. **Tests**: unit-test the application layer against a hand-written fake of
   the port (see `chatservice/service_test.go`); unit-test each adapter in
   isolation (`httptest.Server` for HTTP, `t.TempDir()` for filesystem —
   see the existing adapter tests for the pattern).
6. **Wire it up** in `cmd/grok/main.go`, the only file allowed to import both
   a concrete adapter and the application layer.

## Build system

Everything above is built, tested, and linted exclusively through
[nava](https://github.com/nirantaraai/nava) + Mage — see the root
[`README.md`](../README.md) and [`magefile.go`](../magefile.go). No `.sh`
files exist in this tree; that's a hard constraint for this port, not
incidental.
