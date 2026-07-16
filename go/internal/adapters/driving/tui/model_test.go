package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// scriptedLLM is a minimal ports.LLMProvider fake: it replays a fixed
// event list, ignoring the session/tools passed to it.
type scriptedLLM struct{ events []ports.StreamEvent }

func (f scriptedLLM) StreamChat(context.Context, *chat.Session, []ports.ToolSpec) (<-chan ports.StreamEvent, error) {
	ch := make(chan ports.StreamEvent, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func newTestModel(events ...ports.StreamEvent) Model {
	svc := chatservice.New(scriptedLLM{events: events}, nil)
	session := chat.NewSession("s1", "grok-4", "")
	return New(context.Background(), svc, session)
}

func TestInitReturnsBlinkCmd(t *testing.T) {
	if newTestModel().Init() == nil {
		t.Fatal("Init() = nil, want a non-nil blink cmd")
	}
}

func TestViewBeforeReady(t *testing.T) {
	if got := newTestModel().View(); got != "initializing…" {
		t.Fatalf("View() = %q, want the pre-ready placeholder", got)
	}
}

func TestWindowSizeMsgMarksReady(t *testing.T) {
	m := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := updated.(Model)

	if !mm.ready {
		t.Fatal("want ready=true after a WindowSizeMsg")
	}
	if mm.viewport.Width != 80 {
		t.Fatalf("viewport.Width = %d, want 80", mm.viewport.Width)
	}
	if got := mm.View(); got == "initializing…" {
		t.Fatal("View() still shows the pre-ready placeholder after WindowSizeMsg")
	}
}

func TestCtrlCAndEscQuit(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyEsc} {
		m := newTestModel()
		_, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd == nil {
			t.Fatalf("key %v: want a non-nil quit cmd", key)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("key %v: want tea.QuitMsg, got %T", key, cmd())
		}
	}
}

func TestSubmitAppendsUserMessageAndSetsWaiting(t *testing.T) {
	m := newTestModel(ports.StreamEvent{Type: ports.EventDone})
	m.input.SetValue("hello there")

	updated, cmd := m.submit()
	mm := updated.(Model)

	if !mm.waiting {
		t.Fatal("want waiting=true immediately after submit")
	}
	if mm.input.Value() != "" {
		t.Fatalf("input.Value() = %q, want it reset", mm.input.Value())
	}
	if !strings.Contains(mm.transcript.String(), "hello there") {
		t.Fatalf("transcript = %q, want it to contain the submitted text", mm.transcript.String())
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd to kick off streaming")
	}
}

func TestEnterIsIgnoredWhileWaitingOrEmpty(t *testing.T) {
	m := newTestModel()
	m.waiting = true
	m.input.SetValue("should not send")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("want nil cmd when a turn is already in flight")
	}

	m.waiting = false
	m.input.SetValue("")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("want nil cmd for an empty prompt")
	}
}

func TestHandleStreamEventTextDelta(t *testing.T) {
	m := newTestModel()
	updated, cmd := m.handleStreamEvent(ports.StreamEvent{Type: ports.EventTextDelta, Text: "hello"})
	mm := updated.(Model)

	if !strings.Contains(mm.transcript.String(), "hello") {
		t.Fatalf("transcript = %q, want it to contain the streamed text", mm.transcript.String())
	}
	if mm.streaming.String() != "hello" {
		t.Fatalf("streaming buffer = %q, want %q", mm.streaming.String(), "hello")
	}
	if cmd == nil {
		t.Fatal("want a non-nil cmd to keep draining the event channel")
	}
}

func TestHandleStreamEventToolCall(t *testing.T) {
	m := newTestModel()
	tc := chat.ToolCall{ID: "1", Name: "shell_exec", Arguments: `{"command":"ls"}`}

	updated, _ := m.handleStreamEvent(ports.StreamEvent{Type: ports.EventToolCall, ToolCall: tc})
	mm := updated.(Model)

	if !strings.Contains(mm.transcript.String(), "shell_exec") {
		t.Fatalf("transcript = %q, want it to mention the tool call", mm.transcript.String())
	}
}

func TestHandleStreamEventDoneResetsStreamingBuffer(t *testing.T) {
	m := newTestModel()
	m.streaming.WriteString("partial")

	updated, _ := m.handleStreamEvent(ports.StreamEvent{Type: ports.EventDone})
	mm := updated.(Model)

	if mm.streaming.Len() != 0 {
		t.Fatalf("streaming buffer = %q, want it reset on EventDone", mm.streaming.String())
	}
}

func TestHandleStreamEventErrorRecordsAndRenders(t *testing.T) {
	m := newTestModel()
	wantErr := errors.New("boom")

	updated, _ := m.handleStreamEvent(ports.StreamEvent{Type: ports.EventError, Err: wantErr})
	mm := updated.(Model)

	if !errors.Is(mm.err, wantErr) {
		t.Fatalf("err = %v, want %v", mm.err, wantErr)
	}
	if !strings.Contains(mm.transcript.String(), "boom") {
		t.Fatalf("transcript = %q, want it to mention the error", mm.transcript.String())
	}
}

func TestStreamClosedMsgClearsWaiting(t *testing.T) {
	m := newTestModel()
	m.waiting = true

	updated, cmd := m.Update(streamClosedMsg{})
	mm := updated.(Model)

	if mm.waiting {
		t.Fatal("want waiting=false after streamClosedMsg")
	}
	if cmd != nil {
		t.Fatal("want a nil cmd on stream close")
	}
}

func TestWaitForEventTranslatesChannel(t *testing.T) {
	ch := make(chan ports.StreamEvent, 1)
	ch <- ports.StreamEvent{Type: ports.EventDone}

	msg := waitForEvent(ch)()
	ev, ok := msg.(streamEventMsg)
	if !ok {
		t.Fatalf("want streamEventMsg, got %T", msg)
	}
	if ev.event.Type != ports.EventDone {
		t.Fatalf("event.Type = %v, want EventDone", ev.event.Type)
	}

	close(ch)
	if _, ok := waitForEvent(ch)().(streamClosedMsg); !ok {
		t.Fatal("want streamClosedMsg once the channel is closed")
	}
}
