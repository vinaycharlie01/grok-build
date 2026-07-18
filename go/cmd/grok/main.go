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
	"time"

	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/config/file"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/credentials/env"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/llm/resilience"
	sessionmongo "github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/sessionstore/mongo"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/readfile"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/search"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/shellexec"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driven/tools/writefile"
	"github.com/vinaycharlie01/grok-build/go/internal/adapters/driving/tui"
	"github.com/vinaycharlie01/grok-build/go/internal/application/chatservice"
	"github.com/vinaycharlie01/grok-build/go/internal/domain/ports"
)

// localSessionID is the fixed session identity used for the single
// interactive session cmd/grok drives today (no multi-session support
// yet — see ROADMAP.md's Phase 2 sessionmanager task). It's also the key
// a configured SessionStore loads/saves under, so an operator who enables
// sessionStore: in their config file resumes the same conversation across
// restarts rather than starting fresh each time.
const localSessionID = "local"

// Circuit breaker defaults for the selected LLM provider. Always on
// (unlike SessionStore, this changes nothing observable while the
// provider is healthy — CLOSED just passes every call through), matching
// ROADMAP.md's Phase 2 "resilience wrapping any ports.LLMProvider" task.
// These numbers follow OmniRoute's API-key-provider tier (see that
// project's CLAUDE.md "Resilience Runtime State" section) as a
// reasonable default for a cloud LLM API; not yet exposed as config —
// see ROADMAP.md if a provider needs its own threshold/timeout later.
const (
	circuitBreakerThreshold    = 5
	circuitBreakerResetTimeout = 30 * time.Second
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

	llmClient := resilience.Wrap(buildLLMClient(chosen.Kind, chosen.BaseURL, creds), circuitBreakerThreshold, circuitBreakerResetTimeout)

	tools := []ports.Tool{
		shellexec.New(),
		readfile.New(workspaceRoot),
		writefile.New(workspaceRoot),
		search.New(workspaceRoot),
	}

	svc := chatservice.New(llmClient, tools)

	// SessionStore is opt-in (see settings.Config.SessionStore's doc
	// comment): with no sessionStore: configured, store stays nil and
	// resolveSession/the deferred Save below behave exactly as before
	// this feature existed — a fresh in-memory session every run.
	var store ports.SessionStore
	if cfg.SessionStore != nil {
		switch cfg.SessionStore.Kind {
		case "mongo":
			mongoStore, err := sessionmongo.New(ctx, cfg.SessionStore.URI, cfg.SessionStore.Database, cfg.SessionStore.Collection)
			if err != nil {
				return fmt.Errorf("connect session store: %w", err)
			}
			defer mongoStore.Close(context.Background())
			store = mongoStore
		default:
			return fmt.Errorf("sessionStore.kind %q is not supported (want \"mongo\")", cfg.SessionStore.Kind)
		}
	}

	session, err := resolveSession(ctx, store, localSessionID, chosen.Model, cfg.SystemPrompt)
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	runErr := tui.Run(ctx, svc, session)

	if store != nil {
		// Save on a fresh context, not ctx: ctx is cancelled by the same
		// Ctrl-C/SIGTERM that ends tui.Run, and a cancelled context would
		// abort the save of exactly the session state we most want to
		// keep (whatever happened right up to the interrupt).
		if saveErr := store.Save(context.Background(), session); saveErr != nil {
			if runErr != nil {
				return fmt.Errorf("%w (additionally, save session: %v)", runErr, saveErr)
			}
			return fmt.Errorf("save session: %w", saveErr)
		}
	}

	return runErr
}
