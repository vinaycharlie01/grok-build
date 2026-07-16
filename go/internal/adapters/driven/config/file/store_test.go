package file_test

import (
	"path/filepath"
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
	if cfg != settings.Default() {
		t.Fatalf("Load() = %+v, want default %+v", cfg, settings.Default())
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s := file.New(filepath.Join(t.TempDir(), "nested", "config.yaml"))

	want := settings.Config{
		DefaultModel: "grok-test",
		BaseURL:      "https://example.test/v1",
		SystemPrompt: "be terse",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}
