// Package settings holds the domain representation of user-configurable
// agent settings. It intentionally stays tiny for this first vertical
// slice — it grows as more of the Rust xai-grok-config surface is ported.
package settings

// Config is the subset of grok's configuration needed to run a chat turn.
type Config struct {
	// DefaultModel is used when a session does not pin its own model.
	DefaultModel string `yaml:"defaultModel"`
	// BaseURL is the xAI-compatible chat completions API root.
	BaseURL string `yaml:"baseURL"`
	// SystemPrompt seeds every new session.
	SystemPrompt string `yaml:"systemPrompt"`
}

// Default returns the built-in configuration used when no config file
// exists yet.
func Default() Config {
	return Config{
		DefaultModel: "grok-4",
		BaseURL:      "https://api.x.ai/v1",
		SystemPrompt: "You are Grok Build, a terminal coding agent.",
	}
}
