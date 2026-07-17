// Package writefile implements ports.Tool as a workspace-scoped
// create-or-overwrite file capability — the write-side counterpart to
// readfile, ported from the same Rust xai-grok-tools file-tool family
// (see ROADMAP.md's Phase 3 tool ecosystem parity list).
package writefile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Name is the tool identifier advertised to the model.
const Name = "write_file"

const filePerm = 0o600
const dirPerm = 0o700

// Tool creates or overwrites files rooted at a fixed workspace directory,
// refusing to escape it via ".." or an absolute path — the same guard as
// readfile.Tool.
type Tool struct {
	root string
}

// New builds a writefile.Tool scoped to root.
func New(root string) *Tool {
	return &Tool{root: root}
}

type args struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Name implements ports.Tool.
func (t *Tool) Name() string { return Name }

// Description implements ports.Tool.
func (t *Tool) Description() string {
	return "Creates or overwrites a text file in the workspace with the given content, creating parent directories as needed."
}

// JSONSchema implements ports.Tool.
func (t *Tool) JSONSchema() string {
	return `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the file, relative to the workspace root."},
    "content": {"type": "string", "description": "The full content to write to the file."}
  },
  "required": ["path", "content"]
}`
}

// Execute implements ports.Tool.
func (t *Tool) Execute(_ context.Context, argumentsJSON string) (string, error) {
	var a args
	if err := json.Unmarshal([]byte(argumentsJSON), &a); err != nil {
		return "", fmt.Errorf("writefile: parse arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("writefile: path is required")
	}

	resolved, err := t.resolve(a.Path)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), dirPerm); err != nil {
		return "", fmt.Errorf("writefile: create parent directories: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(a.Content), filePerm); err != nil {
		return "", fmt.Errorf("writefile: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
}

// resolve joins path onto the workspace root and rejects any result that
// escapes it — identical logic to readfile.Tool.resolve, duplicated
// rather than shared across two small, independent packages (see
// ROADMAP.md's tool ecosystem notes on why each tool stays a standalone
// ports.Tool implementation), plus one addition readfile doesn't need:
// an absolute path is rejected outright rather than silently re-rooted.
// filepath.Join("/workspace", "/etc/passwd") cleans to
// "/workspace/etc/passwd" — never actually escapes the root — but a model
// asking to write "/etc/passwd" and silently landing inside the workspace
// instead is a surprising, easy-to-misdiagnose outcome; an explicit error
// is clearer than a write that "succeeded" somewhere unexpected.
func (t *Tool) resolve(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("writefile: path %q must be relative to the workspace root, not absolute", path)
	}

	root, err := filepath.Abs(t.root)
	if err != nil {
		return "", fmt.Errorf("writefile: resolve workspace root: %w", err)
	}
	joined := filepath.Join(root, path)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("writefile: path %q escapes the workspace root", path)
	}
	return joined, nil
}
