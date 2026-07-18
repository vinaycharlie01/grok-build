// Package settings holds the domain representation of user-configurable
// agent settings.
package settings

import "fmt"

// Config is the agent's on-disk configuration: which LLM providers are
// available, which one to use by default, and shared behavior settings.
// Everything a provider needs — endpoint, model, credential — is data in
// Providers, not a Go type; adding a new OpenAI- or Anthropic-wire-
// compatible integration (a different vendor, a local server, a work
// proxy) is a config file edit, never a code change.
type Config struct {
	// DefaultProvider is the Providers[i].Name used when no override is
	// given (no --provider flag, no GROK_PROVIDER env var).
	DefaultProvider string `yaml:"defaultProvider"`
	// Providers lists every configured LLM backend, selectable by name.
	Providers []ProviderConfig `yaml:"providers"`
	// SystemPrompt seeds every new session.
	SystemPrompt string `yaml:"systemPrompt"`
	// SessionStore configures persisting chat sessions across process
	// restarts (Phase 4 — see ROADMAP.md). Nil (the zero value, and what
	// Default() returns) means no persistence: chatservice/the TUI behave
	// exactly as before this field existed, a fresh in-memory session
	// every run. Persistence is opt-in, never a silent default.
	SessionStore *SessionStoreConfig `yaml:"sessionStore,omitempty"`
}

// SessionStoreConfig configures the ports.SessionStore adapter cmd/grok
// wires in when session persistence is enabled.
type SessionStoreConfig struct {
	// Kind selects which adapter to construct. "mongo" (backed by the
	// official mongo-go-driver, internal/adapters/driven/sessionstore/mongo)
	// is the only kind implemented today.
	Kind string `yaml:"kind"`
	// URI is the backend's connection string (e.g.
	// "mongodb://user:pass@host:27017" for kind: mongo). Credentials
	// belong in the URI here rather than a separate env-var-keyed
	// credential, unlike ProviderConfig.APIKeyEnvVar — a session store is
	// local infrastructure the operator controls, not a third-party API
	// key. Use your deployment's own secret-injection mechanism (env var
	// expansion, mounted file) to avoid committing it to the config file.
	URI string `yaml:"uri"`
	// Database is the database name to use.
	Database string `yaml:"database"`
	// Collection is the collection (kind: mongo) sessions are stored in.
	Collection string `yaml:"collection"`
}

// ProviderConfig describes one configured LLM backend.
type ProviderConfig struct {
	// Name is the identifier used to select this provider (via --provider,
	// GROK_PROVIDER, or DefaultProvider). Must be unique within Providers.
	Name string `yaml:"name"`
	// Kind is the wire-format family this provider speaks — "openai" or
	// "anthropic" — which picks which SDK-backed client gets constructed
	// (see cmd/grok/provider.go). It names a *protocol*, not a vendor:
	// "openai" covers xAI, OpenAI itself, OpenRouter, Groq, a local
	// Ollama/vLLM server, or anything else speaking the OpenAI
	// chat-completions format.
	Kind string `yaml:"kind"`
	// BaseURL is this provider's API root.
	BaseURL string `yaml:"baseURL"`
	// Model is the default model id requested from this provider — the
	// same "just a string" field as before. Selecting it works with zero
	// extra configuration; see Models below for optional richer metadata.
	Model string `yaml:"model"`
	// APIKeyEnvVar names the environment variable holding this provider's
	// credential. The key itself is never stored in the config file.
	// Leave empty for a provider that needs no credential at all (some
	// local model servers accept unauthenticated requests).
	APIKeyEnvVar string `yaml:"apiKeyEnvVar"`
	// Models is an optional catalog of selectable models for this
	// provider, each carrying the metadata a model-aware caller wants
	// (context window for auto-compact thresholds, sampling defaults,
	// which API backend it expects, public availability). This mirrors
	// the Rust reference's xai-grok-models crate (default_models.json) —
	// see ModelInfo. Entirely optional: a provider with no Models
	// configured still works exactly as before, selecting Model by id
	// with no metadata attached (see ProviderConfig.ModelInfo).
	Models []ModelInfo `yaml:"models,omitempty"`
}

// APIBackend names the wire format a model expects requests in — the Go
// analogue of the Rust reference's xai_grok_sampling_types::ApiBackend.
// It's a property of the *model*, not just the provider: two models
// served by the same provider entry can expect different backends (e.g.
// xAI's grok-4 via chat_completions vs. grok-build via responses).
type APIBackend string

const (
	// APIBackendChatCompletions is the OpenAI Chat Completions wire format
	// (POST /v1/chat/completions). The default when unset, matching the
	// Rust enum's #[default].
	APIBackendChatCompletions APIBackend = "chat_completions"
	// APIBackendResponses is the OpenAI Responses API wire format
	// (POST /v1/responses).
	APIBackendResponses APIBackend = "responses"
	// APIBackendMessages is the Anthropic Messages API wire format
	// (POST /v1/messages).
	APIBackendMessages APIBackend = "messages"
)

// ModelInfo describes one selectable model, mirroring the fields in the
// Rust reference's crates/codegen/xai-grok-models/default_models.json
// (id/name/description/context_window/temperature/top_p/api_backend/
// supported_in_api). Temperature and TopP are pointers because "unset"
// (use the provider's/model's own default) is a real, distinct state from
// "explicitly zero".
type ModelInfo struct {
	// ID is the model identifier sent in API requests — what Model /
	// --model / GROK_MODEL name to select this entry.
	ID string `yaml:"id"`
	// Name is a short human-readable label (e.g. for a future TUI model
	// picker) — the Rust catalog's "name" field.
	Name string `yaml:"name,omitempty"`
	// Description is a one-line summary of what the model is best for.
	Description string `yaml:"description,omitempty"`
	// ContextWindow is the total context window size in tokens, used for
	// auto-compact thresholds once Phase 4's session/memory work lands
	// (see ROADMAP.md) — same purpose as SamplingConfig.context_window in
	// the Rust reference.
	ContextWindow uint64 `yaml:"contextWindow,omitempty"`
	// Temperature is this model's sampling temperature default, if it has
	// one. Nil means "don't set it" (let the provider apply its own default).
	Temperature *float64 `yaml:"temperature,omitempty"`
	// TopP is this model's nucleus-sampling default, if it has one. Nil
	// means "don't set it".
	TopP *float64 `yaml:"topP,omitempty"`
	// APIBackend is which wire format this model expects. Empty
	// (unmarshaled from a file that omits it) behaves as
	// APIBackendChatCompletions, matching the Rust enum's #[default].
	APIBackend APIBackend `yaml:"apiBackend,omitempty"`
	// SupportedInAPI mirrors the Rust catalog's flag distinguishing models
	// only usable inside the first-party client (e.g. an internal/
	// preview model) from those any API caller can request.
	SupportedInAPI bool `yaml:"supportedInAPI,omitempty"`
}

// Float64Ptr is a small helper for building ModelInfo.Temperature/TopP
// literals (Go has no float64 literal-to-pointer syntax).
func Float64Ptr(v float64) *float64 { return &v }

// ModelInfo looks up metadata for a model id in this provider's Models
// catalog. A provider is never required to configure one: if Models is
// empty, or id isn't in it, this returns a minimal ModelInfo carrying
// just the id with the chat_completions default — the common case for a
// hand-added provider entry that's just name/kind/baseURL/model/
// apiKeyEnvVar. Never errors, unlike Config.Provider, because "no
// metadata for this id" isn't a misconfiguration; it's the normal state
// for most providers today.
func (p ProviderConfig) ModelInfo(id string) ModelInfo {
	for _, m := range p.Models {
		if m.ID == id {
			return m
		}
	}
	return ModelInfo{ID: id, APIBackend: APIBackendChatCompletions}
}

// Default returns the built-in configuration used when no config file
// exists yet: xAI, OpenAI, and Anthropic pre-configured, xAI active.
//
// xai is the one entry with a Models catalog populated, to demonstrate
// the design without inventing data: "grok-build" below is ported
// verbatim (id/name/description/context_window/temperature/top_p/
// api_backend/supported_in_api) from the Rust reference's
// crates/codegen/xai-grok-models/default_models.json. openai and
// anthropic are deliberately left with no Models catalog, to prove that's
// still a fully working, zero-metadata provider entry — see
// ProviderConfig.ModelInfo's fallback behavior.
func Default() Config {
	return Config{
		DefaultProvider: "xai",
		Providers: []ProviderConfig{
			{
				Name: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-4", APIKeyEnvVar: "XAI_API_KEY",
				Models: []ModelInfo{
					{ID: "grok-4", Name: "Grok 4", Description: "General-purpose flagship model", ContextWindow: 256000, APIBackend: APIBackendChatCompletions, SupportedInAPI: true},
					{ID: "grok-build", Name: "Grok Build", Description: "Best for advanced coding tasks", ContextWindow: 500000, Temperature: Float64Ptr(0.7), TopP: Float64Ptr(0.95), APIBackend: APIBackendResponses, SupportedInAPI: false},
				},
			},
			{Name: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o", APIKeyEnvVar: "OPENAI_API_KEY"},
			{Name: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5", APIKeyEnvVar: "ANTHROPIC_API_KEY"},
		},
		SystemPrompt: "You are Grok Build, a terminal coding agent.",
	}
}

// Provider looks up a configured provider by name, falling back to
// DefaultProvider when name is empty.
func (c Config) Provider(name string) (ProviderConfig, error) {
	if name == "" {
		name = c.DefaultProvider
	}
	for _, p := range c.Providers {
		if p.Name == name {
			return p, nil
		}
	}
	return ProviderConfig{}, fmt.Errorf("settings: no provider named %q configured (add it to providers: in your config file)", name)
}
