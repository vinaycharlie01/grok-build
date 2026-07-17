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

## Library & framework choices

Concrete third-party library decisions per phase — and one explicit
**non-adoption** that's important enough to call out before the phase list.

### Core agent loop: staying hand-rolled, not adopting a third-party framework

Google [ADK Go](https://github.com/google/adk-go), ByteDance's
[Eino](https://github.com/cloudwego/eino), and
[`nlpodyssey/openai-agents-go`](https://github.com/nlpodyssey/openai-agents-go)
are all real, capable Go agent frameworks with genuinely useful ideas
(multi-agent handoffs, graph workflows, guardrails, built-in memory/session
machinery). This roadmap does **not** adopt any of them as the backbone of
`internal/application/chatservice` — not a judgment on their quality, but a
fit problem specific to this project:

1. **Provider-agnostic is the whole point.** Each of these frameworks has
   its own opinion about how a "tool," a "session," or a "provider" is
   shaped. Adopting one as the core would mean `chatservice`'s loop is
   governed by that framework's abstractions instead of our own
   `ports.LLMProvider`/`ports.Tool` — undermining the reason Phase 0 built a
   hexagon in the first place (swapping a framework should be an adapter
   change, not a rewrite of the thing every other phase depends on).
2. **It's already built, tested, and TDD'd.** Replacing it is a full
   rewrite of the core loop for a framework whose fit with 4+ non-native
   providers — xAI in particular, which none of these frameworks ship
   first-party support for — is unverified.
3. **Nothing is lost.** Their ideas stay valid *inspiration* for phases that
   need exactly what they're good at: ADK's multi-agent/handoff model and
   OpenAI Agents Go's guardrails are natural reference points for Phase 6
   (ACP) and Phase 8 (hooks/plugins) — read their source for the pattern,
   implement it behind our own ports, same as every other phase here.

Revisit this if a concrete phase hits a wall the hexagon doesn't solve well
— but the default is: our loop, their ideas where useful.

### LLM provider SDKs (Phase 1)

Official vendor SDKs replace hand-rolled HTTP/SSE parsing wherever one
exists and is well-maintained — less wire-format code for us to own and
test, better handling of edge cases (retries, rate-limit headers, streaming
ergonomics) than a bespoke client. Each still implements our own
`ports.LLMProvider` — the SDK is an implementation detail of one adapter,
never visible above `internal/adapters/driven/llm/providers/*`.

| Provider | Adapter package | SDK |
|---|---|---|
| xAI | *(no separate package — reuses `providers/openai.Client` pointed at `https://api.x.ai/v1`)* | [`github.com/openai/openai-go`](https://github.com/openai/openai-go) — xAI has no official Go SDK, but its API is OpenAI-wire-compatible, so it needs no hand-rolled client either. Phase 0 originally hand-rolled this (`providers/xai`, raw `net/http` + manual SSE parsing); replaced with the SDK client once `providers/openai` existed to point at instead — no raw HTTP client remains anywhere in this tree for any LLM provider. |
| OpenAI | `providers/openai` | [`github.com/openai/openai-go`](https://github.com/openai/openai-go) |
| Anthropic | `providers/anthropic` ✅ built | [`github.com/anthropics/anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go) |
| Google Gemini | `providers/gemini` | [`github.com/googleapis/go-genai`](https://github.com/googleapis/go-genai) |
| Ollama (local) | `providers/ollama` | [`github.com/ollama/ollama/api`](https://github.com/ollama/ollama/tree/main/api) — native client, not the generic OpenAI-compat path |
| Other OpenAI-compatible (OpenRouter, Groq, vLLM, Ollama, ...) | *(no separate package — reuses `providers/openai.Client` with a config-supplied `BaseURL`)* | `github.com/openai/openai-go` pointed at a configurable `BaseURL` — add a `kind: openai` entry to the config file's `providers:` list, see README.md's "Running it against a different provider" |

### CLI (Phase 1)

- [x] Wire [Cobra](https://github.com/spf13/cobra) into `cmd/grok`: bare
      `grok` and `grok run` both launch the TUI (`runInteractive` in
      `main.go`, unchanged behavior — `cli.go` just routes to it),
      `grok version` prints `Version`/`Commit`/`BuildDate` (`version.go`,
      exact names nava's `versionPkg` build option expects — Phase 11
      wires the `-ldflags` injection, these are `"dev"`/`"unknown"` until
      then). Root uses `Args: cobra.NoArgs` so an unrecognized subcommand
      errors instead of silently falling through to a TUI launch. Room
      left for a future `headless`/`mcp-server` subcommand (Phase 5).
      Purely a driving-adapter/composition-root concern — no domain or
      application changes. `cmd/grok` test coverage 40.5% → 46.4%.
- [x] Added the `--provider` persistent flag (root + `run` both read it)
      as the one flag/env var governing which config file provider entry
      is active — see "Multi-provider architecture" above. This is the
      *only* provider-related flag; everything else lives in the config
      file, not more flags.
- [ ] Evaluate [Viper](https://github.com/spf13/viper) for config loading
      (env var + flag + file precedence) as an enhancement to
      `adapters/driven/config/file`; only adopt if it doesn't compromise the
      `ports.ConfigStore` abstraction — the port stays ours either way.

### TUI (Phase 9)

Bubble Tea + Lip Gloss + Bubbles are already in use since Phase 0. Adding:
- [ ] [Glamour](https://github.com/charmbracelet/glamour) for markdown/
      code-block rendering in the transcript (the `xai-grok-markdown`
      parity item already in Phase 9's task list).

### MCP (Phase 5)

- [ ] Use [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) for the
      `ports.MCPClient` adapter instead of hand-rolling the MCP transport/
      protocol layer — same reasoning as the LLM SDKs above.

### Memory & vector search (Phase 4)

The simple file-backed adapter stays the MVP (see Phase 4's task list). For
the production-grade adapter behind the same port:
- [ ] [Qdrant Go client](https://github.com/qdrant/go-client) or
      [Weaviate Go client](https://github.com/weaviate/weaviate-go-client), or
- [ ] MongoDB Atlas Vector Search via the official
      [`mongo-go-driver`](https://github.com/mongodb/mongo-go-driver) (fits
      well if Phase 4's session store already lands on MongoDB instead of
      SQLite for a given deployment)

Pick one when Phase 4 starts, driven by whatever the session-store decision
ends up being — don't stand up a second database system if the first
choice already covers it.

### Observability (Phase 10)

- [ ] [OpenTelemetry](https://opentelemetry.io/) for traces/metrics.
      [Prometheus](https://prometheus.io/) + [Grafana](https://grafana.com/)
      + [Loki](https://grafana.com/oss/loki/) is the common self-hosted
      export target; [Langfuse](https://langfuse.com/) if LLM-call-level
      tracing/eval is specifically wanted. All of it stays behind the
      opt-in telemetry port already in Phase 10's task list — same
      opt-in-by-default-off stance as everywhere else data leaves the
      process.

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

**Built:** `internal/domain/settings.Config` has a `Providers []ProviderConfig`
list and a `DefaultProvider` name — `Config.Provider(name)` looks one up,
falling back to `DefaultProvider` when name is empty. Each `ProviderConfig`
is `{Name, Kind, BaseURL, Model, APIKeyEnvVar}`; `Kind` (`"openai"` or
`"anthropic"`) picks which SDK-backed client `cmd/grok/main.go` constructs
— it names a *wire-format family*, not a vendor, so `Kind: "openai"`
covers xAI, OpenAI itself, and any OpenAI-compatible endpoint (OpenRouter,
Groq, a local Ollama/vLLM server, ...). **Adding a new provider — even one
nobody's heard of — is a config file edit, never a Go change, as long as
it speaks one of the two wire formats already implemented.** Selection at
runtime is one flag/env var: `--provider` (falls back to `GROK_PROVIDER`,
falls back to `Config.DefaultProvider`) — see `cmd/grok/provider.go`'s
`resolveProviderName` and README.md's "Running it against a different
provider". `ports.CredentialStore` resolution is per-provider via each
entry's `APIKeyEnvVar`; an empty `APIKeyEnvVar` uses
`credentials/env.NoAuth` for providers that need none (some local model
servers accept unauthenticated requests).

**Still ahead:** a composite `internal/adapters/driven/llm/router` adapter
— itself a `ports.LLMProvider` wrapping N other `ports.LLMProvider`s with a
routing strategy (`priority`, `round-robin` first; more later) for
fallback/load-balancing across *multiple* providers in one turn. Because
it would satisfy the same port, `chatservice` needs zero changes to go
from "one provider selected by name" (today) to "a routed pool of them"
(this). `Config` would grow a `Router {Strategy string; Order []string}`
section alongside `Providers` for this. OAuth credential adapters
(matching Rust's `xai-grok-auth` device-code flow) are a later, explicit
task per provider, behind the same `ports.CredentialStore` port —
`APIKeyEnvVar` isn't the only way to satisfy it, just the only one built
so far.

## Phases & tasks

### Phase 0 — Foundation ✅ done
- [x] Hexagonal skeleton (`domain` → `application/chatservice` → `adapters/{driving,driven}`).
- [x] xAI provider (`llm/xai`), file config, env credentials.
- [x] `shell_exec` + `read_file` tools.
- [x] Bubble Tea TUI.
- [x] nava/Mage build (`magefile.go`, `go.yaml`), zero shell scripts.
- [x] Unit tests across every layer, `-race` clean.

### Phase 1 — Multi-provider support
SDK choice per provider: see "Library & framework choices" → "LLM provider SDKs" above.
- [x] Move `adapters/driven/llm/xai` → `adapters/driven/llm/providers/xai` (namespace only, no behavior change; update `cmd/grok/main.go` import).
- [x] `providers/openai`: wraps `github.com/openai/openai-go`'s streaming chat completions + tool calling.
- [x] Retargeted xAI onto `providers/openai.Client` (base URL `https://api.x.ai/v1`) and deleted `providers/xai`'s hand-rolled `net/http`/SSE client — no raw HTTP client remains for any LLM provider. `cmd/grok/main.go`'s provider switch collapsed to a single constructor call as a result.
- [x] `providers/anthropic`: wraps `github.com/anthropics/anthropic-sdk-go`'s Messages API streaming. Real translation logic, not a copy-paste of the OpenAI-format adapter — Anthropic's system prompt is a top-level request field (not a message), there's no "tool" role (tool results are `tool_result` content blocks inside a user-role message), and an assistant turn's text + tool calls are both content blocks on one message. Auth is `X-Api-Key`, not `Authorization: Bearer`. Wired into `cmd/grok` as a `kind: anthropic` config entry (see below). 82.9% coverage, same bar as the other providers.
- [ ] `providers/gemini`: wraps `github.com/googleapis/go-genai`'s streaming `GenerateContent`.
- [ ] `providers/ollama`: wraps `github.com/ollama/ollama/api` for local models (native client — as opposed to reaching Ollama's OpenAI-compat mode through `Kind: "openai"`, already possible today via a config entry, see "Multi-provider architecture" above).
- [x] `openaicompat` (any OpenAI-wire-compatible endpoint — OpenRouter, Groq, vLLM, Ollama, ...): no separate package needed — `providers/openai.Client` already takes an arbitrary `BaseURL`. Purely a config file entry (`kind: openai`, your `baseURL`/`model`/`apiKeyEnvVar`) — see README.md's "Running it against a different provider" for worked examples. Superseded the earlier `GROK_PROVIDER=openaicompat` + `GROK_BASE_URL`/`GROK_MODEL`/`GROK_API_KEY` env-var scheme, which is gone.
- [ ] `llm/router`: composite `ports.LLMProvider` with `priority` and `round-robin` strategies to start — routes across *multiple* configured providers in one turn (fallback/load-balancing), distinct from today's "select one provider by name" (`Config.Provider`).
- [x] `settings.Config` redesigned: `Providers []ProviderConfig` (`Name`/`Kind`/`BaseURL`/`Model`/`APIKeyEnvVar`) + `DefaultProvider` + `Config.Provider(name)` lookup, replacing the old single-provider `DefaultModel`/`BaseURL` fields. 100% test coverage.
- [x] `ports.CredentialStore` resolution is per-provider via each entry's `APIKeyEnvVar`; `credentials/env.NoAuth` added for providers needing none. OAuth device-code adapters (matching Rust's `xai-grok-auth`) are still a later, explicit task per provider, behind the same port.
- [x] `cmd/grok` rebuilt around the new config: `provider.go`'s `resolveProviderName` is the entire selection surface (`--provider` flag → `GROK_PROVIDER` env var → `Config.DefaultProvider`), `main.go`'s `runInteractive` does `cfg.Provider(name)` then switches on `Kind` to build `openai.New`/`anthropic.New`. This is also where the Cobra CLI task (see "Library & framework choices" → "CLI") landed.
- [x] `settings.ModelInfo`/`APIBackend` + `ProviderConfig.Models`/`ProviderConfig.ModelInfo(id)`: ports the Rust reference's `xai-grok-models` crate design (`default_models.json`'s id/name/description/context_window/temperature/top_p/api_backend/supported_in_api shape) as an optional per-provider model catalog — `settings.Default()`'s `xai` entry carries the real `grok-build` entry ported verbatim from that JSON file. `cmd/grok/model.go`'s `resolveModelID` mirrors `resolveProviderName`'s exact precedence shape (`--model` flag → `GROK_MODEL` env var → the resolved provider's `Model`). Metadata only for now — nothing consumes `ContextWindow` for auto-compact yet, that's Phase 4.
- [x] Tests: `httptest`/SDK-provided test transports per provider's wire format (mirror `llm/xai/client_test.go`'s small `fmt.Sprintf(%q, ...)` fixture-builder pattern — hand-typed SSE JSON is a proven bug source, don't repeat it); `settings.Config.Provider` tests, `resolveProviderName` tests, `config/file` round-trip test updated to the new struct shape (and switched from `!=` to `reflect.DeepEqual` since `Config` now contains a slice and isn't `comparable` anymore); `resolveModelID` table-driven tests (`tests := []struct{...}`), `ProviderConfig.ModelInfo` found/fallback cases, a dedicated `config/file` round-trip test for the `Models` catalog (pointer fields + a named string type are exactly what silently breaks YAML round-trips). Router tests (success, fallback-on-error, all-providers-failed) are still ahead, once `llm/router` exists. TDD per the Definition of Done below throughout: test first, watch it fail, then implement.
- [x] Docs: `ARCHITECTURE.md`'s provider table and `README.md`'s "Running it against a different provider" section rewritten to match; this ROADMAP's Phase 1 checklist gets checked off item by item as PRs land.

### Phase 2 — Concurrency & performance hardening
- [ ] `internal/adapters/driven/llm/resilience`: circuit breaker wrapping any `ports.LLMProvider` (CLOSED/OPEN/HALF_OPEN, config-driven threshold + reset timeout, lazy recovery on read — no background timer goroutine needed, matching the lazy-recovery pattern OmniRoute uses).
- [x] Concurrent tool-call execution in `chatservice.run`: replaced the sequential `for _, call := range calls` loop with `errgroup.WithContext` bounded by `WithMaxConcurrentTools` (default 4), preserving deterministic ordering of tool-result messages (each goroutine writes its own pre-sized slice index, so results land in call order regardless of completion order). Pulled forward from Phase 2 ahead of the rest of that phase — tests cover the parallelism speedup, order preservation despite completion order, and the concurrency cap, all `-race` clean.
- [ ] `internal/application/sessionmanager`: registry of concurrent sessions (`sync.Map` or mutex-guarded map), needed before any multi-session/headless mode.
- [ ] Streaming backpressure policy documented and enforced on every SSE consumer (bounded channel, explicit slow-consumer behavior).
- [ ] Add `go.uber.org/goleak` (or equivalent) to every adapter test package's `TestMain`, catching goroutine leaks automatically instead of relying on manual review.
- [ ] Benchmarks: `BenchmarkChatServiceConcurrentToolCalls`, `BenchmarkSSEParse`; wire `mage go:bench` (new `bench` section in `go.yaml`, new `Go.Bench` target in `magefile.go`, same pattern as existing targets).
- [ ] pprof HTTP endpoint behind a `debug` build tag or `GROK_DEBUG=1` env check (loopback-only, never exposed by default).

### Phase 3 — Tool ecosystem parity
Rust reference: `xai-grok-tools`, `xai-grok-tools-api` (`grok-tools.proto`).
- [x] `tools/writefile`: create/overwrite a file within the workspace root (same path-escape guard as `readfile`, plus an explicit absolute-path rejection — `filepath.Join(root, "/etc/passwd")` cleans to a safe in-root path rather than actually escaping, but silently re-rooting an absolute path is a confusing outcome worth a clear error instead).
- [ ] `tools/editfile`: hunk-based patch application (Rust reference: `xai-hunk-tracker`).
- [x] `tools/search`: content + glob search — pure-Go grep (`regexp` + `filepath.WalkDir`, `bufio.Scanner` per file), no external binary. The ripgrep-via-`shellexec` alternative noted here is unneeded now that this exists; revisit only if performance on very large trees demands it.
- [ ] `tools/git`: status/diff/commit — a **runtime agent tool** (like `shellexec`), not project build tooling; still zero `.sh` files.
- [ ] `tools.Registry`: replace the hardcoded `[]ports.Tool{...}` slice in `main.go` with a registry adapters register themselves into, so adding a tool never touches the composition root's tool list by hand.
- [ ] JSON Schema validation of tool arguments before `Execute` is called, so malformed model-generated args produce a clear tool-result error instead of an adapter-specific panic/failure.
- [x] Tests for every new tool (`t.TempDir()`-based, following `readfile`/`shellexec`'s existing pattern) — `writefile` and `search` both TDD'd (test written and confirmed failing to compile before the implementation existed).

### Phase 4 — Session, memory & checkpoints
Rust reference: `xai-chat-state`, `xai-prompt-queue`, `xai-grok-memory`, `xai-fast-worktree`, `xai-hunk-tracker`.
- [x] `ports.SessionStore` + a MongoDB adapter (`internal/adapters/driven/sessionstore/mongo`), backed by the official [`mongo-go-driver/v2`](https://github.com/mongodb/mongo-go-driver) — no hand-rolled wire protocol, consistent with the official-SDK-only principle used for the LLM providers. Opt-in via `settings.Config.SessionStore` (`sessionStore:` in the config file, `kind: mongo` today); nil/absent means no persistence, the same in-memory-only behavior as before this existed — see README.md's "Session persistence" section. A JSON-file adapter (the originally planned "start simple" step) was skipped in favor of going straight to the real adapter, since MongoDB was the explicit target.
  - **Caveat, read before assuming this is battle-tested**: `Save`/`Load`/`Delete` against a real `mongod` are covered by `tests/integration/sessionstore_mongo_test.go` (testcontainers-go, real container, 5 cases: round-trip, overwrite-not-duplicate, not-found, delete-then-not-found, delete-of-missing-is-a-no-op) — but that suite could not be *executed* in the sandbox that wrote it, because Docker Hub registry pulls were blocked by the environment's egress policy (`production.cloudfront.docker.com` → 403; confirmed via the pull erroring `Error response from daemon: No such image: mongo:7`, not a code-path bug). The BSON document mapping (`toDocument`/`fromDocument`, plus a real `bson.Marshal`/`Unmarshal` round-trip — the part that doesn't need a live server) is unit-tested and passes. **Run `mage go:integration` (needs a reachable Docker daemon with registry access) before trusting the live-server path in a real deployment.**
- [ ] Prompt queue for multi-turn interleaving (Rust reference: `xai-prompt-queue`).
- [ ] Long-term memory port + simple file-backed adapter (MVP); vector search adapter (Qdrant/Weaviate/MongoDB Atlas Vector Search — see "Library & framework choices" → "Memory & vector search") is an explicit stretch goal, not required for Phase 4 to ship.
- [ ] Checkpoint/worktree snapshotting before risky tool calls, git-worktree-based (Rust reference: `xai-fast-worktree`).

### Phase 5 — MCP (Model Context Protocol)
Rust reference: `xai-grok-mcp`.
- [ ] `ports.MCPClient`: connect to external MCP servers (stdio/SSE/HTTP transports) via `mark3labs/mcp-go` (see "Library & framework choices" → "MCP"); expose their tools dynamically as `ports.Tool` implementations through the Phase 3 registry.
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
- [ ] Markdown/code-block rendering with syntax highlighting via [Glamour](https://github.com/charmbracelet/glamour) (first cut of `xai-grok-markdown` parity — see "Library & framework choices" → "TUI").
- [ ] Diff rendering for file edits (pairs with Phase 3's `editfile`).

### Phase 10 — Observability & telemetry
Rust reference: `xai-grok-telemetry`, `xai-mixpanel`.
- [ ] `log/slog` structured logging end-to-end through the agent (not just nava's build-time runners).
- [ ] Opt-in telemetry port + local-file sink adapter by default; OpenTelemetry-backed remote sink adapter(s) (Prometheus/Grafana/Loki, or Langfuse for LLM-call-level tracing — see "Library & framework choices" → "Observability") stay behind explicit operator opt-in — data-collection features default OFF, matching the opt-in-only stance this kind of feature needs.

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
