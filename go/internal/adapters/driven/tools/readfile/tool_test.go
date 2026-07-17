package readfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/readfile"
)

func TestExecuteReadsFileWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hi there"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := readfile.New(root)
	got, err := tool.Execute(context.Background(), `{"path":"hello.txt"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "hi there" {
		t.Fatalf("Execute() = %q, want %q", got, "hi there")
	}
}

func TestExecuteRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	tool := readfile.New(root)

	if _, err := tool.Execute(context.Background(), `{"path":"../../etc/passwd"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for path escaping the workspace root")
	}
}

func TestExecuteMissingFile(t *testing.T) {
	tool := readfile.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `{"path":"nope.txt"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for missing file")
	}
}
