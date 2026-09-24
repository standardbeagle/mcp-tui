package oauth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestedScopes is the scope set the last authorization request asked for.
func requestedScopes(srv *mockAuthServer) []string {
	return strings.Fields(srv.lastAuthorizeRequest().Get("scope"))
}

// TestAuthorizationCodeFlow_ConfiguredScopes: --oauth-scopes replaces the
// scopes discovered from the challenge / PRM.
func TestAuthorizationCodeFlow_ConfiguredScopes(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)

	h, err := NewHandler(&Config{
		ServerURL: srv.ResourceURL(),
		ClientID:  srv.clientID,
		Scopes:    []string{"files:read", "files:write"},
		CachePath: "-",
	}, http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	assert.ElementsMatch(t, []string{"files:read", "files:write"}, requestedScopes(srv))
	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out, "[oauth] Scopes selected source=configured discovered=[mcp:read] selected=[files:read files:write]")
	assert.NotContains(t, out, "are not applied")
}

// TestAuthorizationCodeFlow_DiscoveredScopes: without --oauth-scopes the
// discovered set is requested unchanged.
func TestAuthorizationCodeFlow_DiscoveredScopes(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)

	h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, CachePath: "-"},
		http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	assert.ElementsMatch(t, []string{"mcp:read"}, requestedScopes(srv))
	assertLogged(t, logs(), "[oauth] Scopes selected source=discovered discovered=[mcp:read] selected=[mcp:read]")
}

// TestConfig_ScopesRejectedForClientCredentials: the SDK's client-credentials
// handler has no scope hook, so configured scopes there would be ignored.
func TestConfig_ScopesRejectedForClientCredentials(t *testing.T) {
	err := (&Config{ServerURL: "https://x", ClientID: "svc", ClientSecret: "s3cr3t-value", Scopes: []string{"mcp:read"}}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scopes cannot be configured for client-credentials")
}
