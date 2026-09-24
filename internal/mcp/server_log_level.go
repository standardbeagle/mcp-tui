package mcp

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// How a client asks a server for log notifications changed in 2026-07-28
// (SEP-2575, SEP-2577):
//
//   - Before: one logging/setLevel request sets a session-wide level.
//   - From 2026-07-28: logging/setLevel is removed (the SDK server answers
//     "method not found"); each request carries the level it wants in
//     _meta["io.modelcontextprotocol/logLevel"], and ServerSession.Log only
//     sends a notification for a request whose _meta asked for that level or
//     lower. Logging as a whole is deprecated but remains functional for the
//     deprecation window.
//
// mcp-tui supports both: the level from --server-log-level is sent with
// logging/setLevel after a legacy handshake, and stamped into every
// request's _meta on 2026-07-28 by a sending middleware.

// mcpLoggingLevels are the syslog-style levels MCP defines, lowest first.
var mcpLoggingLevels = []string{"debug", "info", "notice", "warning", "error", "critical", "alert", "emergency"}

func validateServerLogLevel(level string) error {
	if level == "" || slices.Contains(mcpLoggingLevels, level) {
		return nil
	}
	return fmt.Errorf("unsupported server log level %q: valid levels are %s", level, strings.Join(mcpLoggingLevels, ", "))
}

// serverLogLevelMiddleware stamps the requested level into the _meta of
// every 2026-07-28 request. The SDK has already injected the SEP-2575 _meta
// triple by the time sending middleware runs, so a protocolVersion key marks
// a new-protocol request; legacy requests are left alone. A level the caller
// set explicitly on a request is kept.
func serverLogLevelMiddleware(level string) officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if params := req.GetParams(); !isNilParams(params) {
				if meta := params.GetMeta(); meta != nil {
					if _, newProtocol := meta[officialMCP.MetaKeyProtocolVersion]; newProtocol {
						if _, set := meta[officialMCP.MetaKeyLogLevel]; !set {
							meta[officialMCP.MetaKeyLogLevel] = level
						}
					}
				}
			}
			return next(ctx, method, req)
		}
	}
}

// isNilParams reports a nil interface or a typed-nil params pointer; legacy
// requests without params reach middleware as the latter, and GetMeta on it
// would dereference nil.
func isNilParams(params officialMCP.Params) bool {
	if params == nil {
		return true
	}
	v := reflect.ValueOf(params)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// applyServerLogLevel sets the session-wide level on a legacy session. On
// 2026-07-28 there is nothing to do here: the middleware carries the level.
// A server that rejects setLevel is logged, not fatal: the connection is
// usable, it just sends no logs.
func applyServerLogLevel(ctx context.Context, session *officialMCP.ClientSession, level string) {
	if level == "" || session == nil {
		return
	}
	res := session.InitializeResult()
	if res != nil && res.ProtocolVersion >= statelessProtocolVersion {
		debug.Info("Server log level requested per request", debug.F("level", level), debug.F("via", "_meta "+officialMCP.MetaKeyLogLevel))
		return
	}
	if res != nil && (res.Capabilities == nil || res.Capabilities.Logging == nil) {
		debug.Warn("Server does not advertise logging; not sending logging/setLevel", debug.F("level", level))
		return
	}
	if err := session.SetLoggingLevel(ctx, &officialMCP.SetLoggingLevelParams{Level: officialMCP.LoggingLevel(level)}); err != nil {
		debug.Warn("Server rejected logging/setLevel", debug.F("level", level), debug.F("error", err))
		return
	}
	debug.Info("Server log level set", debug.F("level", level), debug.F("via", "logging/setLevel"))
}
