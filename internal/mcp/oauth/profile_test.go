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

// TestAuthorizationCodeFlow_RequestsOfflineAccess (SEP-2207): mcp-tui can
// store refresh tokens, so it asks for offline_access when the AS supports
// it, and a dynamically registered client declares the refresh_token grant.
func TestAuthorizationCodeFlow_RequestsOfflineAccess(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.allowDCR = true
	srv.asScopes = []string{"mcp:read", "mcp:write", "offline_access"}

	h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), EnableDynamicRegistration: true, CachePath: "-"},
		http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	assert.ElementsMatch(t, []string{"mcp:read", "offline_access"}, requestedScopes(srv))
	srv.mu.Lock()
	require.Len(t, srv.registerRequests, 1)
	assert.Contains(t, string(srv.registerRequests[0]), `"refresh_token"`)
	srv.mu.Unlock()

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out, "grant_types=[authorization_code refresh_token]", "request_refresh_token=true")
}

// TestAuthorizationCodeFlow_UnadvertisedIss: an AS that sends iss without
// advertising support is refused by default; --oauth-accept-unadvertised-iss
// accepts a matching iss and warns that it is on.
func TestAuthorizationCodeFlow_UnadvertisedIss(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "default_rejects", true: "opt_in_accepts"}[accept], func(t *testing.T) {
			logs := captureAuthLogs(t)
			srv := newMockAuthServer(t)
			srv.sendUnadvertisedIss = true

			h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID,
				AcceptUnadvertisedIss: accept, CachePath: "-"}, http.DefaultClient, NoopCache{})
			require.NoError(t, err)
			installAutoApproveFetcher(t, h)
			req, resp := unauthorizedExchange(srv)
			err = h.Authorize(t.Context(), req, resp)

			out := logs()
			assertNoSecrets(t, out, srv.issuedSecrets())
			if !accept {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "does not advertise RFC 9207")
				assert.NotContains(t, out, "unadvertised RFC 9207 iss")
				return
			}
			require.NoError(t, err)
			assertLogged(t, out, "WARN [oauth] Accepting unadvertised RFC 9207 iss")
		})
	}
}

// insufficientScopeExchange builds the 403 a resource server returns when
// the token lacks a scope the operation needs (step-up, SEP-2350).
func insufficientScopeExchange(srv *mockAuthServer, scope string) (*http.Request, *http.Response) {
	req, resp := unauthorizedExchange(srv)
	resp.StatusCode = http.StatusForbidden
	resp.Header.Set("WWW-Authenticate",
		`Bearer error="insufficient_scope", scope="`+scope+`", resource_metadata="`+srv.resourceServer.URL+`/.well-known/oauth-protected-resource/mcp"`)
	return req, resp
}

// TestAuthorizationCodeFlow_StepUp (SEP-2350): a 403 insufficient_scope
// challenge re-runs authorization asking for the scopes already granted
// plus the challenged one, and the log shows the step-up.
func TestAuthorizationCodeFlow_StepUp(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured []string
		first      []string
	}{
		{name: "discovered", first: []string{"mcp:read"}},
		{name: "configured", configured: []string{"files:read"}, first: []string{"files:read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureAuthLogs(t)
			srv := newMockAuthServer(t)
			h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, Scopes: tc.configured, CachePath: "-"},
				http.DefaultClient, NoopCache{})
			require.NoError(t, err)
			installAutoApproveFetcher(t, h)
			driveAuthCode(t, h, srv)
			require.Equal(t, StateAuthorized, h.Status().State, "first flow failed: %v", h.Status().LastError)
			assert.ElementsMatch(t, tc.first, requestedScopes(srv))

			req, resp := insufficientScopeExchange(srv, "mcp:write")
			require.NoError(t, h.Authorize(t.Context(), req, resp))
			require.Equal(t, StateAuthorized, h.Status().State, "step-up failed: %v", h.Status().LastError)
			assert.ElementsMatch(t, append(append([]string{}, tc.first...), "mcp:write"), requestedScopes(srv))

			out := logs()
			assertNoSecrets(t, out, srv.issuedSecrets())
			assertLogged(t, out,
				"[oauth] Authorization required status=403",
				"error=\"insufficient_scope\"",
				"[oauth] Scopes selected source=step_up discovered=[mcp:write] selected=[mcp:write]")
		})
	}
}
