package writefile_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/writefile"
)

func TestExecuteCreatesNewFileWithinRoot(t *testing.T) {
	root := t.TempDir()
	tool := writefile.New(root)

	got, err := tool.Execute(context.Background(), `{"path":"notes/hello.txt","content":"hi there"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got == "" {
		t.Fatal("Execute() = \"\", want a non-empty confirmation")
	}

	data, err := os.ReadFile(filepath.Join(root, "notes", "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v, want the file to have been created (including its parent dir)", err)
	}
	if string(data) != "hi there" {
		t.Fatalf("file content = %q, want %q", string(data), "hi there")
	}
}

func TestExecuteOverwritesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := writefile.New(root)
	if _, err := tool.Execute(context.Background(), `{"path":"existing.txt","content":"new content"}`); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Fatalf("file content = %q, want %q", string(data), "new content")
	}
}

func TestExecuteRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	tool := writefile.New(root)

	if _, err := tool.Execute(context.Background(), `{"path":"../escape.txt","content":"x"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for path escaping the workspace root")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("escape.txt was created outside the workspace root, want the write rejected before touching disk")
	}
}

func TestExecuteRejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	tool := writefile.New(root)

	if _, err := tool.Execute(context.Background(), `{"path":"/etc/passwd","content":"x"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for an absolute path")
	}
}

func TestExecuteRequiresPath(t *testing.T) {
	tool := writefile.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `{"content":"x"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for a missing path")
	}
}

func TestExecuteRejectsMalformedArguments(t *testing.T) {
	tool := writefile.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `not json`); err == nil {
		t.Fatal("Execute() error = nil, want error for malformed JSON arguments")
	}
}

func TestNameDescriptionSchema(t *testing.T) {
	tool := writefile.New(t.TempDir())

	if tool.Name() != writefile.Name {
		t.Fatalf("Name() = %q, want %q", tool.Name(), writefile.Name)
	}
	if tool.Description() == "" {
		t.Fatal("Description() is empty")
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(tool.JSONSchema()), &schema); err != nil {
		t.Fatalf("JSONSchema() is not valid JSON: %v", err)
	}
}
