// Package sampling provides client-side handlers for MCP sampling/createMessage
// requests. Servers performing agentic work request LLM sampling from the
// client; this package exposes pluggable handlers so both the TUI and the CLI
// can satisfy those requests in their own way.
package sampling

import (
	"context"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler is invoked when an MCP server sends a sampling/createMessage request.
// Implementations decide how to produce the reply: ask the user via the TUI,
// return a fixed stubbed response in CLI mode, or anything else.
//
// The returned result is sent back to the server verbatim. Returning a non-nil
// error causes the SDK to forward a JSON-RPC error to the server, which is the
// correct behavior for "user aborted" or "no handler configured" cases.
type Handler interface {
	// HandleCreateMessage handles a sampling/createMessage request.
	HandleCreateMessage(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error)
}

// HandlerFunc is an adapter that lets ordinary functions satisfy Handler.
type HandlerFunc func(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error)

// HandleCreateMessage calls the underlying function.
func (f HandlerFunc) HandleCreateMessage(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
	return f(ctx, req)
}
