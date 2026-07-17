package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// streamEventMsg wraps one ports.StreamEvent delivered from the running turn.
type streamEventMsg struct{ event ports.StreamEvent }

// streamClosedMsg signals the events channel for the current turn closed.
type streamClosedMsg struct{}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.waiting || m.input.Value() == "" {
				return m, nil
			}
			return m.submit()
		}

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case streamClosedMsg:
		m.waiting = false
		return m, nil
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	if m.waiting {
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	text := m.input.Value()
	m.appendTranscript(userBadgeStyle.Render("YOU") + "  " + messageBodyStyle.Render(text) + "\n")
	m.input.Reset()
	m.waiting = true
	m.streaming.Reset()

	events := m.svc.Send(m.ctx, m.session, text)
	m.events = events

	return m, tea.Batch(spinner.Tick, waitForEvent(events))
}

func (m Model) handleStreamEvent(ev ports.StreamEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case ports.EventTextDelta:
		if m.streaming.Len() == 0 {
			m.appendTranscript(assistantBadgeStyle.Render("GROK") + "  ")
		}
		m.streaming.WriteString(ev.Text)
		m.appendTranscript(ev.Text)
	case ports.EventToolCall:
		m.appendTranscript("\n" + renderToolCall(ev.ToolCall))
	case ports.EventDone:
		m.appendDivider()
		m.streaming.Reset()
	case ports.EventError:
		m.err = ev.Err
		m.appendTranscript("\n" + renderError(ev.Err))
		m.appendDivider()
	}
	return m, waitForEvent(m.events)
}

// waitForEvent returns a tea.Cmd that blocks on the next event from a
// running turn, translating channel receive into the Bubble Tea Msg loop.
func waitForEvent(events <-chan ports.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return streamClosedMsg{}
		}
		return streamEventMsg{event: ev}
	}
}
