package oauth

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// captureAuthLogs records everything logged at debug level for the rest of
// the test, the level --debug runs at.
func captureAuthLogs(t *testing.T) func() string {
	t.Helper()
	read, stop := debug.Capture(debug.LogLevelDebug)
	t.Cleanup(stop)
	return read
}

// assertNoSecrets fails the test if any credential value appears in out.
func assertNoSecrets(t *testing.T, out string, secrets []string) {
	t.Helper()
	require.NotEmpty(t, secrets)
	for _, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaked credential %q", secret)
		}
	}
}

// assertLogged fails the test for each expected fragment missing from out.
func assertLogged(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("log output missing %q", w)
		}
	}
}

// TestAuthorizationCodeFlow_DCR_LogsEveryStepWithoutSecrets runs the full
// DCR + authorization-code + PKCE flow against the mock AS and sweeps every
// line the run logged: each step of the flow must be visible, and no token,
// code, verifier, state or client secret may appear anywhere.
func TestAuthorizationCodeFlow_DCR_LogsEveryStepWithoutSecrets(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.allowDCR = true

	h, err := NewHandler(&Config{
		ServerURL:                 srv.ResourceURL(),
		EnableDynamicRegistration: true,
		CachePath:                 "-",
	}, http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	var buffered strings.Builder
	for _, line := range debug.GetLogBuffer().GetEntriesAsStrings() {
		buffered.WriteString(line + "\n")
	}
	assertNoSecrets(t, buffered.String(), srv.issuedSecrets())

	assertLogged(t, out,
		"[oauth] OAuth mode selected mode=authorization_code",
		"registration=dynamic",
		"[oauth] Authorization required status=401",
		"[oauth] Protected resource metadata discovered",
		"scopes_supported=[mcp:read mcp:write]",
		"[oauth] Authorization server metadata discovered",
		"code_challenge_methods_supported=[S256]",
		"registration_endpoint="+srv.AuthURL()+"/register",
		"iss_parameter_supported=false",
		"client_id_metadata_document_supported=false",
		"[oauth] Dynamic client registration status=201",
		"[oauth] Scopes discovered",
		"[oauth] Authorization request",
		"code_challenge_method=S256",
		"[oauth] Authorization callback received has_code=true has_state=true has_iss=false",
		"[oauth] Token request grant_type=authorization_code",
		"[oauth] Token response status=200 grant_type=authorization_code token_type=Bearer",
		"has_refresh_token=true",
		"[oauth] Authorization succeeded",
		"[oauth-http] HTTP exchange",
	)
}

// TestClientCredentialsFlow_LogsWithoutSecrets covers the client-secret
// grant, where the secret travels in the token request itself.
func TestClientCredentialsFlow_LogsWithoutSecrets(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)

	h, err := NewHandler(&Config{
		ServerURL:    srv.ResourceURL(),
		ClientID:     srv.clientID,
		ClientSecret: srv.clientSecret,
		CachePath:    "-",
	}, http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	driveClientCredentials(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out,
		"[oauth] OAuth mode selected mode=client_credentials",
		"[oauth] Token request grant_type=client_credentials",
		"[oauth] Token response status=200",
	)
}

// TestAuthorizationCodeFlow_IssParameter covers RFC 9207: an AS that
// advertises authorization_response_iss_parameter_supported sends iss on the
// redirect, and the SDK refuses the response unless the callback hands it on.
func TestAuthorizationCodeFlow_IssParameter(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.advertiseIss = true

	h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, CachePath: "-"},
		http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out,
		"iss_parameter_supported=true",
		"[oauth] Authorization callback received has_code=true has_state=true has_iss=true",
		"[oauth] Authorization succeeded")
}

// TestAuthorizationCodeFlow_IssMismatchRejected is the mix-up defence: an iss
// naming a different issuer fails the flow before the code is redeemed.
func TestAuthorizationCodeFlow_IssMismatchRejected(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.advertiseIss = true
	srv.callbackIss = "https://evil.example"

	h, err := NewHandler(&Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, CachePath: "-"},
		http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	req, resp := unauthorizedExchange(srv)
	err = h.Authorize(context.Background(), req, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match expected issuer")
	assert.Equal(t, 0, srv.tokenRequestCount(), "code must not be redeemed after an issuer mismatch")

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out, "[oauth] Authorization failed")
}
