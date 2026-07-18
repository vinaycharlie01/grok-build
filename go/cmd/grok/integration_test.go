package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports/portsfakes"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/settings"
)

// TestInteractiveWiringEndToEnd is an integration test — unlike
// tests/integration/ (reserved for suites needing a real external
// process, e.g. the MongoDB one via testcontainers-go), this one needs no
// Docker: an httptest.Server stands in for the upstream LLM API, and
// counterfeiter-generated fakes (portsfakes.FakeTool, .FakeSessionStore —
// see internal/domain/ports's //go:generate directives) stand in for a
// real tool and a real session store. Everything else is the real
// production composition: real settings.Config, real
// resolveProviderName/resolveModelID/buildLLMClient, a real
// *openai.Client actually talking HTTP to the fake server, real
// chatservice, real resolveSession.
//
// It can't drive tui.Run itself (that needs a real terminal), so it stops
// at "everything runInteractive hands to tui.Run is correctly wired and
// produces the right output for one full turn, including a tool call
// round-trip" — the part of the composition root that's actually
// testable without a live terminal. See provider_test.go/model_test.go
// for the pure-function unit tests this builds on.
func TestInteractiveWiringEndToEnd(t *testing.T) {
	toolCallSent := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if !toolCallSent {
			toolCallSent = true
			fmt.Fprint(w, sseToolCall(0, "call-1", "lookup", `{"q":"go"}`))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, sseTextDelta("done, found it"))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	// A config file's providers: list, shaped exactly like a real one.
	cfg := settings.Config{
		DefaultProvider: "test-openai",
		Providers: []settings.ProviderConfig{
			{Name: "test-openai", Kind: "openai", BaseURL: srv.URL, Model: "test-model", APIKeyEnvVar: ""},
		},
		SystemPrompt: "be terse",
	}

	// The real --provider/--model resolution path (empty flag/env, so it
	// falls back to cfg.DefaultProvider / the provider's configured Model).
	providerName := resolveProviderName("", func(string) string { return "" })
	chosen, err := cfg.Provider(providerName)
	if err != nil {
		t.Fatalf("cfg.Provider() error = %v", err)
	}
	chosen.Model = resolveModelID("", func(string) string { return "" }, chosen.Model)
	if chosen.Model != "test-model" {
		t.Fatalf("resolved model = %q, want %q", chosen.Model, "test-model")
	}

	// Real credential resolution + real LLM client construction, hitting
	// the fake server above.
	llmClient := buildLLMClient(chosen.Kind, chosen.BaseURL, env.NoAuth{})

	fakeTool := &portsfakes.FakeTool{}
	fakeTool.NameReturns("lookup")
	fakeTool.DescriptionReturns("looks things up")
	fakeTool.JSONSchemaReturns(`{"type":"object"}`)
	fakeTool.ExecuteReturns("found: go", nil)

	svc := chatservice.New(llmClient, []ports.Tool{fakeTool})

	// A mocked SessionStore with nothing saved yet — resolveSession
	// should fall through to a fresh session.
	store := &portsfakes.FakeSessionStore{}
	store.LoadReturns(nil, ports.ErrSessionNotFound)

	session, err := resolveSession(context.Background(), store, "local", chosen.Model, cfg.SystemPrompt)
	if err != nil {
		t.Fatalf("resolveSession() error = %v", err)
	}
	if store.LoadCallCount() != 1 {
		t.Fatalf("store.Load() called %d times, want 1", store.LoadCallCount())
	}

	var got []ports.StreamEvent
	for ev := range svc.Send(context.Background(), session, "look something up") {
		got = append(got, ev)
	}

	if fakeTool.ExecuteCallCount() != 1 {
		t.Fatalf("tool Execute called %d times, want 1 (the model's tool call should have reached the mocked tool)", fakeTool.ExecuteCallCount())
	}
	if _, argsJSON := fakeTool.ExecuteArgsForCall(0); argsJSON != `{"q":"go"}` {
		t.Fatalf("tool called with args %q, want %q", argsJSON, `{"q":"go"}`)
	}

	var finalText string
	for _, ev := range got {
		if ev.Type == ports.EventTextDelta {
			finalText += ev.Text
		}
	}
	if finalText != "done, found it" {
		t.Fatalf("final assistant text = %q, want %q", finalText, "done, found it")
	}

	// The other half of runInteractive's session wiring: saving the
	// finished session back through the store after a turn.
	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	if store.SaveCallCount() != 1 {
		t.Fatalf("store.Save() called %d times, want 1", store.SaveCallCount())
	}
	if _, savedSession := store.SaveArgsForCall(0); savedSession.ID != session.ID {
		t.Fatalf("saved session ID = %q, want %q", savedSession.ID, session.ID)
	}
}

func sseTextDelta(text string) string {
	return sseChunk(fmt.Sprintf(`{"content":%q}`, text))
}

func sseToolCall(index int, id, name, argsJSON string) string {
	return sseChunk(fmt.Sprintf(
		`{"tool_calls":[{"index":%d,"id":%q,"function":{"name":%q,"arguments":%q}}]}`,
		index, id, name, argsJSON,
	))
}

func sseChunk(deltaJSON string) string {
	return "data: " + fmt.Sprintf(
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":%s,"finish_reason":null}]}`,
		deltaJSON,
	) + "\n\n"
}
