// Package readfile implements ports.Tool as a workspace-scoped file-read
// capability — the Go equivalent of one of the Rust xai-grok-tools file
// tools, simplified for this vertical slice.
package readfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Name is the tool identifier advertised to the model.
const Name = "read_file"

const maxReadBytes = 64 * 1024

// Tool reads files rooted at a fixed workspace directory, refusing to
// escape it via ".." or an absolute path.
type Tool struct {
	root string
}

// New builds a readfile.Tool scoped to root.
func New(root string) *Tool {
	return &Tool{root: root}
}

type args struct {
	Path string `json:"path"`
}

// Name implements ports.Tool.
func (t *Tool) Name() string { return Name }

// Description implements ports.Tool.
func (t *Tool) Description() string {
	return "Reads a text file from the workspace and returns its contents."
}

// JSONSchema implements ports.Tool.
func (t *Tool) JSONSchema() string {
	return `{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the file, relative to the workspace root."}
  },
  "required": ["path"]
}`
}

// Execute implements ports.Tool.
func (t *Tool) Execute(_ context.Context, argumentsJSON string) (string, error) {
	var a args
	if err := json.Unmarshal([]byte(argumentsJSON), &a); err != nil {
		return "", fmt.Errorf("readfile: parse arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("readfile: path is required")
	}

	resolved, err := t.resolve(a.Path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("readfile: %w", err)
	}
	if len(data) > maxReadBytes {
		return string(data[:maxReadBytes]) + "\n...[truncated]", nil
	}
	return string(data), nil
}

// resolve joins path onto the workspace root and rejects any result that
// escapes it.
func (t *Tool) resolve(path string) (string, error) {
	root, err := filepath.Abs(t.root)
	if err != nil {
		return "", fmt.Errorf("readfile: resolve workspace root: %w", err)
	}
	joined := filepath.Join(root, path)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("readfile: path %q escapes the workspace root", path)
	}
	return joined, nil
}
