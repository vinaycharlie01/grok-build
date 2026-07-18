// Package resilience wraps a ports.LLMProvider with a circuit breaker —
// the Go analogue of OmniRoute's provider circuit breaker (see that
// project's CLAUDE.md, "Resilience Runtime State" section, for the
// pattern this deliberately follows): stop sending traffic to a
// provider that's repeatedly failing, so one unhealthy provider doesn't
// slow down every turn.
//
// The breaker itself is github.com/sony/gobreaker/v2, not a hand-rolled
// state machine — the same "use an official/well-established library,
// don't reinvent it" rule already applied elsewhere in this tree (the
// official openai-go/anthropic-sdk-go SDKs instead of a hand-rolled HTTP
// client, counterfeiter instead of hand-rolled test mocks). An earlier
// version of this package did hand-roll the CLOSED/OPEN/HALF_OPEN state
// machine; it's gone now, replaced by gobreaker's TwoStepCircuitBreaker.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// ErrCircuitOpen wraps gobreaker's own rejection errors (ErrOpenState,
// ErrTooManyRequests) so callers can errors.Is against one sentinel from
// this package without importing gobreaker directly.
var ErrCircuitOpen = errors.New("resilience: circuit breaker open")

// Provider wraps any ports.LLMProvider with a circuit breaker.
type Provider struct {
	inner ports.LLMProvider
	cb    *gobreaker.TwoStepCircuitBreaker[any]
}

var _ ports.LLMProvider = (*Provider)(nil)

// Wrap builds a Provider around inner: threshold consecutive failures
// trip the breaker OPEN, and it allows one HALF_OPEN probe resetTimeout
// after tripping.
//
// Uses TwoStepCircuitBreaker (Allow() + a done(err) callback), not the
// simpler CircuitBreaker.Execute(func() (T, error)): Execute records the
// outcome the moment its callback returns, which fits a synchronous
// call, not a stream. A StreamChat call can synchronously return with a
// nil error and *then* the returned channel emits an EventError deep
// into the turn — the openai/anthropic adapters' priming pattern turns
// most upstream failures into a synchronous error, but not all of them.
// TwoStepCircuitBreaker's separate done() step is exactly what lets this
// wrapper watch the whole stream before telling the breaker whether the
// call actually succeeded — see StreamChat's forwarding goroutine below.
func Wrap(inner ports.LLMProvider, threshold int, resetTimeout time.Duration) *Provider {
	cb := gobreaker.NewTwoStepCircuitBreaker[any](gobreaker.Settings{
		Name:        "llm-provider",
		MaxRequests: 1,
		Timeout:     resetTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(threshold)
		},
		IsSuccessful: func(err error) bool {
			return err == nil
		},
		IsExcluded: func(err error) bool {
			// A caller giving up (context cancelled/deadline exceeded) is
			// not the provider's fault — don't let it count as a failure
			// (or a success). Excluded still frees the one HALF_OPEN
			// probe slot via gobreaker's own accounting (onExclusion),
			// unlike never calling done() at all, which would leave that
			// slot stuck "in use" until the next state change.
			return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		},
	})
	return &Provider{inner: inner, cb: cb}
}

// State returns the breaker's current state (gobreaker.StateClosed,
// .StateOpen, or .StateHalfOpen).
func (p *Provider) State() gobreaker.State { return p.cb.State() }

// StreamChat implements ports.LLMProvider. It rejects the call
// immediately with an error wrapping ErrCircuitOpen while the breaker is
// OPEN (or a HALF_OPEN probe is already in flight), without touching
// inner. Otherwise it calls inner and:
//   - a synchronous error is reported to the breaker immediately and
//     propagated, exactly as an unwrapped provider would behave.
//   - on a successful call, the returned event stream is forwarded to
//     the caller unchanged, but observed along the way: an EventError
//     anywhere in the stream is reported as a failure once the stream
//     completes; a clean completion with no EventError is reported as a
//     success. This mirrors the goroutine-leak-safe streaming pattern
//     already used elsewhere in this tree: the forwarding goroutine
//     selects on ctx.Done() so a caller that stops draining early can
//     never leave it blocked forever on an unbuffered send.
func (p *Provider) StreamChat(ctx context.Context, session *chat.Session, tools []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	done, err := p.cb.Allow()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCircuitOpen, err)
	}

	events, err := p.inner.StreamChat(ctx, session, tools)
	if err != nil {
		done(err)
		return nil, err
	}

	out := make(chan ports.StreamEvent)
	go func() {
		defer close(out)
		var streamErr error
		for ev := range events {
			if ev.Type == ports.EventError {
				streamErr = ev.Err
				if streamErr == nil {
					streamErr = errors.New("resilience: stream reported EventError with a nil Err")
				}
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				done(ctx.Err())
				return
			}
		}
		done(streamErr) // nil => success
	}()
	return out, nil
}
