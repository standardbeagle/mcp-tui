package transports

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
)

// RequestHeaderObserver is invoked by the SEP-2243 method-headers RoundTripper
// after it injects MCP-Method/MCP-Name on a request. The mcp package
// registers an observer at startup so the values reach the debug HTTP tab
// even though our HTTP/SSE clients run on a custom http.Transport that the
// global debug RoundTripper does not see.
//
// The observer must not retain refs to req beyond the call — the body is
// already drained at this point and the headers map is the SDK's, not a copy.
type RequestHeaderObserver func(req *http.Request, mcpMethod, mcpName string)

var headerObserver atomic.Value // holds RequestHeaderObserver

// SetRequestHeaderObserver registers a hook that fires once per outgoing
// JSON-RPC request when SEP-2243 headers are injected. Pass nil to disable.
func SetRequestHeaderObserver(obs RequestHeaderObserver) {
	if obs == nil {
		// Clear by storing a typed nil so Load() still returns the right type.
		headerObserver.Store((RequestHeaderObserver)(nil))
		return
	}
	headerObserver.Store(obs)
}

func getRequestHeaderObserver() RequestHeaderObserver {
	v := headerObserver.Load()
	if v == nil {
		return nil
	}
	obs, _ := v.(RequestHeaderObserver)
	return obs
}

// methodHeadersRoundTripper wraps an http.RoundTripper and injects the
// SEP-2243 MCP-Method and MCP-Name headers on every JSON-RPC request.
//
// SEP-2243 (https://github.com/modelcontextprotocol/go-sdk/pull/907) lets load
// balancers, proxies, and observability tools route MCP traffic without
// peeking into the JSON body. The headers are advisory: the spec requires
// servers to ignore unknown ones, and the JSON-RPC envelope remains the
// authoritative source of method/name. Off by default — wired in only when the
// caller passes withMethodHeaders=true.
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

	// Notify the optional observer so the debug HTTP tab can surface the
	// injected headers even when the request goes through a custom transport
	// (which the global debug RoundTripper does not wrap).
	if obs := getRequestHeaderObserver(); obs != nil && method != "" {
		obs(req, method, name)
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
	client := GetHTTPClientForTransport(transportType, customClient)
	if !methodHeaders {
		return client
	}

	// Clone the client so we don't mutate one shared by other transports. The
	// transport itself is the only field we wrap.
	wrapped := *client
	base := wrapped.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = newMethodHeadersRoundTripper(base)
	return &wrapped
}
