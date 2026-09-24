package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
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
	message := fmt.Sprintf("JSON-RPC error %d from %s", wire.Code, method)
	if code == debug.ErrorCodeURLElicitationRequired {
		message += urlElicitationSteps(wire.Data)
	}
	return debug.WrapError(err, code, message)
}

// urlElicitationSteps tells the user how to get past a -32042 (2025-11-25
// only): each URL the server listed in data.elicitations, then the next
// step. mcp-tui neither opens the URLs nor retries by itself, so without
// this the user sees only "URL elicitation required".
func urlElicitationSteps(data json.RawMessage) string {
	var payload struct {
		Elicitations []*officialMCP.ElicitParams `json:"elicitations"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || len(payload.Elicitations) == 0 {
		return "; the server listed no URL to open"
	}
	var b strings.Builder
	b.WriteString("; the server needs you to finish a step in a browser first:")
	for _, e := range payload.Elicitations {
		b.WriteString("\n")
		b.WriteString(elicitation.URLNotice(e.Message, e.URL))
	}
	b.WriteString("\nOpen the URL in a browser, finish there, then retry the call.")
	return b.String()
}
