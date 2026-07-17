package ports

import (
	"context"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
)

// StreamEventType discriminates the payload carried by a StreamEvent.
type StreamEventType int

const (
	// EventTextDelta carries an incremental chunk of assistant text.
	EventTextDelta StreamEventType = iota
	// EventToolCall carries one fully-assembled tool call requested by the model.
	EventToolCall
	// EventDone signals the turn finished; no more events follow.
	EventDone
	// EventError signals the stream ended abnormally.
	EventError
)

// StreamEvent is one unit emitted while an LLMProvider generates a turn.
type StreamEvent struct {
	Type     StreamEventType
	Text     string // set on EventTextDelta
	ToolCall chat.ToolCall
	Err      error // set on EventError
}

// LLMProvider is the driven port through which the application layer talks
// to a model backend (xAI's Grok API, or any future OpenAI-compatible
// provider). Adapters implement this; the application layer never imports
// an HTTP client directly.
type LLMProvider interface {
	// StreamChat sends the session's message history to the model and
	// streams back the response. The returned channel is closed after an
	// EventDone or EventError is delivered.
	StreamChat(ctx context.Context, session *chat.Session, tools []ToolSpec) (<-chan StreamEvent, error)
}

// ToolSpec is the model-facing description of a tool: enough for the
// provider adapter to advertise it in the completion request. It is
// derived from a Tool by the application layer, keeping ports/Tool itself
// free of wire-format concerns.
type ToolSpec struct {
	Name        string
	Description string
	JSONSchema  string // JSON Schema object, as raw text
}
