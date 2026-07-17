package ports

import "github.com/vinaycharlie01/grok-build/go/internal/domain/settings"

// ConfigStore is the driven port for loading and persisting user settings.
type ConfigStore interface {
	Load() (settings.Config, error)
	Save(settings.Config) error
}

// CredentialStore is the driven port for retrieving the API credential used
// to authenticate against the model provider. The Rust auth crate supports
// a full OAuth device-code flow; this slice only ports the simpler API-key
// path (see internal/adapters/driven/credentials/env) and leaves OAuth as a
// follow-up adapter behind this same port.
type CredentialStore interface {
	APIKey() (string, error)
}
