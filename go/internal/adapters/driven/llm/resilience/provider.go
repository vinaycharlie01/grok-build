package resilience

import (
	"context"
	"errors"
	"time"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// ErrCircuitOpen is returned by Provider.StreamChat instead of calling
// through to the wrapped provider while the breaker is OPEN.
var ErrCircuitOpen = errors.New("resilience: circuit breaker open")

// Provider wraps any ports.LLMProvider with a CircuitBreaker, so a
// provider that's repeatedly failing gets skipped for a cooldown period
// instead of every turn paying the latency of a doomed request.
type Provider struct {
	inner   ports.LLMProvider
	breaker *CircuitBreaker
}

var _ ports.LLMProvider = (*Provider)(nil)

// Wrap builds a Provider around inner: threshold consecutive failures
// trip the breaker OPEN, and it allows a HALF_OPEN probe resetTimeout
// after tripping.
func Wrap(inner ports.LLMProvider, threshold int, resetTimeout time.Duration) *Provider {
	return &Provider{inner: inner, breaker: New(threshold, resetTimeout)}
}

// Status reports the wrapped breaker's current state.
func (p *Provider) Status() State { return p.breaker.Status() }

// StreamChat implements ports.LLMProvider. It rejects the call
// immediately with ErrCircuitOpen while the breaker is OPEN, without
// touching inner. Otherwise it calls inner and:
//   - a synchronous error trips the breaker's failure counter and
//     propagates the error, exactly as an unwrapped provider would.
//   - on a successful call, the returned event stream is forwarded to
//     the caller unchanged, but observed along the way: an EventError
//     anywhere in the stream counts as a failure (most upstream failures
//     surface synchronously thanks to the openai/anthropic adapters'
//     priming pattern, but a stream can still fail mid-turn), and a
//     clean completion with no EventError counts as a success. This
//     mirrors the goroutine-leak-safe streaming pattern already used
//     elsewhere in this tree: the forwarding goroutine selects on
//     ctx.Done() so a caller that stops draining early can never leave
//     it blocked forever on an unbuffered send.
func (p *Provider) StreamChat(ctx context.Context, session *chat.Session, tools []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	if !p.breaker.Allow() {
		return nil, ErrCircuitOpen
	}

	events, err := p.inner.StreamChat(ctx, session, tools)
	if err != nil {
		p.breaker.RecordFailure()
		return nil, err
	}

	out := make(chan ports.StreamEvent)
	go func() {
		defer close(out)
		failed := false
		for ev := range events {
			if ev.Type == ports.EventError {
				failed = true
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
		if failed {
			p.breaker.RecordFailure()
		} else {
			p.breaker.RecordSuccess()
		}
	}()
	return out, nil
}
