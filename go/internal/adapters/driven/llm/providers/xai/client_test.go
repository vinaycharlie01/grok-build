package xai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/xai"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

type fakeCreds struct{ key string }

func (f fakeCreds) APIKey() (string, error) { return f.key, nil }

type failingCreds struct{}

func (failingCreds) APIKey() (string, error) { return "", fmt.Errorf("no credentials configured") }

// Three tiny, fixed-shape SSE line builders, one per fixture the tests
// need. %q handles JSON string-escaping for us, so there's no hand-typed
// JSON to get subtly wrong — each function is one literal template, not a
// struct hierarchy to assemble.
func sseTextDelta(text string) string {
	return "data: " + fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text) + "\n\n"
}

func sseToolCallStart(index int, id, name, argsFragment string) string {
	return "data: " + fmt.Sprintf(
		`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":%q,"function":{"name":%q,"arguments":%q}}]}}]}`,
		index, id, name, argsFragment,
	) + "\n\n"
}

func sseToolCallArgs(index int, argsFragment string) string {
	return "data: " + fmt.Sprintf(
		`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":%q}}]}}]}`,
		index, argsFragment,
	) + "\n\n"
}

func TestStreamChatParsesTextDeltas(t *testing.T) {
	sseBody := sseTextDelta("Hel") + sseTextDelta("lo") + "data: [DONE]\n\n"

	client := xai.New(newSSEServer(t, sseBody), fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "grok-4", "")

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
	// The API streams a tool call's id/name once, then its arguments in
	// fragments — the client must reassemble them by index.
	sseBody := sseToolCallStart(0, "call_1", "get_weather", `{"city":`) +
		sseToolCallArgs(0, `"NYC"}`) +
		"data: [DONE]\n\n"

	client := xai.New(newSSEServer(t, sseBody), fakeCreds{key: "test-key"})
	session := chat.NewSession("s1", "grok-4", "")

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
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := xai.New(srv.URL, fakeCreds{key: "test-key"})
	drain(t, client, chat.NewSession("s1", "grok-4", ""))

	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
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
func drain(t *testing.T, client *xai.Client, session *chat.Session) []ports.StreamEvent {
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
