package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/resilience"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports/portsfakes"
)

// shortResetTimeout is used by the handful of tests below that actually
// need OPEN to elapse into HALF_OPEN. gobreaker/v2 has no injectable
// clock in its public API (unlike this package's earlier hand-rolled
// breaker), so those tests use a real, short Timeout plus a real
// time.Sleep well past it — the honest way to test the real library's
// actual behavior, at the cost of a small amount of wall-clock time and
// a theoretical flakiness risk on an extremely loaded runner. The 6x
// margin (sleep 6x the timeout) is meant to make that risk negligible
// without slowing the suite down noticeably.
const shortResetTimeout = 20 * time.Millisecond

func drain(t *testing.T, ch <-chan ports.StreamEvent) []ports.StreamEvent {
	t.Helper()
	var out []ports.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func waitForState(t *testing.T, w *resilience.Provider, want gobreaker.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if w.State() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("State() never reached %v, still %v", want, w.State())
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
	waitForState(t, wrapped, gobreaker.StateClosed)
}

func TestStreamChatRejectsImmediatelyWhenOpen(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, time.Minute)

	inner.StreamChatReturns(nil, errors.New("upstream down"))
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("first StreamChat() error = nil, want the upstream error to propagate")
	}
	waitForState(t, wrapped, gobreaker.StateOpen)

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

	waitForState(t, wrapped, gobreaker.StateOpen)
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

	waitForState(t, wrapped, gobreaker.StateOpen)
}

func TestStreamChatRecordsSuccessOnCleanCompletion(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, time.Minute)

	failCh := make(chan ports.StreamEvent, 1)
	failCh <- ports.StreamEvent{Type: ports.EventError, Err: errors.New("boom")}
	close(failCh)
	inner.StreamChatReturnsOnCall(0, failCh, nil)

	// First call fails and trips the breaker (threshold=1).
	events, _ := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil)
	drain(t, events)
	waitForState(t, wrapped, gobreaker.StateOpen)

	// Not enough time has passed for HALF_OPEN (Timeout is a full
	// minute), so the immediate retry must be rejected without reaching
	// inner a second time yet.
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("retry before reset timeout: error = %v, want ErrCircuitOpen", err)
	}
	if inner.StreamChatCallCount() != 1 {
		t.Fatalf("inner.StreamChat called %d times, want 1 (the rejected retry must not reach inner)", inner.StreamChatCallCount())
	}
}

func TestBreakerAllowsOneHalfOpenProbeAfterResetTimeoutThenCloses(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, shortResetTimeout)

	inner.StreamChatReturns(nil, errors.New("upstream down"))
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("want the synchronous failure to propagate")
	}
	waitForState(t, wrapped, gobreaker.StateOpen)

	time.Sleep(6 * shortResetTimeout) // real time - see shortResetTimeout's doc comment

	okCh := make(chan ports.StreamEvent, 1)
	okCh <- ports.StreamEvent{Type: ports.EventDone}
	close(okCh)
	inner.StreamChatReturns(okCh, nil)

	events, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil)
	if err != nil {
		t.Fatalf("HALF_OPEN probe call StreamChat() error = %v, want nil — exactly one probe should be allowed through", err)
	}
	drain(t, events)

	waitForState(t, wrapped, gobreaker.StateClosed)
}

func TestBreakerReopensAfterFailedHalfOpenProbe(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, shortResetTimeout)

	inner.StreamChatReturns(nil, errors.New("upstream down"))
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("want the synchronous failure to propagate")
	}
	waitForState(t, wrapped, gobreaker.StateOpen)

	time.Sleep(6 * shortResetTimeout)

	// The probe also fails.
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("HALF_OPEN probe: want the synchronous failure to propagate")
	}

	waitForState(t, wrapped, gobreaker.StateOpen)
}

func TestBreakerAllowsOnlyOneHalfOpenProbeAtATime(t *testing.T) {
	inner := &portsfakes.FakeLLMProvider{}
	wrapped := resilience.Wrap(inner, 1, shortResetTimeout)

	inner.StreamChatReturns(nil, errors.New("upstream down"))
	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err == nil {
		t.Fatal("want the synchronous failure to propagate")
	}
	waitForState(t, wrapped, gobreaker.StateOpen)

	time.Sleep(6 * shortResetTimeout)

	// The probe's stream is left undrained/unfinished on purpose: its
	// done() callback (called by the forwarding goroutine only once the
	// caller finishes draining, or ctx is cancelled) hasn't fired yet, so
	// the HALF_OPEN slot is still "in use" when the second call arrives.
	blockingCh := make(chan ports.StreamEvent)
	inner.StreamChatReturns(blockingCh, nil)

	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); err != nil {
		t.Fatalf("first HALF_OPEN probe: StreamChat() error = %v, want nil", err)
	}

	if _, err := wrapped.StreamChat(context.Background(), chat.NewSession("s1", "m", ""), nil); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("second concurrent HALF_OPEN probe: error = %v, want ErrCircuitOpen (only one probe in flight at a time)", err)
	}

	close(blockingCh) // release the first probe's goroutine so it doesn't leak past the test
}
