// Package openai implements ports.LLMProvider using the official OpenAI Go
// SDK (github.com/openai/openai-go) instead of a hand-rolled HTTP/SSE
// client — see ROADMAP.md's "Library & framework choices". The SDK is an
// implementation detail of this one adapter; nothing above this package
// knows it exists.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// Client is a ports.LLMProvider backed by the OpenAI chat completions API.
type Client struct {
	sdk   openaisdk.Client
	creds ports.CredentialStore
}

// Option configures a Client at construction time.
type Option func(*[]option.RequestOption)

// WithHTTPClient overrides the default http.Client (useful for tests).
func WithHTTPClient(hc *http.Client) Option {
	return func(opts *[]option.RequestOption) {
		*opts = append(*opts, option.WithHTTPClient(hc))
	}
}

// New builds a Client targeting baseURL (e.g. "https://api.openai.com/v1"),
// authenticating requests using the given CredentialStore.
func New(baseURL string, creds ports.CredentialStore, opts ...Option) *Client {
	sdkOpts := []option.RequestOption{option.WithBaseURL(baseURL)}
	for _, opt := range opts {
		opt(&sdkOpts)
	}
	return &Client{sdk: openaisdk.NewClient(sdkOpts...), creds: creds}
}

// StreamChat implements ports.LLMProvider.
func (c *Client) StreamChat(ctx context.Context, session *chat.Session, tools []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	apiKey, err := c.creds.APIKey()
	if err != nil {
		return nil, fmt.Errorf("openai: resolve API key: %w", err)
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:    session.Model,
		Messages: toSDKMessages(session.Messages),
	}
	if len(tools) > 0 {
		params.Tools = toSDKTools(tools)
	}

	stream := c.sdk.Chat.Completions.NewStreaming(ctx, params, option.WithAPIKey(apiKey))

	// Prime the stream so an HTTP-level failure (bad auth, 4xx/5xx) surfaces
	// as a synchronous error here — matching every other provider adapter's
	// contract — instead of only appearing as an EventError once the caller
	// starts draining the channel.
	hasFirst := stream.Next()
	if err := stream.Err(); err != nil {
		stream.Close()
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	out := make(chan ports.StreamEvent)
	go consumeStream(ctx, stream, hasFirst, out)
	return out, nil
}

func toSDKMessages(msgs []chat.Message) []openaisdk.ChatCompletionMessageParamUnion {
	out := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case chat.RoleSystem:
			out = append(out, openaisdk.SystemMessage(m.Content))
		case chat.RoleTool:
			out = append(out, openaisdk.ToolMessage(m.ToolResult.Content, m.ToolResult.ToolCallID))
		case chat.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				out = append(out, openaisdk.AssistantMessage(m.Content))
				continue
			}
			toolCalls := make([]openaisdk.ChatCompletionMessageToolCallParam, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				toolCalls = append(toolCalls, openaisdk.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openaisdk.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{
				OfAssistant: &openaisdk.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCalls,
				},
			})
		default: // chat.RoleUser
			out = append(out, openaisdk.UserMessage(m.Content))
		}
	}
	return out
}

func toSDKTools(specs []ports.ToolSpec) []openaisdk.ChatCompletionToolParam {
	out := make([]openaisdk.ChatCompletionToolParam, 0, len(specs))
	for _, s := range specs {
		var schema shared.FunctionParameters
		_ = json.Unmarshal([]byte(s.JSONSchema), &schema) // malformed schema -> empty params, model sees no args

		out = append(out, openaisdk.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        s.Name,
				Description: param.NewOpt(s.Description),
				Parameters:  schema,
			},
		})
	}
	return out
}

// toolCallAccum assembles the fragmented tool-call deltas the API streams
// (id/name usually arrive once, arguments arrive incrementally).
type toolCallAccum struct {
	id   string
	name string
	args strings.Builder
}

// consumeStream runs in its own goroutine for the lifetime of one streamed
// turn. Every send to out goes through sendEvent, which selects on
// ctx.Done(): without that, a consumer that stops draining out early would
// leave this goroutine blocked forever on an unbuffered channel send.
func consumeStream(ctx context.Context, stream *ssestream.Stream[openaisdk.ChatCompletionChunk], hasFirst bool, out chan<- ports.StreamEvent) {
	defer close(out)
	defer stream.Close()

	calls := map[int64]*toolCallAccum{}

	for hasNext := hasFirst; hasNext; hasNext = stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			if !sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventTextDelta, Text: delta.Content}) {
				return
			}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := calls[tc.Index]
			if !ok {
				acc = &toolCallAccum{}
				calls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := stream.Err(); err != nil {
		sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: fmt.Errorf("openai: stream: %w", err)})
		return
	}

	for _, idx := range sortedKeys(calls) {
		acc := calls[idx]
		if !sendEvent(ctx, out, ports.StreamEvent{
			Type: ports.EventToolCall,
			ToolCall: chat.ToolCall{
				ID:        acc.id,
				Name:      acc.name,
				Arguments: acc.args.String(),
			},
		}) {
			return
		}
	}
	sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventDone})
}

// sendEvent delivers ev on out, returning false without blocking forever
// if ctx is cancelled before a reader is ready to receive it.
func sendEvent(ctx context.Context, out chan<- ports.StreamEvent, ev ports.StreamEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func sortedKeys(m map[int64]*toolCallAccum) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
