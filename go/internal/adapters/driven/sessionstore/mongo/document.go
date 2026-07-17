package mongo

import (
	"time"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
)

// document is the BSON shape a chat.Session is stored as. It mirrors
// chat.Session/chat.Message field-for-field rather than putting bson
// struct tags directly on the domain types: internal/domain has "no
// dependency on any adapter, transport, or third-party SDK" (see its
// package doc) and that includes not knowing this adapter's field names
// exist.
type document struct {
	ID       string            `bson:"_id"`
	Model    string            `bson:"model"`
	Messages []messageDocument `bson:"messages"`
}

type messageDocument struct {
	Role       string              `bson:"role"`
	Content    string              `bson:"content"`
	ToolCalls  []toolCallDocument  `bson:"toolCalls,omitempty"`
	ToolResult *toolResultDocument `bson:"toolResult,omitempty"`
	CreatedAt  time.Time           `bson:"createdAt"`
}

type toolCallDocument struct {
	ID        string `bson:"id"`
	Name      string `bson:"name"`
	Arguments string `bson:"arguments"`
}

type toolResultDocument struct {
	ToolCallID string `bson:"toolCallId"`
	Content    string `bson:"content"`
	IsError    bool   `bson:"isError"`
}

func toDocument(s *chat.Session) document {
	messages := make([]messageDocument, len(s.Messages))
	for i, m := range s.Messages {
		md := messageDocument{
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		}
		if len(m.ToolCalls) > 0 {
			md.ToolCalls = make([]toolCallDocument, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				md.ToolCalls[j] = toolCallDocument{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
			}
		}
		if m.ToolResult != nil {
			md.ToolResult = &toolResultDocument{
				ToolCallID: m.ToolResult.ToolCallID,
				Content:    m.ToolResult.Content,
				IsError:    m.ToolResult.IsError,
			}
		}
		messages[i] = md
	}
	return document{ID: s.ID, Model: s.Model, Messages: messages}
}

func fromDocument(d document) *chat.Session {
	messages := make([]chat.Message, len(d.Messages))
	for i, md := range d.Messages {
		m := chat.Message{
			Role:      chat.Role(md.Role),
			Content:   md.Content,
			CreatedAt: md.CreatedAt,
		}
		if len(md.ToolCalls) > 0 {
			m.ToolCalls = make([]chat.ToolCall, len(md.ToolCalls))
			for j, tc := range md.ToolCalls {
				m.ToolCalls[j] = chat.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
			}
		}
		if md.ToolResult != nil {
			m.ToolResult = &chat.ToolResult{
				ToolCallID: md.ToolResult.ToolCallID,
				Content:    md.ToolResult.Content,
				IsError:    md.ToolResult.IsError,
			}
		}
		messages[i] = m
	}
	return &chat.Session{ID: d.ID, Model: d.Model, Messages: messages}
}
