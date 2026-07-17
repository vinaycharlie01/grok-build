package main

import (
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
)

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolveProviderChoiceDefaultsToXAI(t *testing.T) {
	cfg := settings.Config{BaseURL: "https://api.x.ai/v1", DefaultModel: "grok-4"}

	choice, err := resolveProviderChoice(fakeGetenv(nil), cfg)
	if err != nil {
		t.Fatalf("resolveProviderChoice() error = %v", err)
	}

	want := providerChoice{name: "xai", baseURL: cfg.BaseURL, model: cfg.DefaultModel, credVar: env.DefaultVarName}
	if choice != want {
		t.Fatalf("resolveProviderChoice() = %+v, want %+v", choice, want)
	}
}

func TestResolveProviderChoiceXAIModelOverride(t *testing.T) {
	cfg := settings.Config{BaseURL: "https://api.x.ai/v1", DefaultModel: "grok-4"}

	choice, err := resolveProviderChoice(fakeGetenv(map[string]string{"GROK_MODEL": "grok-4-fast"}), cfg)
	if err != nil {
		t.Fatalf("resolveProviderChoice() error = %v", err)
	}
	if choice.model != "grok-4-fast" {
		t.Fatalf("model = %q, want GROK_MODEL override %q", choice.model, "grok-4-fast")
	}
}

func TestResolveProviderChoiceOpenAI(t *testing.T) {
	choice, err := resolveProviderChoice(fakeGetenv(map[string]string{"GROK_PROVIDER": "openai"}), settings.Config{})
	if err != nil {
		t.Fatalf("resolveProviderChoice() error = %v", err)
	}
	want := providerChoice{name: "openai", baseURL: "https://api.openai.com/v1", model: "gpt-4o", credVar: "OPENAI_API_KEY"}
	if choice != want {
		t.Fatalf("resolveProviderChoice() = %+v, want %+v", choice, want)
	}
}

func TestResolveProviderChoiceOpenAIModelOverride(t *testing.T) {
	choice, err := resolveProviderChoice(fakeGetenv(map[string]string{
		"GROK_PROVIDER": "openai",
		"GROK_MODEL":    "gpt-4o-mini",
	}), settings.Config{})
	if err != nil {
		t.Fatalf("resolveProviderChoice() error = %v", err)
	}
	if choice.model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", choice.model, "gpt-4o-mini")
	}
}

func TestResolveProviderChoiceOpenAICompatRequiresBaseURLAndModel(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"missing both", map[string]string{"GROK_PROVIDER": "openaicompat"}},
		{"missing model", map[string]string{"GROK_PROVIDER": "openaicompat", "GROK_BASE_URL": "http://localhost:11434/v1"}},
		{"missing base URL", map[string]string{"GROK_PROVIDER": "openaicompat", "GROK_MODEL": "llama3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveProviderChoice(fakeGetenv(tt.env), settings.Config{}); err == nil {
				t.Fatal("resolveProviderChoice() error = nil, want error")
			}
		})
	}
}

func TestResolveProviderChoiceOpenAICompat(t *testing.T) {
	choice, err := resolveProviderChoice(fakeGetenv(map[string]string{
		"GROK_PROVIDER": "openaicompat",
		"GROK_BASE_URL": "http://localhost:11434/v1",
		"GROK_MODEL":    "llama3",
	}), settings.Config{})
	if err != nil {
		t.Fatalf("resolveProviderChoice() error = %v", err)
	}
	want := providerChoice{name: "openaicompat", baseURL: "http://localhost:11434/v1", model: "llama3", credVar: "GROK_API_KEY"}
	if choice != want {
		t.Fatalf("resolveProviderChoice() = %+v, want %+v", choice, want)
	}
}

func TestResolveProviderChoiceUnknownProvider(t *testing.T) {
	if _, err := resolveProviderChoice(fakeGetenv(map[string]string{"GROK_PROVIDER": "bogus"}), settings.Config{}); err == nil {
		t.Fatal("resolveProviderChoice() error = nil, want error for unknown provider")
	}
}
