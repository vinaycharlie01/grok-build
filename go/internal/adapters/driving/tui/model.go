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

// Palette. Every color is an AdaptiveColor: lipgloss picks the Light or
// Dark value based on the terminal's reported background, and degrades
// automatically on terminals without truecolor support — so this looks
// right without any user configuration.
var (
	colorAccent    = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	colorAssistant = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E3B341"}
	colorTool      = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	colorError     = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#7D8590"}
	colorBorder    = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	colorFg        = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#C9D1D9"}
	colorBadgeText = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}
)

// Layout styles. Frame sizes (border + padding) are read back via
// GetHorizontalFrameSize()/GetVerticalFrameSize() wherever the layout is
// computed, so tweaking a style here can never desync it from the space
// reserved for it — see applyLayout.
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(colorBorder)

	viewportStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	inputIdleStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	inputWaitingStyle = inputIdleStyle.BorderForeground(colorMuted)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)
)

// Message/event styles.
var (
	userBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Background(colorAccent).
			Foreground(colorBadgeText)

	assistantBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Padding(0, 1).
				Background(colorAssistant).
				Foreground(colorBadgeText)

	messageBodyStyle = lipgloss.NewStyle().Foreground(colorFg)

	toolBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorTool).
			Foreground(colorTool).
			Padding(0, 1).
			MarginTop(1)

	errorBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorError).
			Foreground(colorError).
			Bold(true).
			Padding(0, 1).
			MarginTop(1)

	dividerStyle = lipgloss.NewStyle().Foreground(colorBorder)
)

// Model is the Bubble Tea model for the chat TUI.
//
// transcript/streaming are *strings.Builder, not strings.Builder — Bubble
// Tea's Update/View take and return Model by value on every single call,
// so the framework copies this struct constantly. strings.Builder embeds
// a self-referential pointer it uses to detect exactly that ("strings:
// illegal use of non-zero Builder copied by value") and panics the moment
// a copy gets written to after the original already was — which will
// eventually happen over a long-running session even though it isn't
// reliably reproducible in a quick synchronous test (it depends on actual
// memory addresses across the real async event loop's goroutines/GC
// activity, not just call order). A pointer field sidesteps the whole
// problem: copying Model copies the pointer, not the Builder it points
// to, so every copy still writes to the same underlying buffer safely.
// Never make either of these a value field again.
type Model struct {
	ctx     context.Context
	svc     *chatservice.Service
	session *chat.Session

	input    textinput.Model
	viewport viewport.Model
	spinner  spinner.Model

	transcript *strings.Builder // rendered history, fed into the viewport
	streaming  *strings.Builder // in-flight assistant text for the current turn

	events  <-chan ports.StreamEvent
	waiting bool
	ready   bool
	err     error

	width, height int
}

// New builds a chat TUI Model wired to svc, operating on session. ctx
// bounds the lifetime of every model turn started from the TUI (e.g.
// cancelled on process shutdown).
func New(ctx context.Context, svc *chatservice.Service, session *chat.Session) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask grok..."
	ti.Focus()
	ti.CharLimit = 4000
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)

	return Model{
		ctx:        ctx,
		svc:        svc,
		session:    session,
		input:      ti,
		spinner:    sp,
		viewport:   viewport.New(80, 20),
		transcript: &strings.Builder{},
		streaming:  &strings.Builder{},
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

// sectionHeights returns the rendered height of the header, input, and
// status sections at the model's current width. These are the same values
// applyLayout and View must agree on to avoid the body viewport either
// overflowing the terminal or leaving a gap.
func (m Model) sectionHeights() (header, input, status int) {
	header = lipgloss.Height(headerStyle.Render(headerTitle(m.session.Model)))
	input = inputIdleStyle.GetVerticalFrameSize() + 1  // 1 line of input text
	status = statusBarStyle.GetVerticalFrameSize() + 1 // 1 line of status text
	return header, input, status
}

// applyLayout recomputes viewport/input content dimensions from the
// current terminal size.
//
// lipgloss.Style.Width/Height set the size of the *padded* content box —
// padding is already included in the value you pass, and only the border
// is added on top (verified empirically: Style.Width(n) with 1-wide
// padding on each side renders n+2 wide with a rounded border, not
// n+2+2). So there are two different numbers in play, and conflating them
// is the classic bug here:
//   - the width bubbles' viewport/textinput should wrap *content* to is
//     desiredTotal - style.GetHorizontalFrameSize() (padding *and*
//     border subtracted) — that's what's computed below.
//   - the width to hand to the *outer* lipgloss box's .Width() call in
//     View() is desiredTotal - style.GetHorizontalBorderSize() (border
//     only) — computed separately in View(), not reused from here.
func (m *Model) applyLayout() {
	headerHeight, inputHeight, statusHeight := m.sectionHeights()

	m.viewport.Width = max(0, m.width-viewportStyle.GetHorizontalFrameSize())
	m.viewport.Height = max(0, m.height-headerHeight-inputHeight-statusHeight-viewportStyle.GetVerticalFrameSize())
	m.input.Width = max(0, m.width-inputIdleStyle.GetHorizontalFrameSize()-lipgloss.Width(m.input.Prompt))

	m.viewport.SetContent(m.transcript.String())
}

func (m *Model) appendTranscript(s string) {
	m.transcript.WriteString(s)
	m.viewport.SetContent(m.transcript.String())
	m.viewport.GotoBottom()
}

// appendDivider draws a full-width rule between turns so a long scrollback
// stays readable at a glance.
func (m *Model) appendDivider() {
	if m.viewport.Width <= 0 {
		return
	}
	m.appendTranscript("\n" + dividerStyle.Render(strings.Repeat("─", m.viewport.Width)) + "\n\n")
}

func headerTitle(model string) string {
	return fmt.Sprintf("grok  ·  %s", model)
}

func renderToolCall(tc chat.ToolCall) string {
	return toolBoxStyle.Render(fmt.Sprintf("🔧 %s %s", tc.Name, tc.Arguments)) + "\n"
}

func renderError(err error) string {
	return errorBoxStyle.Render("✖ "+err.Error()) + "\n"
}
