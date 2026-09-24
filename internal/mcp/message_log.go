package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// messageLogMiddleware records every JSON-RPC message that passes it in the
// MCP Messages log (debug.GetMCPLogger), which the TUI's Messages tab and
// Events tab read: a request and its response or error, or a notification.
// sending is true on the client's sending side (client requests go out,
// responses come in) and false on its receiving side (server requests and
// notifications come in, the client's answers go out). The log redacts
// payloads where it stores them.
//
// The id is mcp-tui's own counter, not the wire id, which the SDK does not
// expose to middleware; it pairs a request with its response in the log.
func (s *service) messageLogMiddleware(sending bool) officialMCP.Middleware {
	logRequest, logReply := debug.LogMCPIncoming, debug.LogMCPOutgoing
	if sending {
		logRequest, logReply = debug.LogMCPOutgoing, debug.LogMCPIncoming
	}
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			msg := map[string]any{"jsonrpc": "2.0", "method": method}
			if params := req.GetParams(); params != nil {
				msg["params"] = params
			}
			if strings.HasPrefix(method, "notifications/") {
				logMessage(logRequest, msg)
				return next(ctx, method, req)
			}
			id := s.getNextRequestID()
			msg["id"] = id
			logMessage(logRequest, msg)

			result, err := next(ctx, method, req)
			reply := map[string]any{"jsonrpc": "2.0", "id": id}
			if err != nil {
				reply["error"] = map[string]any{"code": errorCode(err), "message": err.Error()}
			} else {
				reply["result"] = result
			}
			logMessage(logReply, reply)
			return result, err
		}
	}
}

// errorCode is the JSON-RPC code err carries, or -32603 (internal error)
// for an error that never crossed the wire.
func errorCode(err error) int64 {
	var wire *jsonrpc.Error
	if errors.As(err, &wire) {
		return wire.Code
	}
	return jsonrpc.CodeInternalError
}

// logMessage marshals msg and hands it to log. A message that cannot be
// marshalled is reported in the debug log instead of silently missing from
// the Messages tab.
func logMessage(log func(rawMessage string, parsedMessage interface{}), msg map[string]any) {
	raw, err := json.Marshal(msg)
	if err != nil {
		debug.Warn("MCP message not logged: cannot marshal", debug.F("method", msg["method"]), debug.F("error", err))
		return
	}
	log(string(raw), nil)
}
