# Architecture: hexagonal layout + Rust → Go migration map

## Why hexagonal

The Rust tree is ~65 crates deep (`crates/codegen/xai-grok-*`) covering the
TUI, agent runtime, tool execution, workspace/VCS, sandboxing, MCP, ACP,
hooks/plugins, memory, telemetry, and more. Porting that in one pass isn't
realistic or honest work. Hexagonal architecture (ports & adapters) is what
makes an *incremental* port possible: the domain and application layers
never import a concrete adapter, so any future PR can add or replace one
adapter (a real gRPC tool runtime instead of `shellexec`, an OAuth
`CredentialStore` instead of `env`, an ACP-driving adapter alongside `tui`)
without touching the layers that already work.

```
                     ┌─────────────────────────┐
   driving adapters  │   internal/adapters/     │  driven adapters
   (call in)         │       driving/           │  (called out to)
                      ─────────────────────────
        tui  ───────▶ │                          │ ◀─────── xai (LLM)
   (future: cli,      │   internal/application/  │          config/file
    headless, ACP)    │      chatservice         │          credentials/env
                      │        (use cases)        │          tools/shellexec
                      └──────────┬────────────────┘          tools/readfile
                                 │                    (future: gRPC tool
                      ┌──────────▼────────────────┐    runtime, OAuth, MCP)
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
| `domain/chat`, `application/chatservice` | `xai-grok-agent`, `xai-chat-state`, `xai-prompt-queue` | Minimal vertical slice (single-session, no persistence, no subagents) |
| `adapters/driven/llm/xai` | `xai-grok-http`, model-facing parts of `xai-grok-shell` | OpenAI-compatible streaming chat only; no leader/relay/remote modes |
| `adapters/driven/config/file` | `xai-grok-config`, `xai-grok-config-types` | One flat YAML file; no managed-config layering, no TOML editing |
| `adapters/driven/credentials/env` | `xai-grok-auth`, `xai-grok-secrets` | Env-var API key only — **no OAuth device-code flow yet** |
| `adapters/driven/tools/shellexec`, `tools/readfile` | `xai-grok-tools`, `xai-grok-tools-api` (see its `grok-tools.proto`) | 2 of many tools; in-process `ports.Tool`, not the real gRPC `GrokToolsService` |
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
