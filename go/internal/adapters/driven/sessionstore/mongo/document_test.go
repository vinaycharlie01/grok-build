package mongo

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
)

// These tests exercise toDocument/fromDocument and their BSON round-trip
// without a live MongoDB connection — pure mapping logic plus the actual
// bson.Marshal/Unmarshal the official driver uses internally, which is
// exactly where a hand-rolled struct tag mistake would silently corrupt
// data. The Save/Load/Delete methods that actually talk to a server are
// covered separately by tests/integration/sessionstore_mongo_test.go
// (testcontainers-go, a real mongod).
func TestToDocumentThenFromDocumentRoundTrips(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	session := &chat.Session{
		ID:    "s1",
		Model: "grok-build",
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "be terse", CreatedAt: createdAt},
			{Role: chat.RoleUser, Content: "hi", CreatedAt: createdAt},
			{
				Role:      chat.RoleAssistant,
				Content:   "",
				ToolCalls: []chat.ToolCall{{ID: "1", Name: "search", Arguments: `{"q":"go"}`}},
				CreatedAt: createdAt,
			},
			{
				Role:       chat.RoleTool,
				ToolResult: &chat.ToolResult{ToolCallID: "1", Content: "result text", IsError: false},
				CreatedAt:  createdAt,
			},
		},
	}

	doc := toDocument(session)
	got := fromDocument(doc)

	if got.ID != session.ID || got.Model != session.Model {
		t.Fatalf("fromDocument(toDocument(session)) = %+v, want ID/Model to match %+v", got, session)
	}
	if len(got.Messages) != len(session.Messages) {
		t.Fatalf("got %d messages, want %d", len(got.Messages), len(session.Messages))
	}
	for i, want := range session.Messages {
		g := got.Messages[i]
		if g.Role != want.Role || g.Content != want.Content || !g.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("message[%d] = %+v, want %+v", i, g, want)
		}
	}
	if len(got.Messages[2].ToolCalls) != 1 || got.Messages[2].ToolCalls[0] != session.Messages[2].ToolCalls[0] {
		t.Fatalf("message[2].ToolCalls = %+v, want %+v", got.Messages[2].ToolCalls, session.Messages[2].ToolCalls)
	}
	if got.Messages[3].ToolResult == nil || *got.Messages[3].ToolResult != *session.Messages[3].ToolResult {
		t.Fatalf("message[3].ToolResult = %+v, want %+v", got.Messages[3].ToolResult, session.Messages[3].ToolResult)
	}
}

// TestDocumentSurvivesRealBSONMarshalRoundTrip proves the bson struct
// tags themselves are correct by going through the actual
// bson.Marshal/Unmarshal the driver calls internally on every Save/Load -
// not just the toDocument/fromDocument Go-level mapping above. A wrong or
// missing `bson:"..."` tag (e.g. an unexported field, a typo'd tag name)
// would pass the test above (pure Go struct copy) but silently drop data
// through the real driver.
func TestDocumentSurvivesRealBSONMarshalRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	original := document{
		ID:    "s1",
		Model: "grok-build",
		Messages: []messageDocument{
			{Role: "user", Content: "hi", CreatedAt: createdAt},
			{
				Role:      "assistant",
				ToolCalls: []toolCallDocument{{ID: "1", Name: "search", Arguments: `{"q":"go"}`}},
				CreatedAt: createdAt,
			},
			{
				Role:       "tool",
				ToolResult: &toolResultDocument{ToolCallID: "1", Content: "result", IsError: true},
				CreatedAt:  createdAt,
			},
		},
	}

	raw, err := bson.Marshal(original)
	if err != nil {
		t.Fatalf("bson.Marshal() error = %v", err)
	}

	var decoded document
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("bson.Unmarshal() error = %v", err)
	}

	if decoded.ID != original.ID || decoded.Model != original.Model {
		t.Fatalf("decoded = %+v, want ID/Model to match %+v", decoded, original)
	}
	if len(decoded.Messages) != 3 {
		t.Fatalf("decoded %d messages, want 3", len(decoded.Messages))
	}
	if decoded.Messages[1].ToolCalls[0] != original.Messages[1].ToolCalls[0] {
		t.Fatalf("decoded ToolCalls = %+v, want %+v", decoded.Messages[1].ToolCalls, original.Messages[1].ToolCalls)
	}
	if decoded.Messages[2].ToolResult == nil || *decoded.Messages[2].ToolResult != *original.Messages[2].ToolResult {
		t.Fatalf("decoded ToolResult = %+v, want %+v", decoded.Messages[2].ToolResult, original.Messages[2].ToolResult)
	}
}
