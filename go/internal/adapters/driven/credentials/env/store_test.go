package env_test

import (
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
)

func TestAPIKeyMissingReturnsError(t *testing.T) {
	s := env.New("XAI_API_KEY", func(string) (string, bool) { return "", false })
	if _, err := s.APIKey(); err == nil {
		t.Fatal("APIKey() error = nil, want error for unset var")
	}
}

func TestAPIKeyEmptyReturnsError(t *testing.T) {
	s := env.New("XAI_API_KEY", func(string) (string, bool) { return "", true })
	if _, err := s.APIKey(); err == nil {
		t.Fatal("APIKey() error = nil, want error for empty var")
	}
}

func TestNoAuthReturnsEmptyKeyWithoutError(t *testing.T) {
	got, err := (env.NoAuth{}).APIKey()
	if err != nil {
		t.Fatalf("NoAuth.APIKey() error = %v, want nil for a provider that needs no credential", err)
	}
	if got != "" {
		t.Fatalf("NoAuth.APIKey() = %q, want empty", got)
	}
}

func TestAPIKeyPresent(t *testing.T) {
	s := env.New("XAI_API_KEY", func(name string) (string, bool) {
		if name == "XAI_API_KEY" {
			return "secret-value", true
		}
		return "", false
	})
	got, err := s.APIKey()
	if err != nil {
		t.Fatalf("APIKey() error = %v", err)
	}
	if got != "secret-value" {
		t.Fatalf("APIKey() = %q, want %q", got, "secret-value")
	}
}
