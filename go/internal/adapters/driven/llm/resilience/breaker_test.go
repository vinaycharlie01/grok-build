package resilience_test

import (
	"testing"
	"time"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/resilience"
)

// fakeClock lets tests control "now" deterministically instead of
// sleeping — the breaker's lazy-recovery design (state re-evaluated on
// read, no background timer goroutine) is exactly what makes this
// possible: advancing a fake clock and re-querying Status()/Allow() is
// enough to exercise the OPEN -> HALF_OPEN transition.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func newTestBreaker(threshold int, resetTimeout time.Duration) (*resilience.CircuitBreaker, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return resilience.NewWithClock(threshold, resetTimeout, clock.now), clock
}

func TestClosedAllowsCallsAndSurvivesFailuresBelowThreshold(t *testing.T) {
	b, _ := newTestBreaker(3, time.Minute)

	for i := 0; i < 2; i++ {
		if !b.Allow() {
			t.Fatalf("Allow() = false before threshold failures, want true")
		}
		b.RecordFailure()
	}
	if got := b.Status(); got != resilience.StateClosed {
		t.Fatalf("Status() = %v, want StateClosed (only 2 of 3 threshold failures recorded)", got)
	}
}

func TestOpensAfterThresholdFailures(t *testing.T) {
	b, _ := newTestBreaker(3, time.Minute)

	for i := 0; i < 3; i++ {
		b.Allow()
		b.RecordFailure()
	}

	if got := b.Status(); got != resilience.StateOpen {
		t.Fatalf("Status() = %v, want StateOpen after %d threshold failures", got, 3)
	}
	if b.Allow() {
		t.Fatal("Allow() = true while OPEN and before the reset timeout, want false")
	}
}

func TestOpenTransitionsToHalfOpenAfterResetTimeoutElapses(t *testing.T) {
	b, clock := newTestBreaker(1, 30*time.Second)

	b.Allow()
	b.RecordFailure() // 1 failure trips it (threshold=1)
	if got := b.Status(); got != resilience.StateOpen {
		t.Fatalf("Status() = %v, want StateOpen", got)
	}

	clock.advance(29 * time.Second)
	if got := b.Status(); got != resilience.StateOpen {
		t.Fatalf("Status() = %v, want still StateOpen 1s before the reset timeout elapses", got)
	}

	clock.advance(2 * time.Second) // now 31s since trip, past the 30s timeout
	if got := b.Status(); got != resilience.StateHalfOpen {
		t.Fatalf("Status() = %v, want StateHalfOpen once the reset timeout has elapsed — this is lazy recovery: no background timer, just re-evaluated on read", got)
	}
}

func TestHalfOpenAllowsExactlyOneProbeAtATime(t *testing.T) {
	b, clock := newTestBreaker(1, 30*time.Second)
	b.Allow()
	b.RecordFailure()
	clock.advance(31 * time.Second)

	if !b.Allow() {
		t.Fatal("Allow() = false for the first HALF_OPEN probe, want true")
	}
	if b.Allow() {
		t.Fatal("Allow() = true for a second concurrent HALF_OPEN probe, want false — only one probe in flight at a time")
	}
}

func TestHalfOpenProbeSuccessClosesTheBreaker(t *testing.T) {
	b, clock := newTestBreaker(1, 30*time.Second)
	b.Allow()
	b.RecordFailure()
	clock.advance(31 * time.Second)

	b.Allow() // the probe
	b.RecordSuccess()

	if got := b.Status(); got != resilience.StateClosed {
		t.Fatalf("Status() = %v, want StateClosed after a successful HALF_OPEN probe", got)
	}
	if !b.Allow() {
		t.Fatal("Allow() = false immediately after closing, want true")
	}
}

func TestHalfOpenProbeFailureReopensTheBreaker(t *testing.T) {
	b, clock := newTestBreaker(1, 30*time.Second)
	b.Allow()
	b.RecordFailure()
	clock.advance(31 * time.Second)

	b.Allow() // the probe
	b.RecordFailure()

	if got := b.Status(); got != resilience.StateOpen {
		t.Fatalf("Status() = %v, want StateOpen again after a failed HALF_OPEN probe", got)
	}
	if b.Allow() {
		t.Fatal("Allow() = true immediately after re-opening, want false")
	}
}

func TestRecordSuccessResetsFailureCountInClosedState(t *testing.T) {
	b, _ := newTestBreaker(3, time.Minute)

	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordSuccess() // resets the failure count back to 0

	b.Allow()
	b.RecordFailure()
	b.Allow()
	b.RecordFailure()
	// Only 2 consecutive failures since the reset — still under threshold=3.
	if got := b.Status(); got != resilience.StateClosed {
		t.Fatalf("Status() = %v, want StateClosed — RecordSuccess should reset the failure count, not just report health", got)
	}
}
