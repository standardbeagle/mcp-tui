package oauth

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// enterpriseFixture is an MCP resource server with its authorization server
// and an enterprise IdP whose ID-JAGs that authorization server trusts.
type enterpriseFixture struct {
	as  *mockAuthServer
	idp *mockIdP
}

func newEnterpriseFixture(t *testing.T) *enterpriseFixture {
	t.Helper()
	as := newMockAuthServer(t)
	idp := newMockIdP(t, as)
	as.idJAG = func() string {
		idp.mu.Lock()
		defer idp.mu.Unlock()
		return idp.idJAG
	}
	return &enterpriseFixture{as: as, idp: idp}
}

func (f *enterpriseFixture) config() *Config {
	return &Config{
		ServerURL:       f.as.ResourceURL(),
		ClientID:        f.as.clientID,
		IdPIssuer:       f.idp.URL(),
		IdPClientID:     f.idp.clientID,
		IdPClientSecret: f.idp.clientSecret,
		Scopes:          []string{"mcp:read"},
		CachePath:       "-",
	}
}

func (f *enterpriseFixture) secrets() []string {
	return append(f.idp.issuedSecrets(), f.as.issuedSecrets()...)
}

func newEnterpriseHandler(t *testing.T, cfg *Config, cache TokenCache) *Handler {
	t.Helper()
	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	return h
}

// assertNoSecretsAnywhere sweeps the captured log output and the TUI log
// buffer (what the Auth tab renders).
func assertNoSecretsAnywhere(t *testing.T, out string, secrets []string) {
	t.Helper()
	assertNoSecrets(t, out, secrets)
	var buffered strings.Builder
	for _, line := range debug.GetLogBuffer().GetEntriesAsStrings() {
		buffered.WriteString(line + "\n")
	}
	assertNoSecrets(t, buffered.String(), secrets)
}

// TestEnterpriseFlow_EndToEnd runs SEP-990 against the mock IdP and MCP
// authorization server: OIDC sign-in for an ID token, token exchange for an
// ID-JAG, JWT bearer grant for the access token. Every step is logged and no
// credential value appears in the logs or the TUI buffer.
func TestEnterpriseFlow_EndToEnd(t *testing.T) {
	logs := captureAuthLogs(t)
	f := newEnterpriseFixture(t)
	h := newEnterpriseHandler(t, f.config(), NoopCache{})

	require.NoError(t, authorizeUnauthorized(t.Context(), h, f.as))
	st := h.Status()
	require.Equal(t, StateAuthorized, st.State, "flow failed: %v", st.LastError)
	assert.Equal(t, "enterprise", st.Mode.String())

	src, err := h.TokenSource(t.Context())
	require.NoError(t, err)
	tok, err := src.Token()
	require.NoError(t, err)
	assert.Equal(t, f.as.issuedAccessToken, tok.AccessToken)

	exchange := f.idp.lastExchangeRequest()
	require.NotNil(t, exchange, "no token exchange reached the IdP")
	assert.Equal(t, f.as.AuthURL(), exchange.Get("audience"))
	assert.Equal(t, f.as.ResourceURL(), exchange.Get("resource"))
	assert.Equal(t, "mcp:read", exchange.Get("scope"))

	out := logs()
	assertNoSecretsAnywhere(t, out, f.secrets())
	assertLogged(t, out,
		"[oauth] OAuth mode selected mode=enterprise",
		"idp_issuer="+f.idp.URL(),
		"idp_client_id="+idpClientID,
		"idp_scopes=[openid]",
		"[oauth] Authorization required status=401",
		"[oauth] Protected resource metadata discovered",
		"[oauth] Enterprise authorization server resolved source=protected_resource_metadata auth_server="+f.as.AuthURL(),
		"[oauth] Enterprise sign-in started idp_issuer="+f.idp.URL(),
		"[oauth] Authorization server metadata discovered url="+f.idp.URL()+"/.well-known/",
		"[oauth] Authorization request authorization_endpoint="+f.idp.URL()+"/authorize",
		"[oauth] Authorization callback received has_code=true has_state=true",
		"[oauth] Token request grant_type=authorization_code endpoint="+f.idp.URL()+"/token",
		"has_id_token=true",
		"[oauth] ID token obtained iss="+f.idp.URL()+" aud=["+idpClientID+"]",
		"[oauth] Token exchange grant_type="+grantTokenExchange,
		"audience="+f.as.AuthURL(),
		"requested_token_type="+oauthex.TokenTypeIDJAG,
		"subject_token_type="+oauthex.TokenTypeIDToken,
		"has_subject_token=true",
		"issued_token_type="+oauthex.TokenTypeIDJAG,
		"id_jag_aud=["+f.as.AuthURL()+"]",
		"[oauth] JWT bearer grant grant_type="+grantJWTBearer,
		"has_assertion=true",
		"assertion_aud=["+f.as.AuthURL()+"]",
		"[oauth] Token response status=200 grant_type="+grantJWTBearer,
		"[oauth] Authorization succeeded mode=enterprise",
	)
}

// TestEnterpriseFlow_ConfiguredAuthServer: with --oauth-issuer the MCP
// authorization server is taken as given, without reading the resource's
// metadata.
func TestEnterpriseFlow_ConfiguredAuthServer(t *testing.T) {
	logs := captureAuthLogs(t)
	f := newEnterpriseFixture(t)
	cfg := f.config()
	cfg.Issuer = f.as.AuthURL()
	h := newEnterpriseHandler(t, cfg, NoopCache{})

	require.NoError(t, authorizeUnauthorized(t.Context(), h, f.as))
	out := logs()
	assertLogged(t, out, "[oauth] Enterprise authorization server resolved source=configured auth_server="+f.as.AuthURL())
	assert.NotContains(t, out, "Protected resource metadata discovered")
}

// TestEnterpriseFlow_WellKnownResourceMetadata: a challenge without
// resource_metadata falls to the RFC 9728 well-known location.
func TestEnterpriseFlow_WellKnownResourceMetadata(t *testing.T) {
	logs := captureAuthLogs(t)
	f := newEnterpriseFixture(t)
	h := newEnterpriseHandler(t, f.config(), NoopCache{})

	require.NoError(t, authorizeChallenge(t.Context(), h, f.as, http.StatusUnauthorized, `Bearer scope="mcp:read"`))
	assertLogged(t, logs(),
		"[oauth] Protected resource metadata discovered url="+f.as.resourceServer.URL+"/.well-known/oauth-protected-resource/mcp",
		"[oauth] Enterprise authorization server resolved source=protected_resource_metadata")
}

func authorizeEnterpriseFails(t *testing.T, f *enterpriseFixture) (out string, err error) {
	t.Helper()
	logs := captureAuthLogs(t)
	h := newEnterpriseHandler(t, f.config(), NoopCache{})
	err = authorizeUnauthorized(context.Background(), h, f.as)
	require.Error(t, err)
	assert.Equal(t, StateError, h.Status().State)
	out = logs()
	assertNoSecretsAnywhere(t, out, f.secrets())
	assertLogged(t, out, "[oauth] Authorization failed mode=enterprise")
	return out, err
}

// TestEnterpriseFlow_IdPDeniesSignIn: the IdP refuses the user at /authorize.
func TestEnterpriseFlow_IdPDeniesSignIn(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.idp.denyLogin = true
	out, err := authorizeEnterpriseFails(t, f)
	assert.Contains(t, err.Error(), "access_denied")
	assertLogged(t, out, "oauth_error=access_denied")
	assert.Equal(t, 0, f.idp.tokenRequestCount(), "no code to redeem after a denied sign-in")
}

// TestEnterpriseFlow_IdPIssMismatch is the mix-up defense on the IdP
// redirect: an iss naming another issuer fails before the code is redeemed.
func TestEnterpriseFlow_IdPIssMismatch(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.idp.callbackIss = "https://login.fabrikam.example"
	_, err := authorizeEnterpriseFails(t, f)
	assert.Contains(t, err.Error(), "does not match the IdP issuer")
	assert.Equal(t, 0, f.idp.tokenRequestCount(), "code must not be redeemed after an issuer mismatch")
}

// TestEnterpriseFlow_ExchangeRejected: the IdP refuses to issue an ID-JAG.
func TestEnterpriseFlow_ExchangeRejected(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.idp.denyExchange = true
	out, err := authorizeEnterpriseFails(t, f)
	assert.Contains(t, err.Error(), "token exchange failed")
	assertLogged(t, out, "[oauth] Token response status=400 grant_type="+grantTokenExchange, "oauth_error=invalid_grant")
	assert.Equal(t, 0, f.as.tokenRequestCount(), "no assertion to redeem after a failed exchange")
}

// TestEnterpriseFlow_AudienceMismatch: an ID-JAG addressed to another
// authorization server is refused by the MCP authorization server, and the
// log shows both audiences.
func TestEnterpriseFlow_AudienceMismatch(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.idp.jagAudience = "https://auth.fabrikam.example"
	out, err := authorizeEnterpriseFails(t, f)
	assert.Contains(t, err.Error(), "JWT bearer grant failed")
	assertLogged(t, out,
		"audience="+f.as.AuthURL(),
		"id_jag_aud=[https://auth.fabrikam.example]",
		"[oauth] Token response status=400 grant_type="+grantJWTBearer,
		"error_description=ID-JAG audience does not match this authorization server")
}

// TestConfig_EnterpriseModeAndValidate: an IdP issuer selects enterprise
// (even with a client secret), and the config must name both clients, use
// URLs the SDK accepts, request openid, and leave out auth-code options.
func TestConfig_EnterpriseModeAndValidate(t *testing.T) {
	const (
		server    = "https://mcp.contoso.example/mcp"
		idp       = "https://login.contoso.example"
		mcpClient = "mcp-tui"
	)
	base := func() *Config {
		return &Config{ServerURL: server, ClientID: mcpClient, IdPIssuer: idp, IdPClientID: idpClientID}
	}
	tests := []struct {
		name    string
		edit    func(*Config)
		wantErr string
	}{
		{name: "minimal", edit: func(*Config) {}},
		{name: "confidential clients", edit: func(c *Config) { c.ClientSecret = "s1"; c.IdPClientSecret = "s2" }},
		{name: "configured MCP auth server", edit: func(c *Config) { c.Issuer = "https://auth.contoso.example" }},
		{name: "loopback http IdP", edit: func(c *Config) { c.IdPIssuer = "http://127.0.0.1:8081" }},
		{name: "IdP scopes with openid", edit: func(c *Config) { c.IdPScopes = []string{"openid", "email"} }},
		{name: "no MCP client", edit: func(c *Config) { c.ClientID = "" },
			wantErr: "requires ClientID"},
		{name: "no IdP client", edit: func(c *Config) { c.IdPClientID = "" },
			wantErr: "requires IdPClientID"},
		{name: "plain http IdP", edit: func(c *Config) { c.IdPIssuer = "http://login.contoso.example" },
			wantErr: "IdPIssuer"},
		{name: "relative IdP", edit: func(c *Config) { c.IdPIssuer = "login.contoso.example" },
			wantErr: "IdPIssuer"},
		{name: "plain http MCP auth server", edit: func(c *Config) { c.Issuer = "http://auth.contoso.example" },
			wantErr: "Issuer"},
		{name: "IdP scopes without openid", edit: func(c *Config) { c.IdPScopes = []string{"email"} },
			wantErr: "must include openid"},
		{name: "with client metadata URL",
			edit:    func(c *Config) { c.ClientMetadataURL = "https://mcp-tui.standardbeagle.dev/client.json" },
			wantErr: "do not apply"},
		{name: "with dynamic registration", edit: func(c *Config) { c.EnableDynamicRegistration = true },
			wantErr: "do not apply"},
		{name: "with unadvertised iss", edit: func(c *Config) { c.AcceptUnadvertisedIss = true },
			wantErr: "not enterprise"},
		{name: "non-loopback redirect", edit: func(c *Config) { c.RedirectHost = "192.168.1.20" },
			wantErr: "not a loopback address"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.edit(cfg)
			assert.Equal(t, ModeEnterprise, cfg.Mode())
			err := cfg.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestConfig_IdPSettingsWithoutIssuer: IdP client or scopes without an IdP
// issuer are a mistake, not silently ignored.
func TestConfig_IdPSettingsWithoutIssuer(t *testing.T) {
	for _, cfg := range []*Config{
		{ServerURL: "https://mcp.contoso.example/mcp", ClientID: "mcp-tui-desktop", IdPClientID: idpClientID},
		{ServerURL: "https://mcp.contoso.example/mcp", ClientID: "mcp-tui-desktop", IdPScopes: []string{"openid"}},
	} {
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "require an IdP issuer")
	}
}

// TestEnterpriseFlow_CachesOnlyTheAccessToken: the MCP access token is cached
// (0600) and reused by the next run without another sign-in; the ID token
// and ID-JAG are never written. The key includes the IdP, so another IdP
// misses.
func TestEnterpriseFlow_CachesOnlyTheAccessToken(t *testing.T) {
	logs := captureAuthLogs(t)
	f := newEnterpriseFixture(t)
	dir := t.TempDir()
	cache, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	h := newEnterpriseHandler(t, f.config(), cache)
	require.NoError(t, authorizeUnauthorized(t.Context(), h, f.as))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	info, err := entries[0].Info()
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)
	assert.Contains(t, string(raw), f.as.issuedAccessToken)
	for _, notCached := range []string{f.idp.idToken, f.idp.idJAG, f.idp.idpAccessToken, f.idp.clientSecret} {
		assert.NotContains(t, string(raw), notCached)
	}

	idpRequests := f.idp.tokenRequestCount()
	again := newEnterpriseHandler(t, f.config(), cache)
	assert.Equal(t, StateAuthorized, again.Status().State, "cached token not loaded")
	src, err := again.TokenSource(t.Context())
	require.NoError(t, err)
	tok, err := src.Token()
	require.NoError(t, err)
	assert.Equal(t, f.as.issuedAccessToken, tok.AccessToken)
	assert.Equal(t, idpRequests, f.idp.tokenRequestCount(), "a cache hit must not sign in again")

	other := f.config()
	other.IdPIssuer = "https://login.fabrikam.example"
	assert.NotEqual(t, cacheKey(f.config()), cacheKey(other))

	assertLogged(t, logs(), "[oauth] Token cached", "[oauth] Token cache hit")
}
