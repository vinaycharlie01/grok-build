package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// resolveSession loads a persisted session by id from store, falling back
// to a freshly seeded one when store is nil (no sessionStore: configured —
// today's default, unaffected by any of this) or has nothing saved yet
// for id. A non-not-found error from the store (e.g. the connection
// dropped) propagates rather than silently discarding whatever history
// was there.
func resolveSession(ctx context.Context, store ports.SessionStore, id, model, systemPrompt string) (*chat.Session, error) {
	if store != nil {
		loaded, err := store.Load(ctx, id)
		switch {
		case err == nil:
			return loaded, nil
		case errors.Is(err, ports.ErrSessionNotFound):
			// Nothing persisted yet for this id — fall through to a fresh session.
		default:
			return nil, fmt.Errorf("load session %q: %w", id, err)
		}
	}
	return chat.NewSession(id, model, systemPrompt), nil
}
