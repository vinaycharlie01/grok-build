package main

import (
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/anthropic"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/openai"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// TestBuildLLMClient is table-driven (see the earlier resolveModelID
// precedent in model_test.go): buildLLMClient is pure composition — no
// network call happens until StreamChat is actually invoked (openai.New/
// anthropic.New just build an SDK client value) — so it's fully
// unit-testable by asserting the concrete type returned, no server needed.
func TestBuildLLMClient(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		wantFn func(ports.LLMProvider) bool
	}{
		{
			name: "openai kind builds an openai.Client",
			kind: "openai",
			wantFn: func(c ports.LLMProvider) bool {
				_, ok := c.(*openai.Client)
				return ok
			},
		},
		{
			name: "anthropic kind builds an anthropic.Client",
			kind: "anthropic",
			wantFn: func(c ports.LLMProvider) bool {
				_, ok := c.(*anthropic.Client)
				return ok
			},
		},
		{
			name: "unknown kind defaults to openai.Client, same as an empty/unset kind",
			kind: "something-nobody-heard-of",
			wantFn: func(c ports.LLMProvider) bool {
				_, ok := c.(*openai.Client)
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLLMClient(tt.kind, "https://example.invalid", env.NoAuth{})
			if !tt.wantFn(got) {
				t.Fatalf("buildLLMClient(%q, ...) = %T, unexpected concrete type", tt.kind, got)
			}
		})
	}
}

func TestResolveProviderNamePrefersFlagOverEnvVar(t *testing.T) {
	getenv := func(string) string { return "openai" }

	got := resolveProviderName("anthropic", getenv)
	if got != "anthropic" {
		t.Fatalf("resolveProviderName() = %q, want the flag value %q to win over GROK_PROVIDER", got, "anthropic")
	}
}

func TestResolveProviderNameFallsBackToEnvVar(t *testing.T) {
	getenv := func(k string) string {
		if k == "GROK_PROVIDER" {
			return "anthropic"
		}
		return ""
	}

	got := resolveProviderName("", getenv)
	if got != "anthropic" {
		t.Fatalf("resolveProviderName() = %q, want GROK_PROVIDER value %q when no flag is set", got, "anthropic")
	}
}

func TestResolveProviderNameEmptyWhenNeitherSet(t *testing.T) {
	got := resolveProviderName("", func(string) string { return "" })
	if got != "" {
		t.Fatalf("resolveProviderName() = %q, want empty so Config.Provider falls back to DefaultProvider", got)
	}
}
