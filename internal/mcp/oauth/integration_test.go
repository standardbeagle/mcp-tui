package oauth_test

// Service-level integration test for the OAuth handler. This is in a
// _test package so we can exercise the public Service interface without
// fighting unexported fields. The test confirms that:
//
//   - When ConnectionConfig.OAuth is populated, service.Connect builds an
//     OAuth handler and exposes it via GetOAuthHandler().
//   - The handler's Status() reports the configured mode.
//   - Reauthenticate() clears the handler state.
//
// We do NOT exercise an end-to-end MCP handshake here — the SDK transport
// requires a real MCP server to negotiate. That is covered by the
// black-box CLI tests under /tests/.

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestServiceWiring_OAuthAttached creates a service, supplies an OAuth
// config, and verifies the wiring. We use a fake remote server URL — the
// service treats connection failure as "not connected" but should still
// have built and stashed the OAuth handler before the transport tried to
// dial.
func TestServiceWiring_OAuthAttached(t *testing.T) {
	testutil.RequireLocalListener(t)

	// Use httptest server URL just to keep Validate() happy with http://.
	srv := httptest.NewServer(nil)
	defer srv.Close()

	svc := mcp.NewService()

	// Build the connection config the way the CLI does.
	connCfg := &config.ConnectionConfig{
		Type: config.TransportHTTP,
		URL:  srv.URL + "/mcp",
		OAuth: &oauth.Config{
			ServerURL:    srv.URL + "/mcp",
			ClientID:     "service-id",
			ClientSecret: "service-secret",
			CachePath:    "-",
		},
	}

	// Connect will fail (no MCP server on the other end) but the OAuth
	// handler is built before the dial.
	_ = svc.Connect(t.Context(), connCfg)

	// Even on failure the handler should be installed so the user can see
	// the configured mode in the TUI.
	h := svc.GetOAuthHandler()
	require.NotNil(t, h, "expected OAuth handler to be wired before transport dial")
	assert.Equal(t, oauth.ModeClientCredentials, h.Mode())

	require.NoError(t, h.Reauthenticate())
	assert.Equal(t, oauth.StateIdle, h.Status().State)
}

// TestServiceWiring_NoOAuth confirms GetOAuthHandler returns nil when no
// OAuth config is supplied (the dominant case for STDIO connections).
func TestServiceWiring_NoOAuth(t *testing.T) {
	svc := mcp.NewService()
	// A process that exits cleanly without speaking MCP. The connection fails;
	// what matters here is that no OAuth handler was built for a STDIO config.
	command, args := testutil.ServerExitsImmediately(t)
	connCfg := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: command,
		Args:    args,
	}
	_ = svc.Connect(t.Context(), connCfg)
	assert.Nil(t, svc.GetOAuthHandler())
}
