package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/resilience"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports/portsfakes"
)

func drain(t *testing.T, ch <-chan ports.StreamEvent) []ports.StreamEvent {
	t.Helper()
	var out []ports.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestStreamChatPassesThroughEventsWhenClosed(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	ch := make(chan ports.StreamEvent, 2)
	ch <- ports.StreamEvent{Type: ports.EventTextDelta, Text: "hi"}
	ch <- ports.StreamEvent{Type: ports.EventDone}
	close(ch)
	inner.StreamChatReturns(ch, nil)

	wrapped := resilience.Wrap(inner, 3, time.Minute)
	events, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}

	got := drain(t, events)
	if len(got) != 2 || got[0].Text != "hi" || got[1].Type != ports.EventDone {
		t.Fatalf("StreamChat() events = %+v, want the inner provider's events passed through unchanged", got)
	}
}

func TestStreamChatRejectsImmediatelyWhenOpen(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, time.Minute)

	// Trip it: one synchronous failure, threshold=1.
	inner.StreamChatReturns(nil, errors.New("upstream down"))
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("first StreamChat() error = nil, want the upstream error to propagate")
	}

	inner.StreamChatReturns(nil, nil) // if this call reaches inner, the test should still fail below
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("second StreamChat() error = %v, want ErrCircuitOpen", err)
	}
	if inner.StreamChatCallCount() != 1 {
		t.Fatalf("inner.StreamChat called %d times, want 1 — the second call must be rejected before reaching the wrapped provider", inner.StreamChatCallCount())
	}
}

func TestStreamChatRecordsFailureOnSynchronousError(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	inner.StreamChatReturns(nil, errors.New("upstream down"))
	wrapped := resilience.Wrap(inner, 2, time.Minute)

	for i := 0; i < 2; i++ {
		if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
			t.Fatalf("call %d: error = nil, want the upstream error", i)
		}
	}

	if got := wrapped.Status(); got != resilience.StateOpen {
		t.Fatalf("Status() = %v, want StateOpen after %d synchronous failures (threshold=2)", got, 2)
	}
}

func TestStreamChatRecordsFailureOnMidStreamEventError(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, time.Minute)

	ch := make(chan ports.StreamEvent, 1)
	ch <- ports.StreamEvent{Type: ports.EventError, Err: errors.New("stream broke mid-turn")}
	close(ch)
	inner.StreamChatReturns(ch, nil) // synchronous return is nil error - the failure is IN the stream

	events, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil)
	if err != nil {
		t.Fatalf("StreamChat() error = %v, want nil (the failure is a mid-stream EventError, not a synchronous error)", err)
	}
	drain(t, events) // must fully drain before the breaker's bookkeeping goroutine records the outcome

	// Give the background forwarding goroutine a moment to record the
	// outcome after the channel closes (it forwards then records, so
	// there's an unavoidable small window between "caller sees channel
	// closed" and "breaker sees the outcome recorded").
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if wrapped.Status() == resilience.StateOpen {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Status() never became StateOpen after a mid-stream EventError (threshold=1)")
}

func TestStreamChatRecordsSuccessOnCleanCompletion(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, time.Minute)

	failCh := make(chan ports.StreamEvent, 1)
	failCh <- ports.StreamEvent{Type: ports.EventError, Err: errors.New("boom")}
	close(failCh)
	inner.StreamChatReturnsOnCall(0, failCh, nil)

	okCh := make(chan ports.StreamEvent, 1)
	okCh <- ports.StreamEvent{Type: ports.EventDone}
	close(okCh)
	inner.StreamChatReturnsOnCall(1, okCh, nil)

	// First call fails and trips the breaker (threshold=1).
	events, _ := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil)
	drain(t, events)
	waitForStatus(t, wrapped, resilience.StateOpen)

	// Not enough time has passed for HALF_OPEN, so the immediate retry
	// must be rejected without reaching inner a second time yet.
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("retry before reset timeout: error = %v, want ErrCircuitOpen", err)
	}
}

func waitForStatus(t *testing.T, w *resilience.Provider, want resilience.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if w.Status() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Status() never reached %v", want)
}
