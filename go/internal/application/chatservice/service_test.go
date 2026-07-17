package chatservice_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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

func TestSendExecutesToolCallsConcurrently(t *testing.T) {
	const delay = 50 * time.Millisecond

	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		if call == 0 {
			return []ports.StreamEvent{
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "1", Name: "a", Arguments: "{}"}},
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "2", Name: "b", Arguments: "{}"}},
			}
		}
		return []ports.StreamEvent{{Type: ports.EventTextDelta, Text: "done"}}
	}}

	sleepTool := func(name string) *fakeTool {
		return &fakeTool{name: name, fn: func(string) (string, error) {
			time.Sleep(delay)
			return name + " done", nil
		}}
	}

	svc := chatservice.New(llm, []ports.Tool{sleepTool("a"), sleepTool("b")})
	session := chat.NewSession("s1", "grok-4", "")

	start := time.Now()
	drain(svc.Send(context.Background(), session, "run both"))
	elapsed := time.Since(start)

	// Sequential execution would take >= 2*delay; concurrent execution
	// should finish in about one delay. Use 1.5x as the cutoff for
	// scheduler slack without weakening the assertion to meaninglessness.
	if elapsed >= delay+delay/2 {
		t.Fatalf("Send() took %v, want well under %v — two %v tool calls should run concurrently, not sequentially", elapsed, delay+delay/2, delay)
	}
}

func TestSendPreservesToolResultOrderDespiteCompletionOrder(t *testing.T) {
	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		if call == 0 {
			return []ports.StreamEvent{
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "1", Name: "slow", Arguments: "{}"}},
				{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: "2", Name: "fast", Arguments: "{}"}},
			}
		}
		return []ports.StreamEvent{{Type: ports.EventTextDelta, Text: "done"}}
	}}

	slow := &fakeTool{name: "slow", fn: func(string) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "slow result", nil
	}}
	fast := &fakeTool{name: "fast", fn: func(string) (string, error) {
		return "fast result", nil
	}}

	svc := chatservice.New(llm, []ports.Tool{slow, fast})
	session := chat.NewSession("s1", "grok-4", "")

	drain(svc.Send(context.Background(), session, "run both"))

	// user, assistant(2 tool calls), tool(slow), tool(fast), assistant("done")
	if len(session.Messages) != 5 {
		t.Fatalf("want 5 session messages, got %d: %+v", len(session.Messages), session.Messages)
	}
	if got := session.Messages[2].ToolResult.Content; got != "slow result" {
		t.Fatalf("messages[2] (first tool result) = %q, want %q (call order, not completion order)", got, "slow result")
	}
	if got := session.Messages[3].ToolResult.Content; got != "fast result" {
		t.Fatalf("messages[3] (second tool result) = %q, want %q", got, "fast result")
	}
}

func TestWithMaxConcurrentToolsLimitsParallelism(t *testing.T) {
	const numCalls = 5
	const limit = 2

	var mu sync.Mutex
	current, maxSeen := 0, 0
	track := func(string) (string, error) {
		mu.Lock()
		current++
		if current > maxSeen {
			maxSeen = current
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()
		return "ok", nil
	}

	calls := make([]ports.StreamEvent, numCalls)
	tools := make([]ports.Tool, numCalls)
	for i := 0; i < numCalls; i++ {
		name := fmt.Sprintf("tool%d", i)
		calls[i] = ports.StreamEvent{Type: ports.EventToolCall, ToolCall: chat.ToolCall{ID: fmt.Sprintf("%d", i), Name: name, Arguments: "{}"}}
		tools[i] = &fakeTool{name: name, fn: track}
	}

	llm := &scriptedLLM{script: func(call int) []ports.StreamEvent {
		if call == 0 {
			return calls
		}
		return []ports.StreamEvent{{Type: ports.EventTextDelta, Text: "done"}}
	}}

	svc := chatservice.New(llm, tools, chatservice.WithMaxConcurrentTools(limit))
	session := chat.NewSession("s1", "grok-4", "")

	drain(svc.Send(context.Background(), session, "run all"))

	mu.Lock()
	gotMax := maxSeen
	mu.Unlock()

	if gotMax > limit {
		t.Fatalf("observed max concurrency = %d, want <= %d", gotMax, limit)
	}
	if gotMax < 2 {
		t.Fatalf("observed max concurrency = %d, want at least 2 — tools should run in parallel up to the limit, not one at a time", gotMax)
	}
}
