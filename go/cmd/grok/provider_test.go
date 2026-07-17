package main

import "testing"

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
