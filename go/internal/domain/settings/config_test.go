package settings_test

import (
	"reflect"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
)

func TestDefaultHasXAIActiveByDefault(t *testing.T) {
	cfg := settings.Default()

	if cfg.DefaultProvider != "xai" {
		t.Fatalf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "xai")
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("Default() has no providers configured")
	}

	got, err := cfg.Provider("")
	if err != nil {
		t.Fatalf("Provider(\"\") error = %v", err)
	}
	if got.Name != "xai" {
		t.Fatalf("Provider(\"\") = %+v, want the xai entry (empty name falls back to DefaultProvider)", got)
	}
}

func TestProviderLooksUpByName(t *testing.T) {
	cfg := settings.Default()

	got, err := cfg.Provider("anthropic")
	if err != nil {
		t.Fatalf("Provider(\"anthropic\") error = %v", err)
	}
	want := settings.ProviderConfig{
		Name:         "anthropic",
		Kind:         "anthropic",
		BaseURL:      "https://api.anthropic.com",
		Model:        "claude-sonnet-5",
		APIKeyEnvVar: "ANTHROPIC_API_KEY",
	}
	// reflect.DeepEqual, not ==: ProviderConfig holds a Models slice now
	// (see TestDefaultXAIModelCatalogMirrorsRustDesign), which makes the
	// struct non-comparable with ==.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Provider(\"anthropic\") = %+v, want %+v", got, want)
	}
}

func TestProviderUnknownNameErrors(t *testing.T) {
	cfg := settings.Default()

	if _, err := cfg.Provider("does-not-exist"); err == nil {
		t.Fatal("Provider(\"does-not-exist\") error = nil, want an error naming the unknown provider")
	}
}

func TestProviderNamesMustBeUniqueAcrossKindOpenAIProviders(t *testing.T) {
	// xai, openai are both kind "openai" but distinct configured entries -
	// selecting by name must not accidentally match the wrong one.
	cfg := settings.Default()

	xai, err := cfg.Provider("xai")
	if err != nil {
		t.Fatalf("Provider(\"xai\") error = %v", err)
	}
	openai, err := cfg.Provider("openai")
	if err != nil {
		t.Fatalf("Provider(\"openai\") error = %v", err)
	}
	if xai.BaseURL == openai.BaseURL {
		t.Fatalf("xai and openai entries have the same BaseURL %q, want distinct endpoints", xai.BaseURL)
	}
}

// TestDefaultXAIModelCatalogMirrorsRustDesign locks the xai entry's Models
// catalog to the real values in the Rust reference's
// crates/codegen/xai-grok-models/default_models.json ("grok-build" entry) -
// this is a deliberate port of that crate's design (a per-provider list of
// selectable models with metadata: context window, sampling defaults,
// which API backend it expects, whether it's publicly available), not a
// fabricated example.
func TestDefaultXAIModelCatalogMirrorsRustDesign(t *testing.T) {
	cfg := settings.Default()

	xai, err := cfg.Provider("xai")
	if err != nil {
		t.Fatalf("Provider(\"xai\") error = %v", err)
	}

	got := xai.ModelInfo("grok-build")
	want := settings.ModelInfo{
		ID:             "grok-build",
		Name:           "Grok Build",
		Description:    "Best for advanced coding tasks",
		ContextWindow:  500000,
		Temperature:    settings.Float64Ptr(0.7),
		TopP:           settings.Float64Ptr(0.95),
		APIBackend:     settings.APIBackendResponses,
		SupportedInAPI: false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ModelInfo(\"grok-build\") = %+v, want %+v", got, want)
	}
}

// TestProviderConfigModelInfoFallsBackWithoutACatalog proves a provider
// never *needs* a Models catalog to function: looking up an id that isn't
// in it (or that has no catalog at all, like anthropic/openai below)
// yields a minimal ModelInfo carrying just that id, defaulting to the
// chat_completions wire format - the common case for a hand-added
// provider entry that's just { name, kind, baseURL, model, apiKeyEnvVar }.
func TestProviderConfigModelInfoFallsBackWithoutACatalog(t *testing.T) {
	cfg := settings.Default()

	anthropic, err := cfg.Provider("anthropic")
	if err != nil {
		t.Fatalf("Provider(\"anthropic\") error = %v", err)
	}

	got := anthropic.ModelInfo("claude-sonnet-5")
	want := settings.ModelInfo{ID: "claude-sonnet-5", APIBackend: settings.APIBackendChatCompletions}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ModelInfo(\"claude-sonnet-5\") = %+v, want the synthesized fallback %+v", got, want)
	}
}
