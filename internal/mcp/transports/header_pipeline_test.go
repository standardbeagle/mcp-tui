package transports

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestHeaderPipeline_FullStackForwardsAndObserves wires the same layered
// HTTP client that GetHTTPClientForTransportFull builds at runtime — static
// headers + response observer + SEP-2243 method headers — and confirms the
// composition reaches the wire and back into the debug surfaces.
//
// This guards the cross-cutting concern that the iter-13 method headers
// path and the iter-14 static headers / response observer paths cooperate
// rather than overwriting each other.
func TestHeaderPipeline_FullStackForwardsAndObserves(t *testing.T) {
	requireLocalListener(t)

	// Track what the server saw — these assertions verify that --header
	// values reach the wire alongside SEP-2243 headers without conflict.
	var (
		mu             sync.Mutex
		seenAuth       string
		seenTrace      string
		seenMCPMethod  string
		seenContent    string
		responseCalled bool
		responseStatus int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAuth = r.Header.Get("Authorization")
		seenTrace = r.Header.Get("X-Trace-Id")
		seenMCPMethod = r.Header.Get("MCP-Method")
		seenContent = r.Header.Get("Content-Type")
		mu.Unlock()
		w.Header().Set("X-Server-Trace", "srv-99")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	// Wire a response observer like the mcp package does at startup.
	SetResponseObserver(func(req *http.Request, resp *http.Response, err error) {
		mu.Lock()
		defer mu.Unlock()
		responseCalled = true
		if resp != nil {
			responseStatus = resp.StatusCode
			// Snapshot a header value so we know the observer can see the
			// real http.Response (not just the request).
			if resp.Header.Get("X-Server-Trace") != "srv-99" {
				t.Errorf("response observer missed X-Server-Trace header")
			}
		}
	})
	defer SetResponseObserver(nil)

	staticHeaders := map[string]string{
		"X-Trace-Id":    "trace-pipeline",
		"Authorization": "Bearer pipeline-token",
	}

	client := GetHTTPClientForTransportFull(TransportHTTP, nil, true, staticHeaders)

	// Build a JSON-RPC POST so the SEP-2243 RoundTripper extracts the
	// method. Body content matches what the SDK would emit for tools/list.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL, stringReader(body))
	req.Header.Set("Content-Type", "application/json") // protocol-correct header set by SDK

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if seenAuth != "Bearer pipeline-token" {
		t.Errorf("server Authorization: got %q, want %q (static headers must reach wire)", seenAuth, "Bearer pipeline-token")
	}
	if seenTrace != "trace-pipeline" {
		t.Errorf("server X-Trace-Id: got %q, want %q", seenTrace, "trace-pipeline")
	}
	if seenMCPMethod != "tools/list" {
		t.Errorf("server MCP-Method: got %q, want %q (SEP-2243 must reach wire)", seenMCPMethod, "tools/list")
	}
	if seenContent != "application/json" {
		t.Errorf("server Content-Type: got %q, want %q (existing protocol header must not be stomped)", seenContent, "application/json")
	}
	if !responseCalled {
		t.Error("response observer was never invoked")
	}
	if responseStatus != http.StatusOK {
		t.Errorf("response observer status: got %d, want %d", responseStatus, http.StatusOK)
	}
}

// TestHeaderPipeline_ObserverSeesMethodHeaders verifies that the response
// observer's snapshot of req.Header includes the SEP-2243 MCP-Method/
// MCP-Name values injected by the outer RoundTripper. This is the load-
// bearing assertion for acceptance criterion 5: the debug pane must show
// the MCP-Method header alongside other request headers.
func TestHeaderPipeline_ObserverSeesMethodHeaders(t *testing.T) {
	requireLocalListener(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var observedMethod, observedName string
	SetResponseObserver(func(req *http.Request, resp *http.Response, err error) {
		observedMethod = req.Header.Get("MCP-Method")
		observedName = req.Header.Get("MCP-Name")
	})
	defer SetResponseObserver(nil)

	client := GetHTTPClientForTransportFull(TransportHTTP, nil, true, nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{}}}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL, stringReader(body))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	resp.Body.Close()

	if observedMethod != "tools/call" {
		t.Errorf("observer MCP-Method: got %q, want %q", observedMethod, "tools/call")
	}
	if observedName != "echo" {
		t.Errorf("observer MCP-Name: got %q, want %q", observedName, "echo")
	}
}

// TestResponseObserver_FiresOnError documents the failure path: when the
// inner round-trip returns an error (resp == nil), the observer still fires
// so the debug pane can show what we attempted to send.
func TestResponseObserver_FiresOnError(t *testing.T) {
	called := false
	SetResponseObserver(func(req *http.Request, resp *http.Response, err error) {
		called = true
		if resp != nil {
			t.Errorf("expected nil resp on error, got %v", resp)
		}
		if err == nil {
			t.Error("expected non-nil error")
		}
	})
	defer SetResponseObserver(nil)

	rt := newResponseObserverRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errFailRoundTrip
	}))
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Error("observer was not called on error")
	}
}

// stringReader is a minimal io.Reader wrapper around a string. We could use
// strings.NewReader but keeping this local and obvious avoids ambiguity over
// who closes the body when the test runs.
type stringReaderImpl struct {
	s   string
	pos int
}

func stringReader(s string) *stringReaderImpl { return &stringReaderImpl{s: s} }

func (r *stringReaderImpl) Read(p []byte) (int, error) {
	if r.pos >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.pos:])
	r.pos += n
	return n, nil
}

var errFailRoundTrip = simpleErr("simulated round-trip failure")

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
