// Package env implements ports.CredentialStore by reading an API key from
// the process environment. The Rust xai-grok-auth crate additionally
// supports a full OAuth device-code flow with a browser handoff; that is
// intentionally out of scope for this vertical slice and left as a future
// adapter behind the same ports.CredentialStore port.
package env

import "fmt"

// DefaultVarName is the environment variable this Store reads by default.
const DefaultVarName = "XAI_API_KEY"

// Store is a ports.CredentialStore backed by an environment variable.
type Store struct {
	varName string
	lookup  func(string) (string, bool)
}

// New builds a Store reading varName from the process environment.
func New(varName string, lookup func(string) (string, bool)) *Store {
	return &Store{varName: varName, lookup: lookup}
}

// APIKey implements ports.CredentialStore.
func (s *Store) APIKey() (string, error) {
	v, ok := s.lookup(s.varName)
	if !ok || v == "" {
		return "", fmt.Errorf("env: %s is not set; export it with your xAI API key", s.varName)
	}
	return v, nil
}

// NoAuth is a ports.CredentialStore for providers configured with an empty
// APIKeyEnvVar — some local model servers accept unauthenticated requests,
// and forcing an env var lookup for them would just be friction.
type NoAuth struct{}

// APIKey implements ports.CredentialStore.
func (NoAuth) APIKey() (string, error) { return "", nil }
