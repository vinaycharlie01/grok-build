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
