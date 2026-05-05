package transports

import (
	"net/http"
	"sync/atomic"
)

// ResponseObserver is invoked for every HTTP response that round-trips through
// the wrapped transport, including responses to non-JSON-RPC GET/DELETE
// requests (SSE listening stream open, session teardown). The observer fires
// AFTER the response object is returned and BEFORE the body is read, so the
// callback must not consume resp.Body — it should snapshot resp.Header and
// optionally req.Header for the debug pane.
//
// The mcp package registers an observer at startup so the Ctrl+D HTTP tab can
// surface request/response headers per request. The wrapper wraps the SDK's
// custom http.Transport directly because the global debugRoundTripper only
// intercepts http.DefaultTransport, which the SDK does not use.
type ResponseObserver func(req *http.Request, resp *http.Response, err error)

var responseObserver atomic.Value // holds ResponseObserver

// SetResponseObserver registers the response-side hook. Pass nil to disable.
// Storing a typed-nil keeps Load() returning the right concrete type so the
// nil-check in getResponseObserver is uniform.
func SetResponseObserver(obs ResponseObserver) {
	if obs == nil {
		responseObserver.Store((ResponseObserver)(nil))
		return
	}
	responseObserver.Store(obs)
}

func getResponseObserver() ResponseObserver {
	v := responseObserver.Load()
	if v == nil {
		return nil
	}
	obs, _ := v.(ResponseObserver)
	return obs
}

// responseObserverRoundTripper notifies the registered ResponseObserver after
// every round-trip. Failures (resp == nil) are still reported so the debug
// pane can show "request failed before response" alongside the request
// headers we captured outbound.
type responseObserverRoundTripper struct {
	base http.RoundTripper
}

// newResponseObserverRoundTripper returns a wrapper that forwards to base
// while invoking the registered observer. We always wrap — even when no
// observer is currently set — so callers don't need to re-build the client
// when the observer is registered later.
func newResponseObserverRoundTripper(base http.RoundTripper) http.RoundTripper {
	return &responseObserverRoundTripper{base: base}
}

func (t *responseObserverRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if obs := getResponseObserver(); obs != nil {
		obs(req, resp, err)
	}
	return resp, err
}
