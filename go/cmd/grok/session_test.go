package main

import (
	"context"
	"errors"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// fakeSessionStore is an in-memory ports.SessionStore fake, used to unit
// test resolveSession without a real MongoDB (that's what
// tests/integration/sessionstore_mongo_test.go is for).
type fakeSessionStore struct {
	sessions map[string]*chat.Session
	loadErr  error
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]*chat.Session{}}
}

func (f *fakeSessionStore) Save(_ context.Context, s *chat.Session) error {
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeSessionStore) Load(_ context.Context, id string) (*chat.Session, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	s, ok := f.sessions[id]
	if !ok {
		return nil, ports.ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeSessionStore) Delete(_ context.Context, id string) error {
	delete(f.sessions, id)
	return nil
}

func TestResolveSessionWithNilStoreCreatesFresh(t *testing.T) {
	got, err := resolveSession(context.Background(), nil, "local", "grok-4", "be terse")
	if err != nil {
		t.Fatalf("resolveSession() error = %v", err)
	}
	if got.ID != "local" || got.Model != "grok-4" {
		t.Fatalf("resolveSession() = %+v, want a fresh session for id/model", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "be terse" {
		t.Fatalf("resolveSession() = %+v, want it seeded with the system prompt", got)
	}
}

func TestResolveSessionLoadsPersistedSessionWhenPresent(t *testing.T) {
	store := newFakeSessionStore()
	saved := chat.NewSession("local", "grok-4", "")
	saved.Append(chat.UserMessage("earlier turn"))
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatal(err)
	}

	got, err := resolveSession(context.Background(), store, "local", "grok-4", "be terse")
	if err != nil {
		t.Fatalf("resolveSession() error = %v", err)
	}
	if got != saved {
		t.Fatalf("resolveSession() = %+v, want the persisted session %+v returned as-is (not re-seeded)", got, saved)
	}
}

func TestResolveSessionFallsBackToFreshWhenStoreHasNothingSaved(t *testing.T) {
	store := newFakeSessionStore()

	got, err := resolveSession(context.Background(), store, "local", "grok-4", "be terse")
	if err != nil {
		t.Fatalf("resolveSession() error = %v", err)
	}
	if got.ID != "local" || len(got.Messages) != 1 {
		t.Fatalf("resolveSession() = %+v, want a fresh seeded session when nothing was persisted yet", got)
	}
}

func TestResolveSessionPropagatesUnexpectedStoreErrors(t *testing.T) {
	store := newFakeSessionStore()
	store.loadErr = errors.New("connection refused")

	if _, err := resolveSession(context.Background(), store, "local", "grok-4", ""); err == nil {
		t.Fatal("resolveSession() error = nil, want the store's non-not-found error to propagate")
	}
}
