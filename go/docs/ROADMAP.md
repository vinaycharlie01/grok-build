# Roadmap: pure-Go, multi-provider, goroutine-native grok-build

Phase 0 (the initial scaffold: hexagonal skeleton, one provider, two tools,
a Bubble Tea TUI, nava/Mage build) is done and merged. This document plans
everything after it.

## Vision

A **pure-Go**, **goroutine-native**, **high-performance**, **multi-provider**
AI coding agent — not an xAI-only client. Any AI backend (xAI, OpenAI,
Anthropic, Google Gemini, OpenAI-compatible local/self-hosted models) plugs
into the same `ports.LLMProvider` interface the way OmniRoute plugs 238
providers behind its executor/translator layer. The Bubble Tea TUI and
hexagonal architecture from Phase 0 don't change — everything below extends
them, it doesn't replace them.

## Repository layout & reference policy

- **`crates/`** (Rust, SpaceXAI's original grok-build) stays exactly where
  it is and is **never modified** by this port. It is the reference
  implementation: when a roadmap task is ambiguous about *what* behavior to
  port, read the matching Rust crate there first (the crate-mapping table in
  [`ARCHITECTURE.md`](ARCHITECTURE.md) says which one). Treat it as
  read-only documentation, not a dependency.
- **`go/`** is the entire Go implementation. Every task below lands here.
- If a task seems to require changing something under `crates/`, it's out
  of scope — split it into "Go-side task" + a separate note, don't touch
  the Rust tree.

## Guiding principles

1. **Provider-agnostic core.** `internal/domain` and
   `internal/application/chatservice` never import a vendor SDK or know
   which AI backend answered a turn. Multi-provider support is entirely an
   `internal/adapters/driven/llm/*` concern.
2. **Goroutine-native, leak-free concurrency.** Every I/O-bound boundary
   (HTTP call, tool exec, file I/O) is designed to run concurrently *and*
   to unwind cleanly when its context is cancelled. The concrete pattern to
   copy is the SSE-reader fix already in `llm/xai/client.go`: every send on
   a channel a producer goroutine owns is wrapped in a `select` against
   `ctx.Done()`, so a consumer that stops draining early can never leave
   the producer blocked forever. **No task in this roadmap is done until
   its goroutines provably can't outlive their context** — verify with
   `-race` plus a leak detector (see Phase 2).
3. **Ports before adapters.** New capability always starts as an interface
   in `internal/domain/ports`, gets a fake for application-layer tests, and
   only then gets a real adapter.
4. **Test-driven, not test-added.** Every unit test in this repo is written
   red→green: write the test against the interface/behavior you're about to
   add, run it and watch it fail for the right reason (compile error is not
   a valid "red" — the test must build and fail on an assertion), then write
   the minimum implementation to turn it green, then refactor with the test
   as your guard rail. This applies to every layer — a fake `ports.Tool`/
   `ports.LLMProvider` and a table-driven test come *before* the adapter
   that will satisfy it, not after. A PR whose tests were written by
   observing already-working code is not TDD and doesn't meet this bar,
   even if coverage looks identical. Table-driven tests for
   domain/application, `httptest`/fakes for adapters, benchmarks for hot
   concurrency paths (Phase 2) — same tools as before, TDD is about the
   *order*, not a new test style.
5. **Pure Go where practical.** Prefer pure-Go dependencies over cgo
   (e.g. `modernc.org/sqlite` over `mattn/go-sqlite3` in Phase 4) so
   `go.yaml`'s `noCGO: true` cross-compiles stay possible. Note exceptions
   explicitly if a phase truly needs cgo.
6. **Build only through nava/Mage.** No shell scripts, ever — new targets
   go in `magefile.go` + `go.yaml`, same as Phase 0.

## Concurrency & performance architecture (cross-cutting, informs every phase)

| Concern | Design |
|---|---|
| **Provider fan-out** (fallback racing, ensemble synthesis) | `errgroup`-based concurrent calls to N providers, mirroring OmniRoute's combo routing / fusion strategy — see Phase 1's `llm/router`. |
| **Tool execution concurrency** | Bounded worker pool (buffered channel + N workers) so a turn requesting many tool calls at once doesn't spawn unbounded goroutines. Currently `chatservice.run` executes tool calls sequentially in a loop — Phase 2 makes this concurrent with a parallelism cap. |
| **Session concurrency** | A session manager (Phase 2) supports many concurrent `chatservice.Send` turns across sessions via a `sync.Map`/mutex-guarded registry, required before any headless/server mode. |
| **Streaming backpressure** | SSE/stream consumers use a bounded channel with an explicit, documented policy for a slow reader (block-with-timeout, not unbounded buffering) so a stalled TUI can't grow memory without limit. |
| **Cancellation discipline** | Every goroutine selects on `ctx.Done()` before any potentially-blocking channel send/receive it doesn't otherwise control. This is a required code-review checklist item, not a suggestion. |
| **Resilience** | Per-provider circuit breaker (CLOSED/OPEN/HALF_OPEN, lazy recovery) — a Go-native take on the 3-layer resilience model in OmniRoute's `RESILIENCE_GUIDE.md` (provider breaker / connection cooldown / model lockout), scoped down to what a single-process CLI agent needs. |
| **Observability** | `log/slog` structured logging throughout (nava's runners already use it); goroutine-count and in-flight-request gauges; pprof behind a debug build tag. |
| **Benchmarks** | `go test -bench` for the chat loop under concurrent tool calls and for SSE parsing throughput, wired as `mage go:bench`. |

## Multi-provider architecture

- New adapter tree: `internal/adapters/driven/llm/providers/{xai,openai,anthropic,gemini,openaicompat}`, each implementing the existing `ports.LLMProvider` interface unchanged — no port changes required, because the interface was already provider-agnostic from Phase 0.
- New composite adapter `internal/adapters/driven/llm/router`: itself a `ports.LLMProvider` that wraps N other `ports.LLMProvider`s and applies a routing strategy (`priority`, `round-robin` first; more later). Because it satisfies the same port, `chatservice` needs zero changes to go from one provider to a routed pool of them.
- `settings.Config` grows a `Providers []ProviderConfig` list (name, base URL, credential var, model list, default flag) and a `Router {Strategy string; Order []string}` section.
- `CredentialStore` becomes provider-keyed; the `env` adapter reads e.g. `XAI_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`. OAuth adapters (matching Rust's `xai-grok-auth` device-code flow) are a later, explicit task per provider — not blocking Phase 1.

## Phases & tasks

### Phase 0 — Foundation ✅ done
- [x] Hexagonal skeleton (`domain` → `application/chatservice` → `adapters/{driving,driven}`).
- [x] xAI provider (`llm/xai`), file config, env credentials.
- [x] `shell_exec` + `read_file` tools.
- [x] Bubble Tea TUI.
- [x] nava/Mage build (`magefile.go`, `go.yaml`), zero shell scripts.
- [x] Unit tests across every layer, `-race` clean.

### Phase 1 — Multi-provider support
- [ ] Move `adapters/driven/llm/xai` → `adapters/driven/llm/providers/xai` (namespace only, no behavior change; update `cmd/grok/main.go` import).
- [ ] `providers/openai`: OpenAI Chat Completions streaming (very close to the xAI wire format already implemented — expect high code reuse).
- [ ] `providers/anthropic`: Claude Messages API streaming — different SSE event shape (`content_block_delta`, `message_delta`, tool_use blocks) requiring real translation logic, not a copy-paste of the OpenAI-style parser.
- [ ] `providers/gemini`: Google Generative Language API streaming (`generateContent` with `alt=sse` or the streaming endpoint) — also a distinct wire format.
- [ ] `providers/openaicompat`: generic OpenAI-compatible adapter (configurable base URL + model) covering OpenRouter, Groq, local vLLM/Ollama/llama.cpp servers — reuses the OpenAI wire format with a config-supplied base URL.
- [ ] `llm/router`: composite `ports.LLMProvider` with `priority` and `round-robin` strategies to start.
- [ ] Extend `settings.Config` (`Providers`, `Router`) and the `file` config adapter's YAML shape; keep `settings.Default()` backward compatible with Phase 0's single-provider config.
- [ ] Extend `CredentialStore` to be provider-keyed; update the `env` adapter.
- [ ] Update `cmd/grok/main.go` to build the provider set + router from config instead of a single hardcoded `xai.New(...)`.
- [ ] Tests: `httptest` fakes per provider's wire format (mirror `llm/xai/client_test.go`'s `json.Marshal`-based fixture pattern — hand-typed SSE JSON is a proven bug source, don't repeat it); router tests covering success, fallback-on-error, and all-providers-failed.
- [ ] Docs: update `ARCHITECTURE.md`'s provider table; this ROADMAP's Phase 1 checklist gets checked off item by item as PRs land.

### Phase 2 — Concurrency & performance hardening
- [ ] `internal/adapters/driven/llm/resilience`: circuit breaker wrapping any `ports.LLMProvider` (CLOSED/OPEN/HALF_OPEN, config-driven threshold + reset timeout, lazy recovery on read — no background timer goroutine needed, matching the lazy-recovery pattern OmniRoute uses).
- [ ] Concurrent tool-call execution in `chatservice.run`: replace the sequential `for _, call := range calls` loop with `errgroup.WithContext` bounded by a configurable max-parallelism, preserving deterministic ordering of tool-result messages appended back to the session.
- [ ] `internal/application/sessionmanager`: registry of concurrent sessions (`sync.Map` or mutex-guarded map), needed before any multi-session/headless mode.
- [ ] Streaming backpressure policy documented and enforced on every SSE consumer (bounded channel, explicit slow-consumer behavior).
- [ ] Add `go.uber.org/goleak` (or equivalent) to every adapter test package's `TestMain`, catching goroutine leaks automatically instead of relying on manual review.
- [ ] Benchmarks: `BenchmarkChatServiceConcurrentToolCalls`, `BenchmarkSSEParse`; wire `mage go:bench` (new `bench` section in `go.yaml`, new `Go.Bench` target in `magefile.go`, same pattern as existing targets).
- [ ] pprof HTTP endpoint behind a `debug` build tag or `GROK_DEBUG=1` env check (loopback-only, never exposed by default).

### Phase 3 — Tool ecosystem parity
Rust reference: `xai-grok-tools`, `xai-grok-tools-api` (`grok-tools.proto`).
- [ ] `tools/writefile`: create/overwrite a file within the workspace root (same path-escape guard as `readfile`).
- [ ] `tools/editfile`: hunk-based patch application (Rust reference: `xai-hunk-tracker`).
- [ ] `tools/search`: content + glob search (ripgrep-backed via `shellexec`-style subprocess, or a pure-Go grep for the no-external-binary case).
- [ ] `tools/git`: status/diff/commit — a **runtime agent tool** (like `shellexec`), not project build tooling; still zero `.sh` files.
- [ ] `tools.Registry`: replace the hardcoded `[]ports.Tool{...}` slice in `main.go` with a registry adapters register themselves into, so adding a tool never touches the composition root's tool list by hand.
- [ ] JSON Schema validation of tool arguments before `Execute` is called, so malformed model-generated args produce a clear tool-result error instead of an adapter-specific panic/failure.
- [ ] Tests for every new tool (`t.TempDir()`-based, following `readfile`/`shellexec`'s existing pattern).

### Phase 4 — Session, memory & checkpoints
Rust reference: `xai-chat-state`, `xai-prompt-queue`, `xai-grok-memory`, `xai-fast-worktree`, `xai-hunk-tracker`.
- [ ] `ports.SessionStore` + a JSON-file adapter (start simple); SQLite adapter later using a **pure-Go** driver (`modernc.org/sqlite`), consistent with the pure-Go principle.
- [ ] Prompt queue for multi-turn interleaving (Rust reference: `xai-prompt-queue`).
- [ ] Long-term memory port + simple file-backed adapter; FTS5 or vector search is an explicit stretch goal, not required for Phase 4 to ship.
- [ ] Checkpoint/worktree snapshotting before risky tool calls, git-worktree-based (Rust reference: `xai-fast-worktree`).

### Phase 5 — MCP (Model Context Protocol)
Rust reference: `xai-grok-mcp`.
- [ ] `ports.MCPClient`: connect to external MCP servers (stdio/SSE/HTTP transports); expose their tools dynamically as `ports.Tool` implementations through the Phase 3 registry.
- [ ] Stretch: embedded MCP *server* mode exposing grok's own tools.

### Phase 6 — ACP (Agent Client Protocol)
Rust reference: `xai-acp-lib`.
- [ ] `adapters/driving/acp`: a second driving adapter implementing the ACP JSON-RPC surface so editors (e.g. Zed) can drive `chatservice` exactly like the TUI does. This phase's real deliverable is proof that the hexagon holds — **zero application-layer changes** should be required to add it.

### Phase 7 — Sandbox & security
Rust reference: `xai-grok-sandbox`.
- [ ] Sandboxed tool-execution port + adapter gating `shellexec`/file-write tools (namespaces/seccomp on Linux, `sandbox-exec` on macOS, container fallback elsewhere).
- [ ] Path/command allow/deny policy, config-driven.

### Phase 8 — Extensibility: hooks & plugins
Rust reference: `xai-hooks-plugins-types`, `xai-grok-plugin-marketplace`.
- [ ] Hook port (pre/post tool-call, pre/post turn) + config-driven external-command or in-process adapters.
- [ ] Plugin manifest loading (list/enable/disable to start; marketplace fetch is a stretch goal).

### Phase 9 — TUI parity & polish
Rust reference: `xai-grok-pager*`, `xai-ratatui-*`.
- [ ] Slash-command palette.
- [ ] Modal dialogs (risky-tool-call confirmation, model/provider picker — ties directly into Phase 1's router).
- [ ] Scrollback search.
- [ ] Theming (color profiles).
- [ ] Markdown/code-block rendering with syntax highlighting (first cut of `xai-grok-markdown` parity).
- [ ] Diff rendering for file edits (pairs with Phase 3's `editfile`).

### Phase 10 — Observability & telemetry
Rust reference: `xai-grok-telemetry`, `xai-mixpanel`.
- [ ] `log/slog` structured logging end-to-end through the agent (not just nava's build-time runners).
- [ ] Opt-in telemetry port + local-file sink adapter by default; a remote sink adapter stays behind explicit operator opt-in — data-collection features default OFF, matching the opt-in-only stance this kind of feature needs.

### Phase 11 — Distribution & release
- [ ] `mage go:crossBuild` wired to nava's `CrossBuild` target (darwin/linux/windows × amd64/arm64), config in `go.yaml`'s `crossBuild` section.
- [ ] Version/commit/build-date ldflags via nava's `versionPkg` build option (`go.yaml`'s `build.versionPkg`).
- [ ] Release checklist doc for this Go binary (separate from the Rust `grok` release process).

## Suggested sequencing

1. **Phase 1 + Phase 2 together next.** Multi-provider support and
   concurrency hardening are both foundational — nearly everything after
   this depends on the provider abstraction being real (not just
   one-adapter-pretending-to-be-generic) and on tool execution already
   being safely concurrent before more tools get added in Phase 3.
2. **Phase 3** (tools) — most of an agent's day-to-day usefulness comes
   from what it can *do*, not which model answers, so tool parity is the
   next highest-leverage phase.
3. **Phases 4–6** (session/memory, MCP, ACP) can proceed in roughly any
   order relative to each other once 1–3 are stable; they don't block one
   another.
4. **Phases 7–8** (sandbox, hooks/plugins) are hardening/extensibility work
   best done once there's real tool-call volume to secure and extend.
5. **Phase 9** (TUI polish) is continuous — individual items can land
   whenever, there's no reason to gate them behind other phases.
6. **Phase 10–11** (telemetry, distribution) near the point this is ready
   for anyone outside the core team to run.

## Definition of done (applies to every task above)

A task is done only when it:

1. Builds via `mage go:build` and passes `mage go:test` / `mage go:race`.
2. Was developed TDD: the test(s) at the appropriate layer (fake-based for
   application-layer logic, `httptest`/`t.TempDir()` for adapters) were
   written first, confirmed to fail for the right reason, then made to pass
   — not written afterward to describe code that already works. Commit
   history / PR description should make the red→green step visible (e.g.
   a commit that adds only the failing test, or the PR notes the failure
   output before the fix).
3. Has no goroutine that can outlive its context — verified, not assumed.
4. Updates `ARCHITECTURE.md`'s provider/tool table if it adds a new
   provider or tool.
5. Introduces zero `.sh` files. New build steps are a `go.yaml` section
   plus a `magefile.go` target, always.
