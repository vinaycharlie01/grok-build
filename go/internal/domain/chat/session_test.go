package chat_test

import (
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
)

func TestNewSessionSeedsSystemPrompt(t *testing.T) {
	s := chat.NewSession("s1", "grok-4", "be helpful")
	if len(s.Messages) != 1 {
		t.Fatalf("want 1 seeded message, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != chat.RoleSystem || s.Messages[0].Content != "be helpful" {
		t.Fatalf("unexpected seeded message: %+v", s.Messages[0])
	}
}

func TestNewSessionWithoutSystemPromptStartsEmpty(t *testing.T) {
	s := chat.NewSession("s1", "grok-4", "")
	if len(s.Messages) != 0 {
		t.Fatalf("want 0 messages, got %d", len(s.Messages))
	}
}

func TestAppendAndPendingToolCalls(t *testing.T) {
	s := chat.NewSession("s1", "grok-4", "")
	s.Append(chat.UserMessage("hi"))

	if calls := s.PendingToolCalls(); calls != nil {
		t.Fatalf("want no pending tool calls after a user message, got %v", calls)
	}

	want := []chat.ToolCall{{ID: "1", Name: "search", Arguments: `{"q":"go"}`}}
	s.Append(chat.AssistantMessage("", want))

	got := s.PendingToolCalls()
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("PendingToolCalls() = %+v, want %+v", got, want)
	}

	s.Append(chat.ToolMessage(chat.ToolResult{ToolCallID: "1", Content: "result"}))
	if calls := s.PendingToolCalls(); calls != nil {
		t.Fatalf("want no pending tool calls after a tool message, got %v", calls)
	}
}
