package ports

import (
	"context"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -o portsfakes/fake_session_store.go --fake-name FakeSessionStore . SessionStore

// SessionStore is the driven port for persisting and retrieving chat
// sessions across process restarts — the Go analogue of the Rust
// reference's xai-chat-state persistence surface (see ROADMAP.md's Phase
// 4). The in-memory-only behavior every existing test and the TUI use
// today is unaffected by this port existing: nothing is required to
// implement it, and cmd/grok only wires an adapter in when a session
// store is configured (see the config/mongo adapter and its opt-in
// sessionStore: config section).
type SessionStore interface {
	// Save persists session, overwriting any previously saved session
	// with the same ID.
	Save(ctx context.Context, session *chat.Session) error
	// Load retrieves the session with the given ID. ErrSessionNotFound
	// is returned (wrapped or bare — callers should use errors.Is) when
	// no session with that ID has been saved.
	Load(ctx context.Context, id string) (*chat.Session, error)
	// Delete removes the session with the given ID. Deleting an ID that
	// doesn't exist is not an error, matching the usual "ensure absent"
	// semantics of a delete operation.
	Delete(ctx context.Context, id string) error
}

// ErrSessionNotFound is returned by SessionStore.Load when no session
// with the requested ID has been saved.
var ErrSessionNotFound = sessionNotFoundError{}

type sessionNotFoundError struct{}

func (sessionNotFoundError) Error() string { return "ports: session not found" }
