// Command grok is the composition root for the Go port of grok-build: it
// wires driven adapters (xAI LLM client, file-backed config, env-var
// credentials, shell/file tools) and a driving adapter (the Bubble Tea TUI)
// together through the chatservice application layer, without any of those
// pieces knowing about each other directly.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/config/file"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/xai"
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

	creds := env.New(env.DefaultVarName, os.LookupEnv)

	workspaceRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	llmClient := xai.New(cfg.BaseURL, creds)

	tools := []ports.Tool{
		shellexec.New(),
		readfile.New(workspaceRoot),
	}

	svc := chatservice.New(llmClient, tools)
	session := chat.NewSession("local", cfg.DefaultModel, cfg.SystemPrompt)

	return tui.Run(ctx, svc, session)
}
