package search_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/search"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteFindsMatchingLinesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\nfunc Foo() {}\n")
	writeFile(t, root, "sub/b.go", "package sub\nfunc Bar() { Foo() }\n")
	writeFile(t, root, "c.txt", "no matches in here\n")

	tool := search.New(root)
	got, err := tool.Execute(context.Background(), `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"a.go:2", "sub/b.go:2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("results = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "c.txt") {
		t.Fatalf("results = %q, want no match from c.txt", got)
	}
}

func TestExecuteFiltersByGlob(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "TODO: fix this\n")
	writeFile(t, root, "b.md", "TODO: fix this too\n")

	tool := search.New(root)
	got, err := tool.Execute(context.Background(), `{"pattern":"TODO","glob":"*.go"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got, "a.go") {
		t.Fatalf("results = %q, want a.go to match", got)
	}
	if strings.Contains(got, "b.md") {
		t.Fatalf("results = %q, want b.md excluded by the glob", got)
	}
}

func TestExecuteSupportsRegexPatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func handleFoo() {}\nfunc handleBar() {}\nfunc other() {}\n")

	tool := search.New(root)
	got, err := tool.Execute(context.Background(), `{"pattern":"^func handle\\w+"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got, "handleFoo") || !strings.Contains(got, "handleBar") {
		t.Fatalf("results = %q, want both handleFoo and handleBar", got)
	}
	if strings.Contains(got, "other") {
		t.Fatalf("results = %q, want other() excluded", got)
	}
}

func TestExecuteNoMatchesReturnsEmptyResultNotError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "nothing interesting here\n")

	tool := search.New(root)
	got, err := tool.Execute(context.Background(), `{"pattern":"NOTPRESENT"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v, want no error for zero matches", err)
	}
	if strings.Contains(got, "a.go") {
		t.Fatalf("results = %q, want no match", got)
	}
}

func TestExecuteRejectsInvalidRegex(t *testing.T) {
	tool := search.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `{"pattern":"("}`); err == nil {
		t.Fatal("Execute() error = nil, want error for an invalid regex pattern")
	}
}

func TestExecuteRequiresPattern(t *testing.T) {
	tool := search.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `{}`); err == nil {
		t.Fatal("Execute() error = nil, want error for a missing pattern")
	}
}

func TestExecuteRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	tool := search.New(root)

	if _, err := tool.Execute(context.Background(), `{"pattern":"x","path":"../"}`); err == nil {
		t.Fatal("Execute() error = nil, want error for a search path escaping the workspace root")
	}
}

func TestExecuteRejectsMalformedArguments(t *testing.T) {
	tool := search.New(t.TempDir())
	if _, err := tool.Execute(context.Background(), `not json`); err == nil {
		t.Fatal("Execute() error = nil, want error for malformed JSON arguments")
	}
}

func TestNameDescriptionSchema(t *testing.T) {
	tool := search.New(t.TempDir())

	if tool.Name() != search.Name {
		t.Fatalf("Name() = %q, want %q", tool.Name(), search.Name)
	}
	if tool.Description() == "" {
		t.Fatal("Description() is empty")
	}
	if tool.JSONSchema() == "" {
		t.Fatal("JSONSchema() is empty")
	}
}
