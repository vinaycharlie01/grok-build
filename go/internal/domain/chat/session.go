package chat

// Session is the ordered conversation history for one agent run. It is a
// plain aggregate — mutation happens only through its own methods so the
// invariant "tool messages must reference a preceding tool call" can be
// enforced in one place.
type Session struct {
	ID       string
	Model    string
	Messages []Message
}

// NewSession starts an empty conversation targeting the given model, seeded
// with an optional system prompt.
func NewSession(id, model, systemPrompt string) *Session {
	s := &Session{ID: id, Model: model}
	if systemPrompt != "" {
		s.Messages = append(s.Messages, Message{Role: RoleSystem, Content: systemPrompt})
	}
	return s
}

// Append adds a message to the session history.
func (s *Session) Append(m Message) {
	s.Messages = append(s.Messages, m)
}

// PendingToolCalls returns the tool calls requested by the most recent
// assistant message, or nil if the last turn did not request tool use.
func (s *Session) PendingToolCalls() []ToolCall {
	if len(s.Messages) == 0 {
		return nil
	}
	last := s.Messages[len(s.Messages)-1]
	if last.Role != RoleAssistant {
		return nil
	}
	return last.ToolCalls
}
