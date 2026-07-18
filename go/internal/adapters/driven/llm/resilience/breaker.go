// Package resilience wraps a ports.LLMProvider with a circuit breaker —
// the Go analogue of OmniRoute's provider circuit breaker (see that
// project's CLAUDE.md, "Resilience Runtime State" section, for the
// pattern this deliberately follows): stop sending traffic to a
// provider that's repeatedly failing, so one unhealthy provider doesn't
// slow down every turn.
package resilience

import (
	"sync"
	"time"
)

// State is a circuit breaker's current state.
type State int

const (
	// StateClosed is normal operation: calls are allowed through.
	StateClosed State = iota
	// StateOpen means the failure threshold has been reached; calls are
	// rejected immediately without touching the wrapped provider, until
	// the reset timeout elapses.
	StateOpen
	// StateHalfOpen means the reset timeout has elapsed since the last
	// trip; exactly one probe call is allowed through to test recovery.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker is a CLOSED/OPEN/HALF_OPEN state machine with lazy
// recovery: there is no background timer goroutine polling for when to
// transition out of OPEN. Instead, every read (Status, Allow) first
// checks whether the reset timeout has elapsed and transitions to
// HALF_OPEN right there if so — the same lazy-recovery approach
// OmniRoute's provider circuit breaker uses, chosen for the same reason:
// a dashboard or candidate-selection read should never see a provider
// stuck OPEN forever just because nothing happened to tick a timer.
type CircuitBreaker struct {
	mu           sync.Mutex
	threshold    int
	resetTimeout time.Duration
	now          func() time.Time

	state                 State
	failures              int
	openedAt              time.Time
	halfOpenProbeInFlight bool
}

// New builds a CircuitBreaker that trips to OPEN after threshold
// consecutive failures (in CLOSED state) and allows one HALF_OPEN probe
// resetTimeout after it trips.
func New(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	return NewWithClock(threshold, resetTimeout, time.Now)
}

// NewWithClock is New with an injectable clock, for deterministic tests
// that advance time without sleeping (see breaker_test.go's fakeClock).
func NewWithClock(threshold int, resetTimeout time.Duration, now func() time.Time) *CircuitBreaker {
	return &CircuitBreaker{threshold: threshold, resetTimeout: resetTimeout, now: now}
}

// Status returns the breaker's current state, lazily transitioning
// OPEN -> HALF_OPEN first if the reset timeout has elapsed.
func (b *CircuitBreaker) Status() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshLocked()
	return b.state
}

// Allow reports whether a call should be permitted right now: always in
// CLOSED, never in OPEN, and exactly once at a time in HALF_OPEN (a
// second concurrent call while a probe is already in flight is refused,
// so a burst of concurrent requests can't all become probes at once).
func (b *CircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshLocked()

	switch b.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		if b.halfOpenProbeInFlight {
			return false
		}
		b.halfOpenProbeInFlight = true
		return true
	default: // StateOpen
		return false
	}
}

// RecordSuccess reports a successful call: closes the breaker (from
// HALF_OPEN, this is recovery; from CLOSED, it resets the failure
// count — a call succeeding means whatever run of failures preceded it
// is no longer relevant).
func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = StateClosed
	b.halfOpenProbeInFlight = false
}

// RecordFailure reports a failed call: in HALF_OPEN, the probe failed,
// so trip back to OPEN immediately (no second chance). In CLOSED,
// increment the failure count and trip to OPEN once threshold is
// reached.
func (b *CircuitBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateHalfOpen:
		b.trip()
	case StateClosed:
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	}
	b.halfOpenProbeInFlight = false
}

func (b *CircuitBreaker) trip() {
	b.state = StateOpen
	b.openedAt = b.now()
	b.failures = 0
}

// refreshLocked must be called with mu held. It performs the lazy
// OPEN -> HALF_OPEN transition described on CircuitBreaker's doc comment.
func (b *CircuitBreaker) refreshLocked() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.resetTimeout {
		b.state = StateHalfOpen
	}
}
