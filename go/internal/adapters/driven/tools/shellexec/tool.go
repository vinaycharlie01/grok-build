// Package shellexec implements ports.Tool as an agent capability that runs
// a shell command on the user's behalf and reports its output back to the
// model — the Go equivalent of one of the Rust xai-grok-tools terminal
// tools.
//
// This is a product feature of the agent, not build tooling for this
// repository: grok-build's own build/test/lint pipeline goes exclusively
// through nava/Mage (see go/magefile.go) and contains no .sh files. An
// agent that cannot run shell commands on request isn't a coding agent.
package shellexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Name is the tool identifier advertised to the model.
const Name = "shell_exec"

const (
	defaultTimeout = 30 * time.Second
	maxOutputBytes = 32 * 1024
)

// Tool runs shell commands via /bin/sh -c.
type Tool struct {
	timeout time.Duration
}

// New builds a shellexec.Tool with the default timeout.
func New() *Tool {
	return &Tool{timeout: defaultTimeout}
}

type args struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

// Name implements ports.Tool.
func (t *Tool) Name() string { return Name }

// Description implements ports.Tool.
func (t *Tool) Description() string {
	return "Executes a shell command and returns its combined stdout/stderr output."
}

// JSONSchema implements ports.Tool.
func (t *Tool) JSONSchema() string {
	return `{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The shell command to run."},
    "cwd": {"type": "string", "description": "Working directory, defaults to the agent's current directory."}
  },
  "required": ["command"]
}`
}

// Execute implements ports.Tool.
func (t *Tool) Execute(ctx context.Context, argumentsJSON string) (string, error) {
	var a args
	if err := json.Unmarshal([]byte(argumentsJSON), &a); err != nil {
		return "", fmt.Errorf("shellexec: parse arguments: %w", err)
	}
	if a.Command == "" {
		return "", fmt.Errorf("shellexec: command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", a.Command)
	if a.Cwd != "" {
		cmd.Dir = a.Cwd
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()

	out := buf.Bytes()
	truncated := false
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes]
		truncated = true
	}

	result := string(out)
	if truncated {
		result += "\n...[output truncated]"
	}

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return result, nil
	case errors.As(runErr, &exitErr):
		// A non-zero exit is a normal outcome the model should reason
		// about (e.g. grep with no matches) — report it in the result
		// text rather than as a tool-execution error.
		return fmt.Sprintf("%s\n[exit status %d]", result, exitErr.ExitCode()), nil
	default:
		// Setup/launch failure (e.g. context deadline exceeded).
		return result, fmt.Errorf("shellexec: %w", runErr)
	}
}
