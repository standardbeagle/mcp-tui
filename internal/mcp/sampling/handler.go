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

// WithToolsHandler is the optional richer interface implemented by handlers
// that can satisfy sampling/createMessage requests carrying server tools (the
// SDK v1.4.0+ CreateMessageWithTools variant). Handlers that implement this
// interface are registered with the SDK as CreateMessageWithToolsHandler;
// handlers that only implement Handler are registered with the simpler
// CreateMessageHandler.
//
// The two SDK handlers are mutually exclusive (the SDK panics if both are
// set), so choosing which one to register based on the handler type is the
// caller's responsibility — see service.createClient.
type WithToolsHandler interface {
	Handler
	// HandleCreateMessageWithTools handles a sampling/createMessage request
	// that carries the server's available tools and supports array-content
	// replies (text + parallel tool_use blocks). Implementations may return
	// either a text reply or one or more tool_use blocks; the server then
	// dispatches the tool calls and follows up with a tool_result message.
	HandleCreateMessageWithTools(ctx context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error)
}
