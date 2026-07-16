package chatservice_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// scriptedLLM is a fake ports.LLMProvider: each call to StreamChat pulls
// the next canned event slice from script, indexed by call count.
type scriptedLLM struct {
	mu     sync.Mutex
	calls  int
	script func(call int) []ports.StreamEvent
}

func (f *scriptedLLM) StreamChat(_ context.Context, _ *chat.Session, _ []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	f.mu.Lock()
	call := f.calls
	f.calls++
	f.mu.Unlock()

	events := f.script(call)
	ch := make(chan ports.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type fakeTool struct {
	name string
	fn   func(argsJSON string) (string, error)
}

func (t *fakeTool) Name() string        { return t.name }
func (t *fakeTool) Description() string { return "fake tool for tests" }
func (t *fakeTool) JSONSchema() string  { return `{"type":"object"}` }
func (t *fakeTool) Execute(_ context.Context, argsJSON string) (string, error) {
	return t.fn(argsJSON)
}

func drain(ch <-chan ports.StreamEvent) []ports.StreamEvent {
	var out []ports.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestSendTextOnlyReply(t *testing.T) {
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		return []ports.StreamEvent{
			{Type: ports.EventTextDelta, Text: "Hello, "},
			{Type: ports.EventTextDelta, Text: "world!"},
		}
	}}

	svc := chatservice.New(llm, nil)
	session := chat.NewSession("s1", "grok-4", "")

	events := drain(svc.Send(context.Background(), session, "hi"))

	if len(events) != 3 {
		t.Fatalf("want 3 events (2 deltas + done), got %d: %+v", len(events), events)
	}
	if events[0].Text != "Hello, " || events[1].Text != "world!" {
		t.Fatalf("unexpected text deltas: %+v", events[:2])
	}
	if events[2].Type != ports.EventDone {
		t.Fatalf("want final event EventDone, got %+v", events[2])
	}

	if got := len(session.Messages); got != 2 {
		t.Fatalf("want 2 session messages (user+assistant), got %d: %+v", got, session.Messages)
	}
	last := session.Messages[len(session.Messages)-1]
	if last.Role != chat.RoleAssistant || last.Content != "Hello, world!" {
		t.Fatalf("unexpected assistant message: %+v", last)
	}
}

func TestSendExecutesToolThenContinues(t *testing.T) {
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		switch call {
		case 0:
			return []ports.StreamEvent{
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "1", Name: "echo", Arguments: `{"msg":"hi"}`}},
			}
		default:
			return []ports.StreamEvent{{Type: ports.EventTextDelta, Text: "done"}}
		}
	}}

	var gotArgs string
	tool := &fakeTool{name: "echo", fn: func(argsJSON string) (string, error) {
		gotArgs = argsJSON
		return "echoed: hi", nil
	}}

	svc := chatservice.New(llm, []ports.Tool{tool})
	session := chat.NewSession("s1", "grok-4", "")

	events := drain(svc.Send(context.Background(), session, "say hi"))

	if gotArgs != `{"msg":"hi"}` {
		t.Fatalf("tool called with unexpected args: %q", gotArgs)
	}

	var sawToolCall, sawText, sawDone bool
	for _, ev := range events {
		switch ev.Type {
		case ports.EventToolCall:
			sawToolCall = true
		case ports.EventTextDelta:
			sawText = ev.Text == "done"
		case ports.EventDone:
			sawDone = true
		}
	}
	if !sawToolCall || !sawText || !sawDone {
		t.Fatalf("missing expected event types, got %+v", events)
	}

	// user, assistant(tool_call), tool(result), assistant("done")
	if got := len(session.Messages); got != 4 {
		t.Fatalf("want 4 session messages, got %d: %+v", got, session.Messages)
	}
	toolMsg := session.Messages[2]
	if toolMsg.Role != chat.RoleTool || toolMsg.ToolResult.Content != "echoed: hi" {
		t.Fatalf("unexpected tool result message: %+v", toolMsg)
	}
}

func TestSendUnknownToolReportsErrorContentAndContinues(t *testing.T) {
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		if call == 0 {
			return []ports.StreamEvent{
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "1", Name: "missing", Arguments: `{}`}},
			}
		}
		return []ports.StreamEvent{{Type: ports.EventTextDelta, Text: "ok"}}
	}}

	svc := chatservice.New(llm, nil) // no tools registered
	session := chat.NewSession("s1", "grok-4", "")

	_ = drain(svc.Send(context.Background(), session, "hi"))

	toolMsg := session.Messages[2]
	if !toolMsg.ToolResult.IsError {
		t.Fatalf("want IsError=true for unknown tool, got %+v", toolMsg.ToolResult)
	}
}

func TestSendExceedsMaxToolHops(t *testing.T) {
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		// Always request another tool call; the model never finishes.
		return []ports.StreamEvent{
			{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "1", Name: "loop", Arguments: `{}`}},
		}
	}}
	tool := &fakeTool{name: "loop", fn: func(string) (string, error) { return "again", nil }}

	svc := chatservice.New(llm, []ports.Tool{tool}, chatservice.WithMaxToolHops(2))
	session := chat.NewSession("s1", "grok-4", "")

	events := drain(svc.Send(context.Background(), session, "hi"))

	last := events[len(events)-1]
	if last.Type != ports.EventError || !errors.Is(last.Err, chatservice.ErrMaxToolHops) {
		t.Fatalf("want final EventError wrapping ErrMaxToolHops, got %+v", last)
	}
}

func TestSendStopsOnContextCancellation(t *testing.T) {
	block := make(chan struct{})
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		<-block // never returns until the test unblocks it
		return nil
	}}

	svc := chatservice.New(llm, nil)
	session := chat.NewSession("s1", "grok-4", "")

	ctx, cancel := context.WithCancel(context.Background())
	events := svc.Send(ctx, session, "hi")
	cancel()
	close(block)

	// The channel must still close even though the turn was cancelled
	// mid-flight; drain must not hang.
	drain(events)
}
