package mcp

import (
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// nameProtocolError wraps err in a debug.MCPError named after the MCP error
// code its JSON-RPC error carries (debug.ProtocolErrorCode), so the CLI and
// TUI, which print err.Error(), show the name next to the server's message.
// Errors without a JSON-RPC error, or whose code MCP gives no meaning, come
// back unchanged. The original error stays reachable through Unwrap.
func nameProtocolError(err error, method string) error {
	var wire *jsonrpc.Error
	if !errors.As(err, &wire) {
		return err
	}
	code := debug.ProtocolErrorCode(wire.Code, method)
	if code == "" {
		return err
	}
	return debug.WrapError(err, code, fmt.Sprintf("JSON-RPC error %d from %s", wire.Code, method))
}
