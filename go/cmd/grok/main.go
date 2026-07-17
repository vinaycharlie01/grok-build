// Command grok is the composition root for the Go port of grok-build: it
// wires driven adapters (an SDK-backed LLM client, file-backed config,
// env-var credentials, shell/file tools) and a driving adapter (the Bubble
// Tea TUI) together through the chatservice application layer, without any
// of those pieces knowing about each other directly. See cli.go for the
// Cobra command tree (bare `grok` and `grok run` both call runInteractive
// below; `grok version` doesn't touch any of this).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/config/file"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/anthropic"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/providers/openai"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/readfile"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/search"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/shellexec"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/writefile"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driving/tui"
	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "grok:", err)
		os.Exit(1)
	}
}

// runInteractive wires every adapter together and launches the TUI. It's
// the composition root's actual entrypoint; cli.go's RunE funcs (bare
// `grok` and `grok run`) both call it directly, passing the --provider
// and --model flag values through.
func runInteractive(providerFlag, modelFlag string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfgPath, err := file.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	configStore := file.New(cfgPath)
	cfg, err := configStore.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providerName := resolveProviderName(providerFlag, os.Getenv)
	chosen, err := cfg.Provider(providerName)
	if err != nil {
		return fmt.Errorf("resolve provider: %w", err)
	}
	chosen.Model = resolveModelID(modelFlag, os.Getenv, chosen.Model)

	var creds ports.CredentialStore
	if chosen.APIKeyEnvVar == "" {
		creds = env.NoAuth{}
	} else {
		creds = env.New(chosen.APIKeyEnvVar, os.LookupEnv)
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	// Kind names a wire-format family, not a vendor: "openai" covers xAI,
	// OpenAI itself, and any OpenAI-compatible endpoint (OpenRouter, Groq,
	// a local Ollama/vLLM server, ...) via the one SDK-backed
	// openai.Client — see ROADMAP.md's "Library & framework choices" for
	// why there's no hand-rolled HTTP client for any provider in this
	// tree. "anthropic" is a genuinely different wire format, so it gets
	// its own SDK-backed client rather than being forced through the OpenAI one.
	var llmClient ports.LLMProvider
	switch chosen.Kind {
	case "anthropic":
		llmClient = anthropic.New(chosen.BaseURL, creds)
	default:
		llmClient = openai.New(chosen.BaseURL, creds)
	}

	tools := []ports.Tool{
		shellexec.New(),
		readfile.New(workspaceRoot),
		writefile.New(workspaceRoot),
		search.New(workspaceRoot),
	}

	svc := chatservice.New(llmClient, tools)
	session := chat.NewSession("local", chosen.Model, cfg.SystemPrompt)

	return tui.Run(ctx, svc, session)
}
