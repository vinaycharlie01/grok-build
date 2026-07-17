package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model.
//
// Every outer box below is sized with style.Width(m.width -
// style.GetHorizontalBorderSize()) — NOT GetHorizontalFrameSize() — because
// lipgloss.Style.Width already counts padding as part of its argument; only
// the border is added on top of it. Using GetHorizontalFrameSize() here
// (subtracting padding twice: once via this call, once again inside
// lipgloss) under-sizes every bordered box by its padding width. See
// applyLayout's doc comment for how this differs from sizing bubbles'
// internal content width.
func (m Model) View() string {
	if !m.ready {
		return "initializing…"
	}

	headerHeight, inputHeight, statusHeight := m.sectionHeights()
	bodyHeight := max(0, m.height-headerHeight-inputHeight-statusHeight)

	header := headerStyle.
		Width(m.width - headerStyle.GetHorizontalBorderSize()).
		Render(headerTitle(m.session.Model))

	body := viewportStyle.
		Width(m.width - viewportStyle.GetHorizontalBorderSize()).
		Height(bodyHeight - viewportStyle.GetVerticalBorderSize()).
		Render(m.viewport.View())

	inputBoxStyle := inputIdleStyle
	left := "enter send · esc/ctrl+c quit"
	if m.waiting {
		inputBoxStyle = inputWaitingStyle
		left = m.spinner.View() + " thinking…"
	}
	input := inputBoxStyle.
		Width(m.width - inputBoxStyle.GetHorizontalBorderSize()).
		Render(m.input.View())

	right := fmt.Sprintf("%d msgs", len(m.session.Messages))
	statusWidth := m.width - statusBarStyle.GetHorizontalFrameSize() // the text area, padding excluded
	footer := statusBarStyle.
		Width(m.width - statusBarStyle.GetHorizontalBorderSize()).
		Render(statusLine(left, right, statusWidth))

	return lipgloss.JoinVertical(lipgloss.Left, header, body, input, footer)
}

// statusLine right-aligns right within width, left-aligned on the left.
func statusLine(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
