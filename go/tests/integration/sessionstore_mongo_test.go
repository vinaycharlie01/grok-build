//go:build integration

// Package integration holds tests that need a real external process
// (today: a real mongod via testcontainers-go) and are therefore excluded
// from the default `go test ./...` / `mage go:test` run by the
// "integration" build tag above — run them explicitly with
// `go test -tags=integration ./tests/integration/...` or `mage go:integration`.
// This mirrors the test-layer split CONTRIBUTING.md/README.md already
// describe (unit -> integration -> e2e): unit tests live beside their
// package (readfile/tool_test.go, sessionstore/mongo/document_test.go,
// ...); anything that spins up Docker lives here instead.
package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	sessionmongo "github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/sessionstore/mongo"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// newMongoSessionStore starts a real MongoDB container (testcontainers-go)
// and returns a connected sessionmongo.Store, registering cleanup for
// both. Shared by every test below to keep the actual test bodies focused
// on the SessionStore contract, not container bring-up ceremony.
func newMongoSessionStore(t *testing.T) *sessionmongo.Store {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate mongodb container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString() error = %v", err)
	}

	store, err := sessionmongo.New(ctx, uri, "grok_test", "sessions")
	if err != nil {
		t.Fatalf("sessionmongo.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Logf("Close() error = %v", err)
		}
	})

	return store
}

func TestMongoSessionStoreSaveThenLoadRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := newMongoSessionStore(t)

	session := chat.NewSession("integration-s1", "grok-build", "be terse")
	session.Append(chat.UserMessage("hello"))
	session.Append(chat.AssistantMessage("hi there", nil))

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(ctx, session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ID != session.ID || loaded.Model != session.Model {
		t.Fatalf("Load() = %+v, want ID/Model to match the saved session %+v", loaded, session)
	}
	if len(loaded.Messages) != len(session.Messages) {
		t.Fatalf("Load() returned %d messages, want %d", len(loaded.Messages), len(session.Messages))
	}
	for i, want := range session.Messages {
		if loaded.Messages[i].Role != want.Role || loaded.Messages[i].Content != want.Content {
			t.Fatalf("message[%d] = %+v, want %+v", i, loaded.Messages[i], want)
		}
	}
}

func TestMongoSessionStoreSaveTwiceOverwritesRatherThanDuplicates(t *testing.T) {
	ctx := context.Background()
	store := newMongoSessionStore(t)

	session := chat.NewSession("integration-s2", "grok-build", "")
	session.Append(chat.UserMessage("first version"))
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save() [1st] error = %v", err)
	}

	session.Append(chat.UserMessage("second version"))
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save() [2nd] error = %v", err)
	}

	loaded, err := store.Load(ctx, session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("Load() returned %d messages, want 2 (the 2nd Save should overwrite, not duplicate)", len(loaded.Messages))
	}
}

func TestMongoSessionStoreLoadMissingReturnsErrSessionNotFound(t *testing.T) {
	ctx := context.Background()
	store := newMongoSessionStore(t)

	if _, err := store.Load(ctx, "does-not-exist"); !errors.Is(err, ports.ErrSessionNotFound) {
		t.Fatalf("Load() error = %v, want ports.ErrSessionNotFound", err)
	}
}

func TestMongoSessionStoreDeleteThenLoadReturnsErrSessionNotFound(t *testing.T) {
	ctx := context.Background()
	store := newMongoSessionStore(t)

	session := chat.NewSession("integration-s3", "grok-build", "")
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Delete(ctx, session.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Load(ctx, session.ID); !errors.Is(err, ports.ErrSessionNotFound) {
		t.Fatalf("Load() after Delete() error = %v, want ports.ErrSessionNotFound", err)
	}
}

func TestMongoSessionStoreDeleteMissingIsNotAnError(t *testing.T) {
	ctx := context.Background()
	store := newMongoSessionStore(t)

	if err := store.Delete(ctx, "never-existed"); err != nil {
		t.Fatalf("Delete() of a nonexistent session error = %v, want nil (ensure-absent semantics)", err)
	}
}
