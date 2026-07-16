// Package file implements ports.ConfigStore by reading/writing a YAML file
// on disk, mirroring the shape (if not yet the full feature set) of the
// Rust xai-grok-config crate's on-disk config.
package file

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
	"gopkg.in/yaml.v3"
)

// Store is a ports.ConfigStore backed by a single YAML file.
type Store struct {
	path string
}

// New builds a Store reading/writing the given path.
func New(path string) *Store {
	return &Store{path: path}
}

// DefaultPath returns "$GROK_HOME/config.yaml", falling back to
// "~/.grok/config.yaml" when GROK_HOME is unset.
func DefaultPath() (string, error) {
	if home := os.Getenv("GROK_HOME"); home != "" {
		return filepath.Join(home, "config.yaml"), nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("file: resolve home directory: %w", err)
	}
	return filepath.Join(dir, ".grok", "config.yaml"), nil
}

// Load implements ports.ConfigStore. A missing file yields settings.Default()
// rather than an error, so first-run doesn't require a pre-existing config.
func (s *Store) Load() (settings.Config, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return settings.Default(), nil
	}
	if err != nil {
		return settings.Config{}, fmt.Errorf("file: read %s: %w", s.path, err)
	}

	cfg := settings.Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return settings.Config{}, fmt.Errorf("file: parse %s: %w", s.path, err)
	}
	return cfg, nil
}

// Save implements ports.ConfigStore.
func (s *Store) Save(cfg settings.Config) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("file: create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("file: encode config: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("file: write %s: %w", s.path, err)
	}
	return nil
}
