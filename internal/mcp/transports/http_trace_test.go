package transports

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestGetHTTPClientForTransportFull_TracesEveryExchange covers the MCP
// transport client: its custom http.Transport bypasses anything installed on
// http.DefaultTransport, so tracing must be layered into the client itself,
// with or without the optional header wrappers.
func TestGetHTTPClientForTransportFull_TracesEveryExchange(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()
	read, stop := debug.Capture(debug.LogLevelDebug)
	defer stop()

	client := GetHTTPClientForTransportFull(TransportStreamableHTTP, nil, false, nil)
	resp, err := client.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()

	out := read()
	if !strings.Contains(out, "[mcp-http] HTTP exchange") || !strings.Contains(out, "status=200") {
		t.Errorf("MCP transport exchange not traced:\n%s", out)
	}
}
