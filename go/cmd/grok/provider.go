package main

import (
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/anthropic"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/openai"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// resolveProviderName is the only provider-selection logic in this
// binary: it picks the --provider flag value if set, else GROK_PROVIDER,
// else empty (meaning "use settings.Config.DefaultProvider"). Everything
// else about a provider — endpoint, model, credential env var — lives in
// the config file's providers: list (see internal/domain/settings), not
// in flags or env vars. Adding a new provider is a config file edit.
func resolveProviderName(flagValue string, getenv func(string) string) string {
	if flagValue != "" {
		return flagValue
	}
	return getenv("GROK_PROVIDER")
}

// buildLLMClient constructs the ports.LLMProvider for a resolved
// provider's Kind. Kind names a wire-format family, not a vendor:
// "openai" covers xAI, OpenAI itself, and any OpenAI-compatible endpoint
// (OpenRouter, Groq, a local Ollama/vLLM server, ...) via the one
// SDK-backed openai.Client — see ROADMAP.md's "Library & framework
// choices" for why there's no hand-rolled HTTP client for any provider in
// this tree. "anthropic" is a genuinely different wire format, so it gets
// its own SDK-backed client rather than being forced through the OpenAI
// one. Pure composition — openai.New/anthropic.New just build an SDK
// client value, no network call happens until StreamChat is actually
// invoked — so this is unit-testable (see provider_test.go) without a
// live server.
func buildLLMClient(kind, baseURL string, creds ports.CredentialStore) ports.LLMProvider {
	switch kind {
	case "anthropic":
		return anthropic.New(baseURL, creds)
	default:
		return openai.New(baseURL, creds)
	}
}
