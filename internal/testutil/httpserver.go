package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeStreamableHTTP serves server over streamable HTTP on a loopback
// httptest server, closed on test cleanup, and returns its URL. The SDK
// speaks 2026-07-28 (protocolVersion "") only from a stateless handler, and
// delivers older protocols' server-initiated notifications only on a
// stateful handler's GET stream, so the handler mode follows the version.
func ServeStreamableHTTP(t *testing.T, server *officialMCP.Server, protocolVersion string) string {
	t.Helper()
	RequireLocalListener(t)
	httpServer := httptest.NewServer(officialMCP.NewStreamableHTTPHandler(
		func(*http.Request) *officialMCP.Server { return server },
		&officialMCP.StreamableHTTPOptions{Stateless: protocolVersion == "" || protocolVersion >= MRTRProtocolVersion}))
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}
