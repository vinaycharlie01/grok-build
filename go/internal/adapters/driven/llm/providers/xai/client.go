// Package xai implements ports.LLMProvider against xAI's OpenAI-compatible
// streaming chat completions API (https://api.x.ai/v1/chat/completions).
// This is the only place in the codebase allowed to know about that wire
// format — everything else talks to the domain port.
package xai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// Client is a ports.LLMProvider backed by the xAI chat completions API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	creds      ports.CredentialStore
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the default http.Client (useful for tests).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New builds a Client targeting baseURL (e.g. "https://api.x.ai/v1"),
// authenticating requests using the given CredentialStore.
func New(baseURL string, creds ports.CredentialStore, opts ...Option) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		creds:      creds,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// --- wire format -----------------------------------------------------------

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolDef struct {
	Type     string              `json:"type"`
	Function wireToolDefFunction `json:"function"`
}

type wireToolDefFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireToolDef `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// --- ports.LLMProvider -------------------------------------------------

// StreamChat implements ports.LLMProvider.
func (c *Client) StreamChat(ctx context.Context, session *chat.Session, tools []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	apiKey, err := c.creds.APIKey()
	if err != nil {
		return nil, fmt.Errorf("xai: resolve API key: %w", err)
	}

	reqBody, err := json.Marshal(chatRequest{
		Model:    session.Model,
		Messages: toWireMessages(session.Messages),
		Tools:    toWireTools(tools),
		Stream:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("xai: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("xai: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai: request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("xai: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	out := make(chan ports.StreamEvent)
	go consumeSSE(ctx, resp.Body, out)
	return out, nil
}

func toWireMessages(msgs []chat.Message) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case chat.RoleTool:
			out = append(out, wireMessage{
				Role:       "tool",
				ToolCallID: m.ToolResult.ToolCallID,
				Content:    m.ToolResult.Content,
			})
		case chat.RoleAssistant:
			wm := wireMessage{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: wireToolFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
			out = append(out, wm)
		default:
			out = append(out, wireMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	return out
}

func toWireTools(specs []ports.ToolSpec) []wireToolDef {
	if len(specs) == 0 {
		return nil
	}
	out := make([]wireToolDef, 0, len(specs))
	for _, s := range specs {
		out = append(out, wireToolDef{
			Type: "function",
			Function: wireToolDefFunction{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  json.RawMessage(s.JSONSchema),
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

// consumeSSE runs in its own goroutine for the lifetime of one streamed
// turn. Every send to out is routed through sendEvent, which selects on
// ctx.Done(): without that, a consumer that stops draining out (because
// its own ctx was cancelled while this goroutine still has buffered
// events to deliver) would leave this goroutine blocked forever on an
// unbuffered channel send — a goroutine leak on every cancelled request.
func consumeSSE(ctx context.Context, body io.ReadCloser, out chan<- ports.StreamEvent) {
	defer close(out)
	defer body.Close()

	calls := map[int]*toolCallAccum{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

scanLoop:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			break scanLoop
		default:
		}

		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			data, ok = strings.CutPrefix(line, "data:")
		}
		if !ok || data == "" {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: fmt.Errorf("xai: decode chunk: %w", err)})
			return
		}
		if chunk.Error != nil {
			sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: fmt.Errorf("xai: %s", chunk.Error.Message)})
			return
		}
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
	if err := scanner.Err(); err != nil {
		sendEvent(ctx, out, ports.StreamEvent{Type: ports.EventError, Err: fmt.Errorf("xai: read stream: %w", err)})
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

func sortedKeys(m map[int]*toolCallAccum) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
