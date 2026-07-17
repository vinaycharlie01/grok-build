// Package mongo implements ports.SessionStore backed by MongoDB via the
// official go.mongodb.org/mongo-driver/v2 client — no hand-rolled wire
// protocol, the same official-SDK-only rule already applied to every LLM
// provider in this tree (see ROADMAP.md's "Library & framework choices").
package mongo

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// Store implements ports.SessionStore.
type Store struct {
	client *mongo.Client
	coll   *mongo.Collection
}

var _ ports.SessionStore = (*Store)(nil)

// New connects to uri and returns a Store backed by database.collection.
// The connection is verified with a Ping before returning, so a
// misconfigured or unreachable MongoDB deployment fails at construction
// time rather than surprising the first Save/Load call — mongo.Connect
// alone only validates the options, not reachability (see its doc
// comment).
func New(ctx context.Context, uri, database, collection string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("sessionstore/mongo: connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("sessionstore/mongo: ping %s: %w", uri, err)
	}
	return &Store{
		client: client,
		coll:   client.Database(database).Collection(collection),
	}, nil
}

// Save implements ports.SessionStore.
func (s *Store) Save(ctx context.Context, session *chat.Session) error {
	doc := toDocument(session)
	if _, err := s.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("sessionstore/mongo: save %q: %w", session.ID, err)
	}
	return nil
}

// Load implements ports.SessionStore.
func (s *Store) Load(ctx context.Context, id string) (*chat.Session, error) {
	var doc document
	err := s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("sessionstore/mongo: load %q: %w", id, ports.ErrSessionNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("sessionstore/mongo: load %q: %w", id, err)
	}
	return fromDocument(doc), nil
}

// Delete implements ports.SessionStore.
func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := s.coll.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return fmt.Errorf("sessionstore/mongo: delete %q: %w", id, err)
	}
	return nil
}

// Close disconnects the underlying client. Callers that construct a Store
// (cmd/grok's composition root) are responsible for calling this on
// shutdown.
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}
