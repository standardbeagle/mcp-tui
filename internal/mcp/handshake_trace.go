package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

const (
	methodServerDiscover = "server/discover"
	methodInitialize     = "initialize"
)

// handshakeTrace records how the SDK negotiated the protocol version. The
// SDK sends server/discover (2026-07-28+) and initialize through the client's
// sending middleware, so a middleware sees every attempt: which version each
// one asked for, the versions the server advertised (in a DiscoverResult or
// an UnsupportedProtocolVersion error), and whether a failed discover fell
// back to initialize. One trace belongs to one client.
type handshakeTrace struct {
	mu                sync.Mutex
	discoverRequested []string
	serverSupported   []string
	discoverErr       string
	initializeSent    string
	// discoverPending is set when server/discover is sent and cleared when
	// the server answers it (result or error). It is read after a handshake
	// that ran out of time, which the session manager abandons without
	// waiting for the SDK call, so it must be set before the call returns.
	discoverPending bool
}

func newHandshakeTrace() *handshakeTrace { return &handshakeTrace{} }

// middleware is a sending middleware that records handshake traffic and logs
// each attempt at debug level. Other methods pass straight through.
func (h *handshakeTrace) middleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			switch method {
			case methodServerDiscover:
				requested := discoverRequestedVersion(req)
				h.mu.Lock()
				h.discoverPending = true
				h.mu.Unlock()
				res, err := next(ctx, method, req)
				h.recordDiscover(requested, res, err)
				return res, err
			case methodInitialize:
				requested := ""
				if p, ok := req.GetParams().(*officialMCP.InitializeParams); ok && p != nil {
					requested = p.ProtocolVersion
				}
				h.mu.Lock()
				h.initializeSent = requested
				h.mu.Unlock()
				debug.Debug("Handshake: initialize sent", debug.F("requested", requested))
				return next(ctx, method, req)
			default:
				return next(ctx, method, req)
			}
		}
	}
}

func (h *handshakeTrace) recordDiscover(requested string, res officialMCP.Result, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.discoverRequested = append(h.discoverRequested, requested)
	h.discoverPending = errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
	if dr, ok := res.(*officialMCP.DiscoverResult); ok && err == nil {
		h.serverSupported = dr.SupportedVersions
		debug.Debug("Handshake: server/discover answered",
			debug.F("requested", requested),
			debug.F("server_supported_versions", dr.SupportedVersions))
		return
	}
	h.discoverErr = errorText(err)
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) && rpcErr.Code == officialMCP.CodeUnsupportedProtocolVersion && len(rpcErr.Data) > 0 {
		var data officialMCP.UnsupportedProtocolVersionData
		if json.Unmarshal(rpcErr.Data, &data) == nil {
			h.serverSupported = data.Supported
		}
	}
	debug.Debug("Handshake: server/discover rejected",
		debug.F("requested", requested),
		debug.F("error", h.discoverErr),
		debug.F("server_supported_versions", h.serverSupported))
}

// logResult writes the one-line negotiation summary once Connect finished.
// requested is what mcp-tui asked the SDK for (the pin, or the SDK latest).
func (h *handshakeTrace) logResult(requested string, res *officialMCP.InitializeResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	handshake := methodServerDiscover
	if h.initializeSent != "" {
		handshake = methodInitialize
	}
	negotiated := ""
	if res != nil {
		negotiated = res.ProtocolVersion
	}
	fields := []debug.Field{
		debug.F("requested", requested),
		debug.F("negotiated", negotiated),
		debug.F("handshake", handshake),
		debug.F("fell_back_to_initialize", len(h.discoverRequested) > 0 && h.initializeSent != ""),
		debug.F("discover_attempts", h.discoverRequested),
		debug.F("server_supported_versions", h.serverSupported),
	}
	if h.discoverErr != "" {
		fields = append(fields, debug.F("discover_error", h.discoverErr))
	}
	debug.Info("Protocol version negotiated", fields...)
}

// discoverWentUnanswered reports whether the server never answered a
// server/discover before the caller gave up.
func (h *handshakeTrace) discoverWentUnanswered() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.discoverPending
}

// discoverUnansweredError reports a stdio server that did not answer
// server/discover, the first request of the 2026-07-28 handshake, before the
// connect deadline. A running server that ignores methods it does not know
// (instead of answering -32601, which makes the SDK fall back to initialize
// at once) stalls every connection this way; so does a server still starting
// (a cold npx or pwsh start), and from the client the two look the same. The
// cause (the deadline) stays reachable through Unwrap.
type discoverUnansweredError struct {
	cause error
}

func (e *discoverUnansweredError) Error() string {
	return "the server did not answer server/discover, the first request of the MCP 2026-07-28 handshake, " +
		"before the connection deadline. Either it ignores methods it does not know (it must answer them " +
		"with error -32601 so the client can fall back to initialize), or it had not finished starting.\n\n" +
		"Suggestion: pin --protocol-version 2025-11-25 to start with initialize; if the server is only slow " +
		"to start (a cold npx download, for one), raise --timeout"
}

func (e *discoverUnansweredError) Unwrap() error { return e.cause }

func discoverRequestedVersion(req officialMCP.Request) string {
	p, ok := req.GetParams().(*officialMCP.DiscoverParams)
	if !ok || p == nil {
		return ""
	}
	v, ok := p.Meta[officialMCP.MetaKeyProtocolVersion].(string)
	if !ok {
		return ""
	}
	return v
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
