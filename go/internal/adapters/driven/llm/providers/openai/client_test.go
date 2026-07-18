package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/openai"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

type fakeCreds struct{ key string }

func (f fakeCreds) APIKey() (string, error) { return f.key, nil }

type failingCreds struct{}

func (failingCreds) APIKey() (string, error) { return "", fmt.Errorf("no credentials configured") }

// sseChunk builds one well-formed OpenAI streaming chunk line. %q handles
// JSON escaping; every field the SDK's decoder needs is present with a
// plausible constant value so we're only varying what each test cares
// about (content / tool_calls).
func sseChunk(deltaJSON string) string {
	return "data: " + fmt.Sprintf(
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":%s,"finish_reason":null}]}`,
		deltaJSON,
	) + "\n\n"
}

func sseTextDelta(text string) string {
	return sseChunk(fmt.Sprintf(`{"content":%q}`, text))
}

func sseToolCallStart(index int, id, name, argsFragment string) string {
	return sseChunk(fmt.Sprintf(
		`{"tool_calls":[{"index":%d,"id":%q,"function":{"name":%q,"arguments":%q}}]}`,
		index, id, name, argsFragment,
	))
}

func sseToolCallArgs(index int, argsFragment string) string {
	return sseChunk(fmt.Sprintf(
		`{"tool_calls":[{"index":%d,"function":{"arguments":%q}}]}`,
		index, argsFragment,
	))
}

func TestStreamChatParsesTextDeltas(t *testing.T) {
	sseBody := sseTextDelta("Hel") + sseTextDelta("lo") + "data: [DONE]\n\n"

	client := openai.New(newSSEServer(t, sseBody), fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "gpt-4o", "")

	got := drain(t, client, session)

	if len(got) != 3 { // 2 deltas + done
		t.Fatalf("want 3 events, got %d: %+v", len(got), got)
	}
	if got[0].Type != ports.EventTextDelta || got[0].Text != "Hel" {
		t.Fatalf("event[0] = %+v, want text delta %q", got[0], "Hel")
	}
	if got[1].Type != ports.EventTextDelta || got[1].Text != "lo" {
		t.Fatalf("event[1] = %+v, want text delta %q", got[1], "lo")
	}
	if got[2].Type != ports.EventDone {
		t.Fatalf("event[2] = %+v, want EventDone", got[2])
	}
}

func TestStreamChatAssemblesToolCallAcrossChunks(t *testing.T) {
	sseBody := sseToolCallStart(0, "call_1", "get_weather", `{"city":`) +
		sseToolCallArgs(0, `"NYC"}`) +
		"data: [DONE]\n\n"

	client := openai.New(newSSEServer(t, sseBody), fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "gpt-4o", "")

	got := drain(t, client, session)

	want := chat.ToolCall{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`}
	if len(got) != 2 { // 1 assembled tool call + done
		t.Fatalf("want 2 events, got %d: %+v", len(got), got)
	}
	if got[0].Type != ports.EventToolCall || got[0].ToolCall != want {
		t.Fatalf("event[0] = %+v, want ToolCall %+v", got[0], want)
	}
	if got[1].Type != ports.EventDone {
		t.Fatalf("event[1] = %+v, want EventDone", got[1])
	}
}

func TestStreamChatSendsBearerAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := openai.New(srv.URL, fakeCreds{key: "test-key"})
	drain(t, client, chat.NewSession("s1", "gpt-4o", ""))

	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
}

func TestStreamChatSendsToolsAndFullConversationHistory(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := openai.New(srv.URL, fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "gpt-4o", "be terse")
	session.Append(chat.AssistantMessage("", []chat.ToolCall{
		{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`},
	}))
	session.Append(chat.ToolMessage(chat.ToolResult{ToolCallID: "call_1", Content: "sunny"}))

	tools := []ports.ToolSpec{{
		Name:        "get_weather",
		Description: "gets the weather for a city",
		JSONSchema:  `{"type":"object","properties":{"city":{"type":"string"}}}`,
	}}

	events, err := client.StreamChat(context.Background(), session, tools)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	for range events {
		// This test only cares about the request body the server
		// received; still must drain the channel so consumeStream's
		// goroutine can send its final event and exit instead of
		// blocking on it forever (goleak's TestMain in main_test.go
		// caught this leak when this loop was missing).
	}

	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v\nbody: %s", err, gotBody)
	}

	if len(req.Messages) != 3 { // system, assistant(tool_call), tool(result)
		t.Fatalf("want 3 messages, got %d: %s", len(req.Messages), gotBody)
	}
	if req.Messages[0].Role != "system" {
		t.Fatalf("messages[0].Role = %q, want %q", req.Messages[0].Role, "system")
	}
	assistantMsg := req.Messages[1]
	if assistantMsg.Role != "assistant" || len(assistantMsg.ToolCalls) != 1 || assistantMsg.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("messages[1] = %+v, want an assistant message with a get_weather tool call", assistantMsg)
	}
	toolResultMsg := req.Messages[2]
	if toolResultMsg.Role != "tool" || toolResultMsg.ToolCallID != "call_1" {
		t.Fatalf("messages[2] = %+v, want the tool result for call_1", toolResultMsg)
	}

	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools = %+v, want one get_weather tool", req.Tools)
	}
	if req.Tools[0].Function.Parameters["type"] != "object" {
		t.Fatalf("tools[0].Function.Parameters = %+v, want the object schema passed in", req.Tools[0].Function.Parameters)
	}
}

func TestStreamChatPropagatesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	client := openai.New(srv.URL, fakeCreds{key: "bad-key"})
	session := chat.NewSession("s1", "gpt-4o", "")

	if _, err := client.StreamChat(context.Background(), session, nil); err == nil {
		t.Fatal("StreamChat() error = nil, want error for HTTP 401")
	}
}

func TestStreamChatPropagatesCredentialError(t *testing.T) {
	client := openai.New("http://unused.invalid", failingCreds{})
	session := chat.NewSession("s1", "gpt-4o", "")

	if _, err := client.StreamChat(context.Background(), session, nil); err == nil {
		t.Fatal("StreamChat() error = nil, want error when CredentialStore fails")
	}
}

// newSSEServer starts a test server that always replies with body as an
// SSE stream and returns its URL; the server is closed when t completes.
func newSSEServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// drain runs one turn and collects every event it produces.
func drain(t *testing.T, client *openai.Client, session *chat.Session) []ports.StreamEvent {
	t.Helper()
	events, err := client.StreamChat(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	var got []ports.StreamEvent
	for ev := range events {
		got = append(got, ev)
	}
	return got
}
