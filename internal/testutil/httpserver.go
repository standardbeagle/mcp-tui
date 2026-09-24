package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// StreamableHTTPHandler serves server over streamable HTTP for a client
// speaking protocolVersion ("" = latest). The SDK speaks 2026-07-28 only
// from a stateless handler, and delivers older protocols' server-initiated
// notifications only on a stateful handler's GET stream, so the handler mode
// follows the version.
func StreamableHTTPHandler(server *officialMCP.Server, protocolVersion string) http.Handler {
	return officialMCP.NewStreamableHTTPHandler(
		func(*http.Request) *officialMCP.Server { return server },
		&officialMCP.StreamableHTTPOptions{Stateless: protocolVersion == "" || protocolVersion >= MRTRProtocolVersion})
}

// ServeStreamableHTTP serves handler on a loopback httptest server, closed
// on test cleanup, and returns its URL.
func ServeStreamableHTTP(t *testing.T, handler http.Handler) string {
	t.Helper()
	RequireLocalListener(t)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}
