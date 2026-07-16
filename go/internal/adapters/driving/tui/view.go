package tui

import "fmt"

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "initializing…"
	}

	status := "enter: send · esc/ctrl+c: quit"
	if m.waiting {
		status = m.spinner.View() + " thinking… · esc/ctrl+c: quit"
	}

	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		m.input.View(),
		footerStyle.Render(status),
	)
}
