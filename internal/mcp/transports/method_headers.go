package transports

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocol"
)

// methodHeadersRoundTripper wraps an http.RoundTripper and injects the
// SEP-2243 MCP-Method and MCP-Name headers on every JSON-RPC request.
//
// SEP-2243 (https://github.com/modelcontextprotocol/go-sdk/pull/907) lets load
// balancers, proxies, and observability tools route MCP traffic without
// peeking into the JSON body. The headers are advisory: the spec requires
// servers to ignore unknown ones, and the JSON-RPC envelope remains the
// authoritative source of method/name. Off by default — wired in only when the
// caller passes withMethodHeaders=true. It only acts before protocol
// 2026-07-28; from that version the SDK sends the standard headers.
//
// Header values:
//   - MCP-Method: the JSON-RPC method (e.g. "tools/call", "resources/read").
//   - MCP-Name:   the operation target name when the request carries one:
//     params.name for tools/call, prompts/get, resources/subscribe,
//     resources/unsubscribe, completion/complete; params.uri for
//     resources/read (the URI is the only stable identifier on a read).
//     Omitted when the request has no obvious target (initialize,
//     tools/list, resources/list, prompts/list, ping, notifications, etc.).
type methodHeadersRoundTripper struct {
	base http.RoundTripper
}

// newMethodHeadersRoundTripper wraps base with the SEP-2243 header injector.
// base must be non-nil; pass http.DefaultTransport if you have nothing better.
func newMethodHeadersRoundTripper(base http.RoundTripper) http.RoundTripper {
	return &methodHeadersRoundTripper{base: base}
}

// jsonRPCEnvelope is the minimum-viable shape we need from the request body to
// derive header values. Keeping it private and tiny keeps the parse cheap and
// avoids accidentally coupling to the SDK's internal request types.
type jsonRPCEnvelope struct {
	Method string `json:"method"`
	Params struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	} `json:"params"`
}

func (t *methodHeadersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only POST requests with a JSON body carry a JSON-RPC envelope. SSE GETs
	// (the standalone listening stream) and DELETE (session teardown) hit this
	// same RoundTripper and must pass through untouched.
	if req.Method != http.MethodPost || req.Body == nil {
		return t.base.RoundTrip(req)
	}
	// From 2026-07-28 the SDK sets the standard headers itself, following
	// rules this injector predates (none on notifications, Mcp-Name only
	// for tools/call, prompts/get, resources/read); leave them to it.
	if protocol.IsStateless(req.Header.Get("Mcp-Protocol-Version")) {
		return t.base.RoundTrip(req)
	}

	// Drain the body so we can parse it, then rewind it so the SDK transport
	// can still POST it to the server. http.Request.GetBody is set by
	// http.NewRequest for known body types, but we cannot rely on it in every
	// path (e.g. requests built directly with io.Pipe), so we do the buffer
	// dance ourselves.
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	// Restore GetBody so net/http's redirect/retry path can re-read the body.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}
	req.ContentLength = int64(len(bodyBytes))

	method, name := extractMethodAndName(bodyBytes)
	if method != "" {
		req.Header.Set("MCP-Method", method)
	}
	if name != "" {
		req.Header.Set("MCP-Name", name)
	}

	return t.base.RoundTrip(req)
}

// extractMethodAndName pulls the JSON-RPC method and target name from a
// request body. Returns ("", "") when the body is not a JSON-RPC request, when
// it is a JSON-RPC batch (array), or when the method has no name-shaped target.
//
// The caller passes the entire decoded body so we only do one pass over it.
func extractMethodAndName(body []byte) (method, name string) {
	// Cheap pre-check: a JSON-RPC request envelope is always an object. Skip
	// arrays (batches) and empty bodies without spinning up the decoder.
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", ""
	}

	var env jsonRPCEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ""
	}
	if env.Method == "" {
		return "", ""
	}

	switch env.Method {
	case "resources/read":
		// resources/read identifies the target by URI rather than name, and
		// the URI is the only stable identifier the proxy could route on.
		return env.Method, env.Params.URI
	default:
		// tools/call, prompts/get, resources/subscribe, resources/unsubscribe,
		// completion/complete all carry params.name. List/initialize/ping and
		// notifications have no name and Params.Name decodes as "" — that's
		// the "method only" case the caller wants.
		return env.Method, env.Params.Name
	}
}

// GetHTTPClientForTransportWithMethodHeaders is a thin variant of
// GetHTTPClientForTransport that optionally wraps the client's transport with
// the SEP-2243 method-headers injector. We add a sibling rather than mutating
// the existing helper so callers that don't opt in see no behavior change.
func GetHTTPClientForTransportWithMethodHeaders(transportType TransportType, customClient *http.Client, methodHeaders bool) *http.Client {
	return GetHTTPClientForTransportFull(transportType, customClient, methodHeaders, nil)
}

// HTTPTraceComponent is the debug component of the MCP transport's HTTP
// trace; debug.ObserveHTTPExchanges(HTTPTraceComponent, ...) sees every
// exchange with the headers actually sent.
const HTTPTraceComponent = "mcp-http"

// GetHTTPClientForTransportFull builds the HTTP client used by the SDK
// transport, layering wrappers in a deterministic order:
//
//  1. base transport from GetHTTPClientForTransport (timeout/keepalive policy)
//  2. HTTP trace (one redacted debug line per exchange, HTTPTraceComponent,
//     and the exchange observer the debug HTTP pane reads)
//  3. static headers from --header KEY=VALUE (additive merge)
//  4. SEP-2243 method headers injector
//
// The order matters: SEP-2243 runs last so its MCP-Method/MCP-Name headers
// are not stomped by a user-supplied --header MCP-Method=... entry, and the
// trace wraps the base so it sees the final outbound headers (the SDK's own
// Mcp-* headers included) and the unmodified server response. Tracing is
// always layered in: the custom transport bypasses http.DefaultTransport, so
// nothing else would see this traffic.
func GetHTTPClientForTransportFull(transportType TransportType, customClient *http.Client, methodHeaders bool, staticHeaders map[string]string) *http.Client {
	client := GetHTTPClientForTransport(transportType, customClient)

	// Clone so we don't mutate the shared default client.
	wrapped := *client
	base := wrapped.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	// Layer 1 (innermost): HTTP trace — times the real network exchange and
	// reports the headers actually sent.
	base = debug.NewHTTPTraceTransport(base, HTTPTraceComponent)

	// Layer 2: static headers — applied before SEP-2243 so a user-supplied
	// MCP-Method cannot override the injected one.
	if len(staticHeaders) > 0 {
		base = newStaticHeadersRoundTripper(base, staticHeaders)
	}

	// Layer 3 (outermost): SEP-2243 method headers — these depend on the
	// JSON-RPC body that the SDK has already serialized, so they go last.
	if methodHeaders {
		base = newMethodHeadersRoundTripper(base)
	}

	wrapped.Transport = base
	return &wrapped
}
