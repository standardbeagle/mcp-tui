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
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
)

// nameProtocolError wraps err in a debug.MCPError named after the MCP error
// code its JSON-RPC error carries (debug.ProtocolErrorCode), so the CLI and
// TUI, which print err.Error(), show the name next to the server's message
// and nothing of the transport it arrived through.
// Errors without a JSON-RPC error, or whose code MCP gives no meaning, come
// back unchanged. The original error stays reachable through Unwrap.
func nameProtocolError(err error, method string) error {
	var wireCode int64
	var wireMessage string
	var wireData json.RawMessage
	var wire *jsonrpc.Error
	var taskWire *tasks.RPCError // the error of a request sent by the tasks link
	switch {
	case errors.As(err, &wire):
		wireCode, wireMessage, wireData = wire.Code, wire.Message, wire.Data
	case errors.As(err, &taskWire):
		wireCode, wireMessage, wireData = taskWire.Code, taskWire.Message, taskWire.Data
	default:
		return err
	}
	code := debug.ProtocolErrorCode(wireCode, method)
	if code == "" {
		return err
	}
	message := fmt.Sprintf("%s (JSON-RPC error %d from %s)", wireMessage, wireCode, method)
	switch code {
	case debug.ErrorCodeURLElicitationRequired:
		message += urlElicitationSteps(wireData)
	case debug.ErrorCodeMissingClientCapabilities:
		if requiresTasksExtension(wireData) {
			message += "; the server runs this only as a task: call it as a task (mcp-tui tool call --task)"
		}
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

// requiresTasksExtension reports whether a -32021 error's data names the
// tasks extension among the required capabilities.
func requiresTasksExtension(data json.RawMessage) bool {
	var payload struct {
		RequiredCapabilities struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"requiredCapabilities"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return false
	}
	_, ok := payload.RequiredCapabilities.Extensions[tasks.ExtensionID]
	return ok
}
