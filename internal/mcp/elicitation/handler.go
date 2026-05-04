// Package elicitation provides client-side handlers for MCP elicitation/create
// requests. Servers performing structured-input flows (configuration forms,
// confirmation dialogs, multi-step wizards) request user input via JSON
// Schema; this package exposes pluggable handlers so both the TUI and the CLI
// can satisfy those requests in their own way.
//
// The package mirrors the design of the sibling `sampling` package: a Handler
// interface, a TUIHandler that bridges the SDK goroutine to a bubbletea
// program via a PendingRequest type, and stub handlers for non-interactive
// CLI runs. The TUI form renderer lives in internal/tui/screens.
package elicitation

import (
	"context"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler is invoked when an MCP server sends an elicitation/create request.
// Implementations decide how to produce the reply: ask the user via the TUI,
// return a fixed stubbed response in CLI mode, or anything else.
//
// The returned result is sent back to the server verbatim. Returning a non-nil
// error causes the SDK to forward a JSON-RPC error to the server, which is
// the correct behavior when no handler is configured.
//
// Per the MCP elicitation spec, well-behaved handlers should set Action to
// one of "accept", "decline", or "cancel" and only populate Content when
// Action is "accept" — this matches the SDK's validation behavior.
type Handler interface {
	// HandleElicit handles an elicitation/create request.
	HandleElicit(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error)
}

// HandlerFunc is an adapter that lets ordinary functions satisfy Handler.
type HandlerFunc func(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error)

// HandleElicit calls the underlying function.
func (f HandlerFunc) HandleElicit(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
	return f(ctx, req)
}
