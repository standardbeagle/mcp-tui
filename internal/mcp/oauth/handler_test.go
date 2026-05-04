package oauth

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_ModeAndValidate exercises the mode-inference table and the
// rejection paths for malformed configs.
func TestConfig_ModeAndValidate(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		wantMode Mode
		wantErr  string // substring match; empty = expect no error
	}{
		{
			name:     "client_credentials full",
			cfg:      &Config{ServerURL: "https://x", ClientID: "id", ClientSecret: "sec"},
			wantMode: ModeClientCredentials,
		},
		{
			name:     "auth_code public client",
			cfg:      &Config{ServerURL: "https://x", ClientID: "id"},
			wantMode: ModeAuthorizationCode,
		},
		{
			name:     "auth_code DCR",
			cfg:      &Config{ServerURL: "https://x", EnableDynamicRegistration: true},
			wantMode: ModeAuthorizationCode,
		},
		{
			name:     "none",
			cfg:      &Config{ServerURL: "https://x"},
			wantMode: ModeNone,
		},
		{
			name:    "missing server URL",
			cfg:     &Config{ClientID: "id", ClientSecret: "sec"},
			wantErr: "ServerURL is required",
		},
		{
			name:    "bad scheme",
			cfg:     &Config{ServerURL: "ftp://x", ClientID: "id", ClientSecret: "sec"},
			wantErr: "must use http or https",
		},
		{
			name:     "auth_code port out of range",
			cfg:      &Config{ServerURL: "https://x", ClientID: "id", RedirectPort: 70000},
			wantMode: ModeAuthorizationCode,
			wantErr:  "RedirectPort 70000 out of range",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.Mode()
			if tc.wantMode != 0 || tc.wantErr == "" {
				assert.Equal(t, tc.wantMode, got, "mode")
			}
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestParseScopes verifies the comma/space-separated scope parser.
func TestParseScopes(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"read", []string{"read"}},
		{"read write", []string{"read", "write"}},
		{"read,write", []string{"read", "write"}},
		{"  read , write \tadmin", []string{"read", "write", "admin"}},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseScopes(tc.raw)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestClientCredentialsFlow runs the full RFC 6749 §4.4 flow end to end:
//  1. SDK transport sends an unauthenticated request → 401 + WWW-Authenticate
//  2. Handler.Authorize discovers PRM, ASM, and exchanges credentials.
//  3. TokenSource returns the issued token.
//
// This is the CI-required mock OAuth server test for client_credentials.
func TestClientCredentialsFlow(t *testing.T) {
	srv := newMockAuthServer(t)

	cfg := &Config{
		ServerURL:    srv.ResourceURL(),
		ClientID:     srv.clientID,
		ClientSecret: srv.clientSecret,
		CachePath:    "-", // disable persistence for the test
	}
	cache, err := NewFileTokenCache(cfg.CachePath)
	require.NoError(t, err)

	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	require.Equal(t, ModeClientCredentials, h.Mode())
	assert.Equal(t, StateIdle, h.Status().State)

	// Simulate the 401 the SDK would have received from the resource
	// server. Authorize() is what the SDK transport calls on the first 401.
	driveClientCredentials(t, h, srv)

	assert.Equal(t, StateAuthorized, h.Status().State)
	assert.NoError(t, h.Status().LastError)

	ts, err := h.TokenSource(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ts)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, srv.issuedAccessToken, tok.AccessToken)

	// Verify the mock saw a client_credentials request with the expected
	// fields. This guards against regressions where the SDK changes the
	// grant type silently.
	last := srv.lastTokenRequest()
	require.NotNil(t, last)
	assert.Equal(t, "client_credentials", last.Get("grant_type"))
}

// TestClientCredentialsFlow_BadSecret verifies the failure path: when the
// auth server rejects the credentials, Handler.Authorize returns an error
// and stays in StateError.
func TestClientCredentialsFlow_BadSecret(t *testing.T) {
	srv := newMockAuthServer(t)

	cfg := &Config{
		ServerURL:    srv.ResourceURL(),
		ClientID:     srv.clientID,
		ClientSecret: "wrong-secret",
		CachePath:    "-",
	}
	cache, err := NewFileTokenCache("-")
	require.NoError(t, err)

	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, srv.ResourceURL(), nil)
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}
	resp.Header.Set("WWW-Authenticate",
		`Bearer resource_metadata="`+srv.resourceServer.URL+`/.well-known/oauth-protected-resource/mcp"`)

	err = h.Authorize(context.Background(), req, resp)
	require.Error(t, err)

	st := h.Status()
	assert.Equal(t, StateError, st.State)
	require.Error(t, st.LastError)
}

// TestAuthorizationCodeFlow runs the full RFC 6749 §4.1 + PKCE flow with a
// pre-registered public client. The fetcher is replaced with a stub that
// auto-approves: it issues a GET to the authorization URL (which the mock
// auth server handles by 302-redirecting back to the loopback callback).
func TestAuthorizationCodeFlow(t *testing.T) {
	srv := newMockAuthServer(t)

	cfg := &Config{
		ServerURL:    srv.ResourceURL(),
		ClientID:     srv.clientID,
		ClientSecret: "", // public client — engages auth-code mode
		CachePath:    "-",
	}
	cache, _ := NewFileTokenCache("-")
	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	require.Equal(t, ModeAuthorizationCode, h.Mode())

	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)

	st := h.Status()
	assert.Equal(t, StateAuthorized, st.State, "auth-code flow should authorize: %v", st.LastError)

	ts, err := h.TokenSource(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ts)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, srv.issuedAccessToken, tok.AccessToken)
	assert.Equal(t, "test_refresh_token", tok.RefreshToken)
}

// TestAuthorizationCodeFlow_DCR verifies that when ClientID is empty and
// EnableDynamicRegistration is true, the handler registers a client via
// RFC 7591 before kicking off the auth-code flow.
func TestAuthorizationCodeFlow_DCR(t *testing.T) {
	srv := newMockAuthServer(t)
	srv.allowDCR = true

	cfg := &Config{
		ServerURL:                 srv.ResourceURL(),
		EnableDynamicRegistration: true,
		CachePath:                 "-",
	}
	cache, _ := NewFileTokenCache("-")
	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	require.Equal(t, ModeAuthorizationCode, h.Mode())

	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)

	st := h.Status()
	assert.Equal(t, StateAuthorized, st.State, "DCR auth-code flow should authorize: %v", st.LastError)

	srv.mu.Lock()
	regCount := len(srv.registerRequests)
	srv.mu.Unlock()
	assert.Equal(t, 1, regCount, "expected exactly one DCR request")
}

// TestReauthenticate verifies the TUI-facing helper. After Authorize
// succeeds Reauthenticate clears state so the next request re-runs the
// flow.
func TestReauthenticate(t *testing.T) {
	srv := newMockAuthServer(t)
	cfg := &Config{
		ServerURL:    srv.ResourceURL(),
		ClientID:     srv.clientID,
		ClientSecret: srv.clientSecret,
		CachePath:    "-",
	}
	cache, _ := NewFileTokenCache("-")
	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)

	driveClientCredentials(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State)

	require.NoError(t, h.Reauthenticate())
	st := h.Status()
	assert.Equal(t, StateIdle, st.State)
	assert.Nil(t, st.LastError)

	ts, _ := h.TokenSource(context.Background())
	assert.Nil(t, ts, "token source should be nil after Reauthenticate")
}

// TestStatus_String covers the trivial enum helpers. Cheap insurance against
// off-by-one breakage in switch statements.
func TestStatus_String(t *testing.T) {
	assert.Equal(t, "idle", StateIdle.String())
	assert.Equal(t, "authorizing", StateAuthorizing.String())
	assert.Equal(t, "authorized", StateAuthorized.String())
	assert.Equal(t, "error", StateError.String())
	assert.Equal(t, "client_credentials", ModeClientCredentials.String())
	assert.Equal(t, "authorization_code", ModeAuthorizationCode.String())
	assert.Equal(t, "none", ModeNone.String())
}

// TestConfig_ScopeList_Dedups exercises the scope-list deduper.
func TestConfig_ScopeList_Dedups(t *testing.T) {
	cfg := &Config{Scopes: []string{"read", "write", "read", "  ", "admin"}}
	got := cfg.scopeList()
	assert.Equal(t, []string{"read", "write", "admin"}, got)
}

// TestPreregistered builds the SDK ClientCredentials value from a Config
// and asserts the shape matches what the SDK expects.
func TestPreregistered(t *testing.T) {
	cfg := &Config{ClientID: "id", ClientSecret: "secret"}
	cc := cfg.preregistered()
	require.NotNil(t, cc)
	assert.Equal(t, "id", cc.ClientID)
	require.NotNil(t, cc.ClientSecretAuth)
	assert.Equal(t, "secret", cc.ClientSecretAuth.ClientSecret)

	// Public client.
	cfg = &Config{ClientID: "id"}
	cc = cfg.preregistered()
	require.NotNil(t, cc)
	assert.Equal(t, "id", cc.ClientID)
	assert.Nil(t, cc.ClientSecretAuth)

	// Empty client.
	cfg = &Config{}
	assert.Nil(t, cfg.preregistered())
}

// TestLocalServerFetcher_RedirectURL verifies that LocalServerFetcher binds
// a loopback listener and produces a callback URL matching its address.
func TestLocalServerFetcher_RedirectURL(t *testing.T) {
	f := newLocalServerFetcher("127.0.0.1", 0)
	defer f.Close() //nolint:errcheck

	u := f.RedirectURL()
	require.NotEmpty(t, u)
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "http", parsed.Scheme)
	assert.True(t, strings.HasSuffix(parsed.Path, "/callback"))
	host, _, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	assert.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host)
}

// TestLocalServerFetcher_Fetch_HappyPath issues a callback to the local
// server and verifies the returned AuthorizationResult.
func TestLocalServerFetcher_Fetch_HappyPath(t *testing.T) {
	f := newLocalServerFetcher("127.0.0.1", 0)
	redirectURL := f.RedirectURL()
	require.NotEmpty(t, redirectURL)

	// fakeAuthSrv plays the role of the IdP: when GET-ed with ?state=...
	// it 302-redirects to the loopback callback with code+state.
	fakeAuthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		http.Redirect(w, r, redirectURL+"?code=abc&state="+state, http.StatusFound)
	}))
	defer fakeAuthSrv.Close()

	f.browserOpener = func(target string) error {
		go func() {
			_, _ = http.Get(target)
		}()
		return nil
	}

	res, err := f.Fetch(context.Background(), &auth.AuthorizationArgs{URL: fakeAuthSrv.URL + "?state=mystate"})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "abc", res.Code)
	assert.Equal(t, "mystate", res.State)
}

// TestLocalServerFetcher_Fetch_OAuthError checks the error path when the
// OAuth server returns ?error= instead of ?code=.
func TestLocalServerFetcher_Fetch_OAuthError(t *testing.T) {
	f := newLocalServerFetcher("127.0.0.1", 0)
	redirectURL := f.RedirectURL()

	fakeAuthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectURL+"?error=access_denied&error_description=user+said+no", http.StatusFound)
	}))
	defer fakeAuthSrv.Close()

	f.browserOpener = func(target string) error {
		go func() { _, _ = http.Get(target) }()
		return nil
	}

	res, err := f.Fetch(context.Background(), &auth.AuthorizationArgs{URL: fakeAuthSrv.URL})
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "access_denied")
}

// driveClientCredentials simulates the SDK transport reaching the resource
// URL, getting a 401, and calling Authorize on the handler.
func driveClientCredentials(t *testing.T, h *Handler, srv *mockAuthServer) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.ResourceURL(), nil)
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}
	resp.Header.Set("WWW-Authenticate",
		`Bearer resource_metadata="`+srv.resourceServer.URL+`/.well-known/oauth-protected-resource/mcp", scope="mcp:read"`)
	require.NoError(t, h.Authorize(context.Background(), req, resp))
}

// driveAuthCode reuses driveClientCredentials — Handler.Authorize dispatches
// based on Mode(), so the same 401 simulation drives either grant. The
// auth-code path additionally needs the fetcher's browser stub installed
// (see installAutoApproveFetcher).
func driveAuthCode(t *testing.T, h *Handler, srv *mockAuthServer) {
	t.Helper()
	driveClientCredentials(t, h, srv)
}

// installAutoApproveFetcher replaces the Handler's fetcherFactory with one
// that auto-approves: each fetch issues an HTTP GET to the supplied auth
// URL, which the mock auth server responds to with a 302 to the loopback
// callback URL — triggering the local server to capture the code/state and
// complete the flow without any human in the loop.
func installAutoApproveFetcher(t *testing.T, h *Handler) {
	t.Helper()
	h.fetcherFactory = func(host string, port int) AuthorizationCodeFetcher {
		f := newLocalServerFetcher(host, port)
		f.browserOpener = func(target string) error {
			go func() {
				// http.Client follows 302 by default, so this single GET
				// chain authorize → callback → done.
				_, err := http.Get(target)
				if err != nil {
					t.Logf("auto-approve GET failed: %v", err)
				}
			}()
			return nil
		}
		return f
	}
}
