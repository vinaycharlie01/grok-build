// Package anthropic implements ports.LLMProvider using the official
// Anthropic Go SDK (github.com/anthropics/anthropic-sdk-go) — no
// hand-rolled HTTP/SSE client, same rule as every other provider in this
// tree (see ROADMAP.md's "Library & framework choices").
//
// Anthropic's Messages API has a materially different shape from OpenAI's
// chat completions: the system prompt is a top-level request field, not a
// message; there is no "tool" role — tool results are content blocks
// inside a user-role message; and an assistant turn's text and tool calls
// are both content blocks on one message rather than separate fields. All
// of that translation lives in toSDKMessages below and nowhere else.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// defaultMaxTokens is required by Anthropic's API (unlike OpenAI's
// optional max_tokens) with no server-side default, so this client must
// supply one.
const defaultMaxTokens = 4096

// Client is a ports.LLMProvider backed by the Anthropic Messages API.
type Client struct {
	sdk       anthropicsdk.Client
	creds     ports.CredentialStore
	maxTokens int64
}

// Option configures a Client at construction time.
type Option func(*clientConfig)

type clientConfig struct {
	sdkOpts   []option.RequestOption
	maxTokens int64
}

// WithHTTPClient overrides the default http.Client (useful for tests).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) { c.sdkOpts = append(c.sdkOpts, option.WithHTTPClient(hc)) }
}

// WithMaxTokens overrides the max_tokens sent with every request.
func WithMaxTokens(n int64) Option {
	return func(c *clientConfig) { c.maxTokens = n }
}

// New builds a Client targeting baseURL (e.g. "https://api.anthropic.com"),
// authenticating requests using the given CredentialStore.
func New(baseURL string, creds ports.CredentialStore, opts ...Option) *Client {
	cfg := clientConfig{maxTokens: defaultMaxTokens}
	for _, opt := range opts {
		opt(&cfg)
	}
	sdkOpts := append([]option.RequestOption{option.WithBaseURL(baseURL)}, cfg.sdkOpts...)
	return &Client{
		sdk:       anthropicsdk.NewClient(sdkOpts...),
		creds:     creds,
		maxTokens: cfg.maxTokens,
	}
}

// StreamChat implements ports.LLMProvider.
func (c *Client) StreamChat(ctx context.Context, session *chat.Session, tools []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	apiKey, err := c.creds.APIKey()
	if err != nil {
		return nil, fmt.Errorf("anthropic: resolve API key: %w", err)
	}

	system, messages := toSDKMessages(session.Messages)
	params := anthropicsdk.MessageNewParams{
		Model:     session.Model,
		MaxTokens: c.maxTokens,
		Messages:  messages,
		System:    system,
	}
	if len(tools) > 0 {
		params.Tools = toSDKTools(tools)
	}

	stream := c.sdk.Messages.NewStreaming(ctx, params, option.WithAPIKey(apiKey))

	// Prime the stream so an HTTP-level failure (bad auth, 4xx/5xx) surfaces
	// as a synchronous error here — matching every other provider adapter's
	// contract — instead of only appearing as an EventError once the caller
	// starts draining the channel.
	hasFirst := stream.Next()
	if err := stream.Err(); err != nil {
		stream.Close()
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}

	out := make(chan ports.StreamEvent)
	go consumeStream(ctx, stream, hasFirst, out)
	return out, nil
}

func toSDKMessages(msgs []chat.Message) ([]anthropicsdk.TextBlockParam, []anthropicsdk.MessageParam) {
	var system []anthropicsdk.TextBlockParam
	var out []anthropicsdk.MessageParam

	for _, m := range msgs {
		switch m.Role {
		case chat.RoleSystem:
			system = append(system, anthropicsdk.TextBlockParam{Text: m.Content})

		case chat.RoleTool:
			out = append(out, anthropicsdk.NewUserMessage(
				anthropicsdk.NewToolResultBlock(m.ToolResult.ToolCallID, m.ToolResult.Content, m.ToolResult.IsError),
			))

		case chat.RoleAssistant:
			var blocks []anthropicsdk.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropicsdk.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input any
				_ = json.Unmarshal([]byte(tc.Arguments), &input) // malformed args -> nil input, model sees an empty call
				blocks = append(blocks, anthropicsdk.NewToolUseBlock(tc.ID, input, tc.Name))
			}
			out = append(out, anthropicsdk.NewAssistantMessage(blocks...))

		default: // chat.RoleUser
			out = append(out, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(m.Content)))
		}
	}
	return system, out
}

func toSDKTools(specs []ports.ToolSpec) []anthropicsdk.ToolUnionParam {
	out := make([]anthropicsdk.ToolUnionParam, 0, len(specs))
	for _, s := range specs {
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		_ = json.Unmarshal([]byte(s.JSONSchema), &schema) // malformed schema -> empty params

		out = append(out, anthropicsdk.ToolUnionParam{
			OfTool: &anthropicsdk.ToolParam{
				Name:        s.Name,
				Description: param.NewOpt(s.Description),
				InputSchema: anthropicsdk.ToolInputSchemaParam{
					Properties: schema.Properties,
					Required:   schema.Required,
				},
			},
		})
	}
	return out
}

// toolCallAccum assembles a tool_use content block's streamed pieces: id
// and name arrive on content_block_start, arguments arrive incrementally
// as input_json_delta fragments on content_block_delta.
type toolCallAccum struct {
	id, name string
	args     strings.Builder
}

// consumeStream runs in its own goroutine for the lifetime of one streamed
// turn. Every send to out goes through sendEvent, which selects on
// ctx.Done(): without that, a consumer that stops draining out early would
// leave this goroutine blocked forever on an unbuffered channel send.
func consumeStream(ctx context.Context, stream *ssestream.Stream[anthropicsdk.MessageStreamEventUnion], hasFirst bool, out chan<- ports.StreamEvent) {
	defer close(out)
	defer stream.Close()

	calls := map[int64]*toolCallAccum{}

	for hasNext := hasFirst; hasNext; hasNext = stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				calls[ev.Index] = &toolCallAccum{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				if !sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventTextDelta, Text: ev.Delta.Text}) {
					return
				}
			case "input_json_delta":
				if acc, ok := calls[ev.Index]; ok {
					acc.args.WriteString(ev.Delta.PartialJSON)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: fmt.Errorf("anthropic: stream: %w", err)})
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
