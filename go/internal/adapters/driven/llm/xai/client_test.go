package xai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/xai"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

type fakeCreds struct{ key string }

func (f fakeCreds) APIKey() (string, error) { return f.key, nil }

// sseChunk et al. mirror just enough of the xAI streaming wire format to
// build well-formed test fixtures via json.Marshal instead of hand-typed
// (and easy to get subtly wrong) JSON literals.
type sseChunk struct {
	Choices []sseChoice `json:"choices"`
}
type sseChoice struct {
	Delta sseDelta `json:"delta"`
}
type sseDelta struct {
	Content   string        `json:"content,omitempty"`
	ToolCalls []sseToolCall `json:"tool_calls,omitempty"`
}
type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Function sseFn  `json:"function"`
}
type sseFn struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

func sseLine(t *testing.T, chunk sseChunk) string {
	t.Helper()
	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal fixture chunk: %v", err)
	}
	return "data: " + string(data) + "\n\n"
}

func TestStreamChatParsesTextAndAssemblesToolCall(t *testing.T) {
	var b strings.Builder
	b.WriteString(sseLine(t, sseChunk{Choices: []sseChoice{{Delta: sseDelta{Content: "Hel"}}}}))
	b.WriteString(sseLine(t, sseChunk{Choices: []sseChoice{{Delta: sseDelta{Content: "lo"}}}}))
	b.WriteString(sseLine(t, sseChunk{Choices: []sseChoice{{Delta: sseDelta{ToolCalls: []sseToolCall{
		{Index: 0, ID: "call_1", Function: sseFn{Name: "get_weather", Arguments: `{"city":`}},
	}}}}}))
	b.WriteString(sseLine(t, sseChunk{Choices: []sseChoice{{Delta: sseDelta{ToolCalls: []sseToolCall{
		{Index: 0, Function: sseFn{Arguments: `"NYC"}`}},
	}}}}}))
	b.WriteString("data: [DONE]\n\n")
	sseBody := b.String()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	client := xai.New(srv.URL, fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "grok-4", "")
	session.Append(chat.UserMessage("what's the weather in NYC?"))

	events, err := client.StreamChat(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}

	var got []ports.StreamEvent
	for ev := range events {
		got = append(got, ev)
	}

	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}

	if len(got) != 4 {
		t.Fatalf("want 4 events (2 deltas + 1 tool call + done), got %d: %+v", len(got), got)
	}
	if got[0].Type != ports.EventTextDelta || got[0].Text != "Hel" {
		t.Fatalf("event[0] = %+v", got[0])
	}
	if got[1].Type != ports.EventTextDelta || got[1].Text != "lo" {
		t.Fatalf("event[1] = %+v", got[1])
	}
	want := chat.ToolCall{ID: "call_1", Name: "get_weather", Arguments: `{"city":"NYC"}`}
	if got[2].Type != ports.EventToolCall || got[2].ToolCall != want {
		t.Fatalf("event[2] = %+v, want ToolCall %+v", got[2], want)
	}
	if got[3].Type != ports.EventDone {
		t.Fatalf("event[3] = %+v, want EventDone", got[3])
	}
}

func TestStreamChatPropagatesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()

	client := xai.New(srv.URL, fakeCreds{key: "bad-key"})
	session := chat.NewSession("s1", "grok-4", "")

	if _, err := client.StreamChat(context.Background(), session, nil); err == nil {
		t.Fatal("StreamChat() error = nil, want error for HTTP 401")
	}
}

func TestStreamChatPropagatesCredentialError(t *testing.T) {
	client := xai.New("http://unused.invalid", failingCreds{})
	session := chat.NewSession("s1", "grok-4", "")

	if _, err := client.StreamChat(context.Background(), session, nil); err == nil {
		t.Fatal("StreamChat() error = nil, want error when CredentialStore fails")
	}
}

type failingCreds struct{}

func (failingCreds) APIKey() (string, error) { return "", fmt.Errorf("no credentials configured") }
