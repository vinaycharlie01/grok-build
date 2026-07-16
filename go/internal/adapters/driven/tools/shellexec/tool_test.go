package shellexec_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/shellexec"
)

func TestExecuteCapturesStdout(t *testing.T) {
	tool := shellexec.New()
	out, err := tool.Execute(context.Background(), `{"command":"echo hello"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("Execute() output = %q, want it to contain %q", out, "hello")
	}
}

func TestExecuteNonZeroExitIsNotAGoError(t *testing.T) {
	tool := shellexec.New()
	out, err := tool.Execute(context.Background(), `{"command":"exit 3"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil (non-zero exit is reported in output, not as an error)", err)
	}
	if !strings.Contains(out, "exit status 3") {
		t.Fatalf("Execute() output = %q, want it to mention the exit status", out)
	}
}

func TestExecuteRejectsMissingCommand(t *testing.T) {
	tool := shellexec.New()
	if _, err := tool.Execute(context.Background(), `{}`); err == nil {
		t.Fatal("Execute() error = nil, want error for missing command")
	}
}

func TestName(t *testing.T) {
	if shellexec.New().Name() != shellexec.Name {
		t.Fatalf("Name() mismatch with exported constant")
	}
}
