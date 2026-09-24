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

// TestPreregisteredClient_IssuerBinding: a pre-registered client bound to
// an issuer (--oauth-issuer) is used only with that authorization server.
func TestPreregisteredClient_IssuerBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secret string
		setup  func(t *testing.T, h *Handler)
	}{
		{name: "authorization_code", setup: installAutoApproveFetcher},
		{name: "client_credentials", secret: "test-secret", setup: func(*testing.T, *Handler) {}},
	} {
		t.Run(tc.name+"/match", func(t *testing.T) {
			logs := captureAuthLogs(t)
			srv := newMockAuthServer(t)
			h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, ClientSecret: tc.secret,
				Issuer: srv.AuthURL(), CachePath: "-"}, http.DefaultClient, NoopCache{})
			require.NoError(t, err)
			tc.setup(t, h)
			driveAuthCode(t, h, srv)
			require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)
			out := logs()
			assertNoSecrets(t, out, srv.issuedSecrets())
			assertLogged(t, out, "client_issuer="+srv.AuthURL())
		})
		t.Run(tc.name+"/mismatch", func(t *testing.T) {
			logs := captureAuthLogs(t)
			srv := newMockAuthServer(t)
			h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, ClientSecret: tc.secret,
				Issuer: "https://login.contoso.example", CachePath: "-"}, http.DefaultClient, NoopCache{})
			require.NoError(t, err)
			tc.setup(t, h)
			req, resp := unauthorizedExchange(srv)
			err = h.Authorize(t.Context(), req, resp)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not match pre-registered credentials issuer")
			assert.Equal(t, 0, srv.tokenRequestCount(), "no token request may reach the wrong issuer")
			out := logs()
			assertNoSecrets(t, out, srv.issuedSecrets())
			assertLogged(t, out, "[oauth] Authorization failed")
		})
	}
}

// TestConfig_IssuerRequiresPreregisteredClient: the binding applies to
// pre-registered credentials only.
func TestConfig_IssuerRequiresPreregisteredClient(t *testing.T) {
	err := (&Config{ServerURL: "https://x", EnableDynamicRegistration: true, Issuer: "https://login.contoso.example"}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Issuer requires a pre-registered ClientID")
}
