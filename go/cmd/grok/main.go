// Command grok is the composition root for the Go port of grok-build: it
// wires driven adapters (an SDK-backed LLM client, file-backed config,
// env-var credentials, shell/file tools) and a driving adapter (the Bubble
// Tea TUI) together through the chatservice application layer, without any
// of those pieces knowing about each other directly.
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
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/shellexec"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driving/tui"
	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/chat"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "grok:", err)
		os.Exit(1)
	}
}

func run() error {
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

	choice, err := resolveProviderChoice(os.Getenv, cfg)
	if err != nil {
		return fmt.Errorf("resolve provider: %w", err)
	}
	creds := env.New(choice.credVar, os.LookupEnv)

	workspaceRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	// xAI, OpenAI, and any OpenAI-compatible endpoint all speak the same
	// chat-completions wire format, so those three GROK_PROVIDER choices
	// use the one SDK-backed openai.Client — see provider.go and
	// ROADMAP.md's "Library & framework choices" for why there's no
	// hand-rolled HTTP client for any provider in this tree. Anthropic's
	// Messages API is a different wire format, so it gets its own
	// SDK-backed client rather than being forced through the same one.
	var llmClient ports.LLMProvider
	switch choice.name {
	case "anthropic":
		llmClient = anthropic.New(choice.baseURL, creds)
	default:
		llmClient = openai.New(choice.baseURL, creds)
	}

	tools := []ports.Tool{
		shellexec.New(),
		readfile.New(workspaceRoot),
	}

	svc := chatservice.New(llmClient, tools)
	session := chat.NewSession("local", choice.model, cfg.SystemPrompt)

	return tui.Run(ctx, svc, session)
}
