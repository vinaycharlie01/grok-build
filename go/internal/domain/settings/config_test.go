package settings_test

import (
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
	if got != want {
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
