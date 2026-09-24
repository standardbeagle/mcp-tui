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

func discoverRequestedVersion(req officialMCP.Request) string {
	p, ok := req.GetParams().(*officialMCP.DiscoverParams)
	if !ok || p == nil {
		return ""
	}
	v, _ := p.Meta[officialMCP.MetaKeyProtocolVersion].(string)
	return v
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
