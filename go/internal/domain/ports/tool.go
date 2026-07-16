package ports

import "context"

// Tool is the driven port for a single agent capability (shell execution,
// file read/edit, search, ...). It mirrors the ExecuteTool/ListTools shape
// of the Rust xai-grok-tools-api gRPC service, simplified to an in-process
// Go interface for this first slice — a future gRPC or MCP-backed adapter
// can implement the same port without touching the application layer.
type Tool interface {
	// Name is the identifier the model uses to request this tool.
	Name() string
	// Description is shown to the model to help it decide when to call this tool.
	Description() string
	// JSONSchema describes the tool's arguments as a JSON Schema object.
	JSONSchema() string
	// Execute runs the tool with JSON-encoded arguments and returns the
	// result text (or an error) to report back to the model.
	Execute(ctx context.Context, argumentsJSON string) (string, error)
}
