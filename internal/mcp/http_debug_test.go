package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoreHTTPDebugging returns the process-global transport to its original
// state, whatever the test did to it.
func restoreHTTPDebugging(t *testing.T) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() {
		httpDebugMu.Lock()
		defer httpDebugMu.Unlock()
		http.DefaultTransport = original
		originalTransport = nil
	})
	EnableHTTPDebugging(false) // start from a known state
}

// Enabling debugging must be reversible. It used to early-return on false, so
// once enabled it could never be turned off.
func TestEnableHTTPDebuggingIsReversible(t *testing.T) {
	restoreHTTPDebugging(t)

	base := http.DefaultTransport

	EnableHTTPDebugging(true)
	assert.NotSame(t, base, http.DefaultTransport, "enabling must wrap the transport")
	_, wrapped := http.DefaultTransport.(*debugRoundTripper)
	assert.True(t, wrapped, "the wrapped transport must be the debug round-tripper")

	EnableHTTPDebugging(false)
	assert.Same(t, base, http.DefaultTransport, "disabling must restore the original transport")
}

// Enabling twice must not nest round-trippers: each layer buffers every
// response body, so N services used to mean N nested readers.
func TestEnableHTTPDebuggingDoesNotNest(t *testing.T) {
	restoreHTTPDebugging(t)

	base := http.DefaultTransport

	EnableHTTPDebugging(true)
	first := http.DefaultTransport

	EnableHTTPDebugging(true)
	EnableHTTPDebugging(true)
	assert.Same(t, first, http.DefaultTransport, "re-enabling must be idempotent")

	rt, ok := first.(*debugRoundTripper)
	require.True(t, ok)
	assert.Same(t, base, rt.base, "the debug round-tripper must wrap the original, not another wrapper")

	EnableHTTPDebugging(false)
	assert.Same(t, base, http.DefaultTransport)
}

// Disabling when never enabled must be a harmless no-op.
func TestEnableHTTPDebuggingDisableWithoutEnable(t *testing.T) {
	restoreHTTPDebugging(t)

	base := http.DefaultTransport
	EnableHTTPDebugging(false)
	assert.Same(t, base, http.DefaultTransport)
}

// The debug round-tripper buffers response bodies so it can inspect them. It
// must never do that for a stream: an SSE body has no EOF, so io.ReadAll would
// block forever and grow without bound.
func TestDebugRoundTripperDoesNotBufferEventStream(t *testing.T) {
	requireLocalListener(t)

	// A server that sends one SSE event and then holds the connection open,
	// exactly like a real MCP SSE endpoint.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("event: endpoint\ndata: /session/1\n\n"))
			f.Flush()
		}
		<-r.Context().Done() // never terminate the body on our own
	}))
	defer server.Close()

	rt := &debugRoundTripper{base: http.DefaultTransport, debugMode: true}
	client := &http.Client{Transport: rt}

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	var resp *http.Response
	go func() {
		defer close(done)
		resp, err = client.Do(req)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RoundTrip blocked on a streaming body: it must not buffer text/event-stream")
	}

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The first event must still be readable: passing the stream through must
	// not consume or discard it.
	buf := make([]byte, 15)
	n, readErr := resp.Body.Read(buf)
	require.NoError(t, readErr)
	assert.Contains(t, string(buf[:n]), "event")
}

// A non-streaming body is still buffered and remains fully readable by the
// caller after the round-tripper has inspected it.
func TestDebugRoundTripperPreservesJSONBody(t *testing.T) {
	requireLocalListener(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	rt := &debugRoundTripper{base: http.DefaultTransport, debugMode: true}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body := make([]byte, 64)
	n, _ := resp.Body.Read(body)
	assert.Contains(t, string(body[:n]), `"jsonrpc":"2.0"`,
		"the buffered body must be replayed to the caller")
}
