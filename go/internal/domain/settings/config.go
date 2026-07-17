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
	// Model is the default model requested from this provider.
	Model string `yaml:"model"`
	// APIKeyEnvVar names the environment variable holding this provider's
	// credential. The key itself is never stored in the config file.
	// Leave empty for a provider that needs no credential at all (some
	// local model servers accept unauthenticated requests).
	APIKeyEnvVar string `yaml:"apiKeyEnvVar"`
}

// Default returns the built-in configuration used when no config file
// exists yet: xAI, OpenAI, and Anthropic pre-configured, xAI active.
func Default() Config {
	return Config{
		DefaultProvider: "xai",
		Providers: []ProviderConfig{
			{Name: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-4", APIKeyEnvVar: "XAI_API_KEY"},
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
