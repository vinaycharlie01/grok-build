package main

import (
	"fmt"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
)

// providerChoice is the result of resolving which LLM backend to build.
//
// This is a manual-testing stopgap ahead of Phase 1's real llm/router +
// multi-provider settings.Config (see ROADMAP.md): it picks exactly one
// provider from an env var, it doesn't route across several.
//
// All three names build a providers/openai.Client (backed by the official
// github.com/openai/openai-go SDK — no hand-rolled HTTP/SSE client exists
// anywhere in this tree). xAI's API is OpenAI-wire-compatible, so it needs
// no client of its own, just its own base URL/credential; "openaicompat"
// is the same story for OpenRouter, Groq, a local Ollama/vLLM server, or
// anything else that speaks the same wire format.
type providerChoice struct {
	name    string // "xai" | "openai" | "openaicompat"
	baseURL string
	model   string
	credVar string
}

// resolveProviderChoice reads GROK_PROVIDER (default "xai") and its
// provider-specific env vars via getenv, so it's unit-testable without
// touching the real environment.
func resolveProviderChoice(getenv func(string) string, cfg settings.Config) (providerChoice, error) {
	name := getenv("GROK_PROVIDER")
	if name == "" {
		name = "xai"
	}
	model := getenv("GROK_MODEL")

	switch name {
	case "xai":
		return providerChoice{
			name:    name,
			baseURL: cfg.BaseURL,
			model:   firstNonEmpty(model, cfg.DefaultModel),
			credVar: env.DefaultVarName,
		}, nil

	case "openai":
		return providerChoice{
			name:    name,
			baseURL: "https://api.openai.com/v1",
			model:   firstNonEmpty(model, "gpt-4o"),
			credVar: "OPENAI_API_KEY",
		}, nil

	case "openaicompat":
		baseURL := getenv("GROK_BASE_URL")
		if baseURL == "" {
			return providerChoice{}, fmt.Errorf("GROK_PROVIDER=openaicompat requires GROK_BASE_URL to be set")
		}
		if model == "" {
			return providerChoice{}, fmt.Errorf("GROK_PROVIDER=openaicompat requires GROK_MODEL to be set")
		}
		return providerChoice{
			name:    name,
			baseURL: baseURL,
			model:   model,
			credVar: "GROK_API_KEY",
		}, nil

	default:
		return providerChoice{}, fmt.Errorf("unknown GROK_PROVIDER %q (want xai, openai, or openaicompat)", name)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
