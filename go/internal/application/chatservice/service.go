// Package chatservice contains the application layer: the single use case
// of this vertical slice, "send a user message and drive the model/tool
// loop to completion". It depends only on domain ports — never on a
// concrete adapter — which is what makes the hexagon swappable (a gRPC
// LLMProvider or an MCP-backed Tool can replace today's adapters without
// touching this file).
package chatservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// ErrMaxToolHops is returned when a single turn requests more chained tool
// calls than the service is configured to allow, guarding against a
// misbehaving model looping forever.
var ErrMaxToolHops = errors.New("chatservice: exceeded max tool-call hops for this turn")

const (
	defaultMaxToolHops        = 8
	defaultMaxConcurrentTools = 4
)

// Service is the composition point between an LLMProvider and the set of
// Tools available to it.
type Service struct {
	llm                ports.LLMProvider
	tools              map[string]ports.Tool
	toolSpecs          []ports.ToolSpec
	maxToolHops        int
	maxConcurrentTools int
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithMaxToolHops overrides the default limit on chained tool-call rounds
// within a single Send call.
func WithMaxToolHops(n int) Option {
	return func(s *Service) { s.maxToolHops = n }
}

// WithMaxConcurrentTools overrides how many tool calls from a single
// model turn may execute at once. A turn that requests, say, three
// independent file reads shouldn't wait for them one at a time — but an
// unbounded fan-out is its own risk (a model requesting 50 tool calls at
// once shouldn't spawn 50 goroutines), hence a cap rather than "all of
// them, always." Values less than 1 are clamped to 1.
func WithMaxConcurrentTools(n int) Option {
	return func(s *Service) {
		if n < 1 {
			n = 1
		}
		s.maxConcurrentTools = n
	}
}

// New builds a Service wired to the given LLMProvider and Tool set.
func New(llm ports.LLMProvider, tools []ports.Tool, opts ...Option) *Service {
	s := &Service{
		llm:                llm,
		tools:              make(map[string]ports.Tool, len(tools)),
		maxToolHops:        defaultMaxToolHops,
		maxConcurrentTools: defaultMaxConcurrentTools,
	}
	for _, t := range tools {
		s.tools[t.Name()] = t
		s.toolSpecs = append(s.toolSpecs, ports.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Send appends userInput to session as a user message, then drives the
// model/tool loop until the model produces a final answer with no further
// tool calls (or an error/hop-limit occurs). Events are streamed on the
// returned channel, which is closed when the turn ends.
func (s *Service) Send(ctx context.Context, session *chat.Session, userInput string) <-chan ports.StreamEvent {
	session.Append(chat.UserMessage(userInput))
	out := make(chan ports.StreamEvent)
	go s.run(ctx, session, out)
	return out
}

func (s *Service) run(ctx context.Context, session *chat.Session, out chan<- ports.StreamEvent) {
	defer close(out)

	for hop := 0; hop <= s.maxToolHops; hop++ {
		events, err := s.llm.StreamChat(ctx, session, s.toolSpecs)
		if err != nil {
			emit(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: err})
			return
		}

		var text strings.Builder
		var calls []chat.ToolCall
		for ev := range events {
			switch ev.Type {
			case ports.EventTextDelta:
				text.WriteString(ev.Text)
				if !emit(ctx, out, ev) {
					return
				}
			case ports.EventToolCall:
				calls = append(calls, ev.ToolCall)
				if !emit(ctx, out, ev) {
					return
				}
			case ports.EventError:
				emit(ctx, out, ev)
				return
			case ports.EventDone:
				// Assembled below, once the events channel closes.
			}
		}

		session.Append(chat.AssistantMessage(text.String(), calls))

		if len(calls) == 0 {
			emit(ctx, out, ports.StreamEvent{Type: ports.EventDone})
			return
		}

		for _, result := range s.executeToolsConcurrently(ctx, calls) {
			session.Append(chat.ToolMessage(result))
		}
		// Loop: feed tool results back to the model for the next hop.
	}

	emit(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: ErrMaxToolHops})
}

// executeToolsConcurrently runs every call from one turn in parallel, up
// to maxConcurrentTools at a time, and returns their results in the same
// order calls were given — not completion order. Each goroutine writes
// to its own index of a pre-sized slice, so there's no shared mutable
// state between them and nothing to race on; errgroup.SetLimit bounds
// how many run at once so a turn with many tool calls can't fan out
// unboundedly.
func (s *Service) executeToolsConcurrently(ctx context.Context, calls []chat.ToolCall) []chat.ToolResult {
	results := make([]chat.ToolResult, len(calls))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.maxConcurrentTools)
	for i, call := range calls {
		g.Go(func() error {
			results[i] = s.executeTool(gctx, call)
			return nil // executeTool reports failure via ToolResult.IsError, never a Go error
		})
	}
	_ = g.Wait() // never non-nil: no Go() func above returns an error

	return results
}

func (s *Service) executeTool(ctx context.Context, call chat.ToolCall) chat.ToolResult {
	tool, ok := s.tools[call.Name]
	if !ok {
		return chat.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("unknown tool %q", call.Name),
			IsError:    true,
		}
	}
	content, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return chat.ToolResult{ToolCallID: call.ID, Content: err.Error(), IsError: true}
	}
	return chat.ToolResult{ToolCallID: call.ID, Content: content}
}

// emit sends ev on out, returning false if ctx was cancelled first so the
// caller can unwind instead of blocking forever on a channel nobody reads.
func emit(ctx context.Context, out chan<- ports.StreamEvent, ev ports.StreamEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
