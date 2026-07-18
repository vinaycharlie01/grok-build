package main

import "testing"

func TestResolveModelID(t *testing.T) {
	tests := []struct {
		name            string
		flagValue       string
		env             map[string]string
		providerDefault string
		want            string
	}{
		{
			name:            "flag wins over env var and provider default",
			flagValue:       "grok-4-fast",
			env:             map[string]string{"GROK_MODEL": "grok-build"},
			providerDefault: "grok-4",
			want:            "grok-4-fast",
		},
		{
			name:            "env var wins over provider default when no flag is set",
			flagValue:       "",
			env:             map[string]string{"GROK_MODEL": "grok-build"},
			providerDefault: "grok-4",
			want:            "grok-build",
		},
		{
			name:            "falls back to the provider's configured default when neither is set",
			flagValue:       "",
			env:             map[string]string{},
			providerDefault: "grok-4",
			want:            "grok-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }

			got := resolveModelID(tt.flagValue, getenv, tt.providerDefault)
			if got != tt.want {
				t.Fatalf("resolveModelID(%q, ..., %q) = %q, want %q", tt.flagValue, tt.providerDefault, got, tt.want)
			}
		})
	}
}
