package transports

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc is a tiny helper RoundTripper used to capture the request the
// outer middleware forwards. We reuse it across all method-headers tests so the
// assertion surface stays focused on the headers the middleware writes.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// callMethodHeaders is a tiny helper that posts a JSON-RPC body through the
// wrapping RoundTripper and returns the request the inner transport observed.
// This keeps each test focused on a single (input body) → (header) assertion.
func callMethodHeaders(t *testing.T, body string) *http.Request {
	t.Helper()

	var captured *http.Request
	inner := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		buf := &bytes.Buffer{}
		if r.Body != nil {
			_, _ = io.Copy(buf, r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		}
		clone := r.Clone(r.Context())
		clone.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		captured = clone
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(http.Header),
		}, nil
	})

	rt := newMethodHeadersRoundTripper(inner)
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	defer resp.Body.Close()

	if captured == nil {
		t.Fatal("inner RoundTripper was not called")
	}
	return captured
}

func TestMethodHeadersRoundTripper_ToolsCallSetsBothHeaders(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`
	captured := callMethodHeaders(t, body)

	if got := captured.Header.Get("MCP-Method"); got != "tools/call" {
		t.Errorf("MCP-Method header = %q, want %q", got, "tools/call")
	}
	if got := captured.Header.Get("MCP-Name"); got != "echo" {
		t.Errorf("MCP-Name header = %q, want %q", got, "echo")
	}
}

func TestMethodHeadersRoundTripper_PromptsGetSetsName(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"summarize"}}`
	captured := callMethodHeaders(t, body)

	if got := captured.Header.Get("MCP-Method"); got != "prompts/get" {
		t.Errorf("MCP-Method header = %q, want %q", got, "prompts/get")
	}
	if got := captured.Header.Get("MCP-Name"); got != "summarize" {
		t.Errorf("MCP-Name header = %q, want %q", got, "summarize")
	}
}

func TestMethodHeadersRoundTripper_ResourcesReadUsesURI(t *testing.T) {
	// resources/read identifies the target by uri rather than name; SEP-2243
	// says MCP-Name carries "tool/resource/prompt name", and the resource URI
	// is the only stable identifier on the request, so we mirror it here.
	body := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///tmp/x.txt"}}`
	captured := callMethodHeaders(t, body)

	if got := captured.Header.Get("MCP-Method"); got != "resources/read" {
		t.Errorf("MCP-Method header = %q, want %q", got, "resources/read")
	}
	if got := captured.Header.Get("MCP-Name"); got != "file:///tmp/x.txt" {
		t.Errorf("MCP-Name header = %q, want %q", got, "file:///tmp/x.txt")
	}
}

func TestMethodHeadersRoundTripper_NoNameForListMethods(t *testing.T) {
	// tools/list, resources/list, prompts/list have no name to mirror, so only
	// MCP-Method should be set.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	captured := callMethodHeaders(t, body)

	if got := captured.Header.Get("MCP-Method"); got != "tools/list" {
		t.Errorf("MCP-Method header = %q, want %q", got, "tools/list")
	}
	if got := captured.Header.Get("MCP-Name"); got != "" {
		t.Errorf("MCP-Name header = %q, want empty", got)
	}
}

func TestMethodHeadersRoundTripper_InitializeNoName(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	captured := callMethodHeaders(t, body)

	if got := captured.Header.Get("MCP-Method"); got != "initialize" {
		t.Errorf("MCP-Method header = %q, want %q", got, "initialize")
	}
	if got := captured.Header.Get("MCP-Name"); got != "" {
		t.Errorf("MCP-Name header = %q, want empty", got)
	}
}

func TestMethodHeadersRoundTripper_NonJSONBodyPasses(t *testing.T) {
	// A non-JSON body (or missing body) must not break the request; SSE GETs
	// reach the same RoundTripper and have no JSON-RPC envelope.
	rt := newMethodHeadersRoundTripper(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("MCP-Method") != "" {
			t.Errorf("expected no MCP-Method on non-JSON request, got %q", r.Header.Get("MCP-Method"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.test/sse", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	resp.Body.Close()
}

func TestMethodHeadersRoundTripper_BodyPreservedForInner(t *testing.T) {
	// The middleware reads the body to peek at the JSON-RPC method. It must
	// rewind the body so the SDK transport can still send it to the server,
	// otherwise every POST would arrive empty.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`

	var seen string
	inner := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("inner read body: %v", err)
		}
		seen = string(buf)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(http.Header),
		}, nil
	})

	rt := newMethodHeadersRoundTripper(inner)
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	resp.Body.Close()

	if seen != body {
		t.Errorf("body forwarded to inner = %q, want %q", seen, body)
	}
}

func TestGetHTTPClientForTransport_FlagOnInjectsHeaders(t *testing.T) {
	requireLocalListener(t)

	// When the flag is on, GetHTTPClientForTransportWithMethodHeaders must
	// return an http.Client whose Transport injects MCP-Method.
	client := GetHTTPClientForTransportWithMethodHeaders(TransportHTTP, nil, true)
	if client == nil {
		t.Fatal("expected non-nil http client")
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("MCP-Method"); got != "tools/list" {
			t.Errorf("MCP-Method header = %q, want %q", got, "tools/list")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
}

func TestSetRequestHeaderObserver_FiresWithInjectedValues(t *testing.T) {
	// The observer hook is how the mcp package surfaces SEP-2243 headers in
	// the debug HTTP tab when requests bypass the global debugRoundTripper.
	// Reset to nil after the test so other tests don't see the stub.
	defer SetRequestHeaderObserver(nil)

	type seen struct {
		method string
		name   string
	}
	got := make(chan seen, 1)
	SetRequestHeaderObserver(func(req *http.Request, mcpMethod, mcpName string) {
		got <- seen{method: mcpMethod, name: mcpName}
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`
	rt := newMethodHeadersRoundTripper(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(http.Header),
		}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	resp.Body.Close()

	select {
	case s := <-got:
		if s.method != "tools/call" || s.name != "echo" {
			t.Errorf("observer saw method=%q name=%q, want method=%q name=%q",
				s.method, s.name, "tools/call", "echo")
		}
	default:
		t.Fatal("observer was not invoked")
	}
}

func TestGetHTTPClientForTransport_FlagOffOmitsHeaders(t *testing.T) {
	requireLocalListener(t)

	// Default path: no flag → no MCP-Method/MCP-Name. This guards against the
	// regression where the wrapper would be installed unconditionally.
	client := GetHTTPClientForTransportWithMethodHeaders(TransportHTTP, nil, false)
	if client == nil {
		t.Fatal("expected non-nil http client")
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("MCP-Method"); got != "" {
			t.Errorf("MCP-Method header = %q, want empty when flag off", got)
		}
		if got := r.Header.Get("MCP-Name"); got != "" {
			t.Errorf("MCP-Name header = %q, want empty when flag off", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
}
