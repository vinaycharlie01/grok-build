package file_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/config/file"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
)

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	s := file.New(filepath.Join(t.TempDir(), "config.yaml"))

	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg, settings.Default()) {
		t.Fatalf("Load() = %+v, want default %+v", cfg, settings.Default())
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s := file.New(filepath.Join(t.TempDir(), "nested", "config.yaml"))

	want := settings.Config{
		DefaultProvider: "home-ollama",
		Providers: []settings.ProviderConfig{
			{Name: "home-ollama", Kind: "openai", BaseURL: "http://localhost:11434/v1", Model: "llama3", APIKeyEnvVar: ""},
			{Name: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5", APIKeyEnvVar: "ANTHROPIC_API_KEY"},
		},
		SystemPrompt: "be terse",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestSaveThenLoadRoundTripsSessionStore proves the opt-in sessionStore:
// section round-trips through YAML, and that a config file with no such
// section at all still loads with SessionStore nil (see
// TestLoadMissingFileReturnsDefault / TestDefaultHasNoSessionStoreConfigured
// in settings/config_test.go — persistence stays fully opt-in).
func TestSaveThenLoadRoundTripsSessionStore(t *testing.T) {
	s := file.New(filepath.Join(t.TempDir(), "config.yaml"))

	want := settings.Config{
		DefaultProvider: "xai",
		Providers: []settings.ProviderConfig{
			{Name: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-4", APIKeyEnvVar: "XAI_API_KEY"},
		},
		SystemPrompt: "be terse",
		SessionStore: &settings.SessionStoreConfig{
			Kind:       "mongo",
			URI:        "mongodb://localhost:27017",
			Database:   "grok",
			Collection: "sessions",
		},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestSaveThenLoadRoundTripsModelsCatalog is a dedicated case for the
// per-provider Models field: it wasn't exercised by
// TestSaveThenLoadRoundTrips above, and pointer fields (Temperature/TopP)
// plus a named string type (APIBackend) are exactly the kind of thing
// that silently round-trips wrong through a YAML marshal/unmarshal pair.
func TestSaveThenLoadRoundTripsModelsCatalog(t *testing.T) {
	s := file.New(filepath.Join(t.TempDir(), "config.yaml"))

	want := settings.Config{
		DefaultProvider: "xai",
		Providers: []settings.ProviderConfig{
			{
				Name: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-4", APIKeyEnvVar: "XAI_API_KEY",
				Models: []settings.ModelInfo{
					{
						ID: "grok-build", Name: "Grok Build", Description: "Best for advanced coding tasks",
						ContextWindow: 500000, Temperature: settings.Float64Ptr(0.7), TopP: settings.Float64Ptr(0.95),
						APIBackend: settings.APIBackendResponses, SupportedInAPI: false,
					},
				},
			},
		},
		SystemPrompt: "be terse",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
