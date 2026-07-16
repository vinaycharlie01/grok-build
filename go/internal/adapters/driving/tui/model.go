// Package tui is the driving adapter that renders and drives
// chatservice.Service through a Bubble Tea full-screen terminal UI — the Go
// analogue of the Rust xai-grok-pager crate for this vertical slice.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

var (
	userStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	assistantStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	toolStyle      = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245"))
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// Model is the Bubble Tea model for the chat TUI.
type Model struct {
	ctx     context.Context
	svc     *chatservice.Service
	session *chat.Session

	input    textinput.Model
	viewport viewport.Model
	spinner  spinner.Model

	transcript strings.Builder // rendered history, fed into the viewport
	streaming  strings.Builder // in-flight assistant text for the current turn

	events  <-chan ports.StreamEvent
	waiting bool
	ready   bool
	err     error
}

// New builds a chat TUI Model wired to svc, operating on session. ctx
// bounds the lifetime of every model turn started from the TUI (e.g.
// cancelled on process shutdown).
func New(ctx context.Context, svc *chatservice.Service, session *chat.Session) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask grok..."
	ti.Focus()
	ti.CharLimit = 4000

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		ctx:      ctx,
		svc:      svc,
		session:  session,
		input:    ti,
		spinner:  sp,
		viewport: viewport.New(80, 20),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Run starts the Bubble Tea program in the full-screen alt-screen buffer.
func Run(ctx context.Context, svc *chatservice.Service, session *chat.Session) error {
	p := tea.NewProgram(New(ctx, svc, session), tea.WithContext(ctx), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *Model) appendTranscript(s string) {
	m.transcript.WriteString(s)
	m.viewport.SetContent(m.transcript.String())
	m.viewport.GotoBottom()
}

func renderToolCall(tc chat.ToolCall) string {
	return toolStyle.Render(fmt.Sprintf("→ calling tool %s(%s)", tc.Name, tc.Arguments)) + "\n"
}
