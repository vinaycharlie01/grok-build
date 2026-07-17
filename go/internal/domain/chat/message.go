// Package chat holds the core domain types for a Grok conversation. It has
// no dependency on any adapter, transport, or third-party SDK — the
// hexagon's center.
package chat

import "time"

// Role identifies who authored a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a request, emitted by the model, to invoke a named tool with
// JSON-encoded arguments.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // raw JSON
}

// ToolResult is the outcome of executing a ToolCall, fed back to the model
// as a RoleTool message.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// Message is one turn in a Session.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall  // set on assistant messages that request tool use
	ToolResult *ToolResult // set on RoleTool messages
	CreatedAt  time.Time
}

// UserMessage builds a user-authored Message.
func UserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content, CreatedAt: time.Now()}
}

// AssistantMessage builds an assistant-authored Message.
func AssistantMessage(content string, calls []ToolCall) Message {
	return Message{Role: RoleAssistant, Content: content, ToolCalls: calls, CreatedAt: time.Now()}
}

// ToolMessage builds the tool-result reply to a ToolCall.
func ToolMessage(result ToolResult) Message {
	return Message{Role: RoleTool, ToolResult: &result, CreatedAt: time.Now()}
}
