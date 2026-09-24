package oauth

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// TestCachedSession_ExpiredIsRefreshedAndSaved: a cached session whose
// access token has expired is refreshed with its refresh token (not
// replayed), and the new token is written back to the cache.
func TestCachedSession_ExpiredIsRefreshedAndSaved(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	cfg := &Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, CachePath: t.TempDir()}
	cache, err := NewFileTokenCache(cfg.CachePath)
	require.NoError(t, err)
	require.NoError(t, cache.Save(cacheKey(cfg), &Session{
		Token: &oauth2.Token{
			AccessToken: "expired_access_token", RefreshToken: "test_refresh_token", TokenType: "Bearer",
			Expiry: time.Now().Add(-time.Hour),
		},
		Client: &SessionClient{ClientID: srv.clientID, TokenURL: srv.AuthURL() + "/token", AuthStyle: oauth2.AuthStyleInParams},
	}))

	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	src, err := h.TokenSource(t.Context())
	require.NoError(t, err)
	tok, err := src.Token()
	require.NoError(t, err)
	assert.Equal(t, srv.issuedAccessToken+"_refreshed", tok.AccessToken)
	assert.Equal(t, "refresh_token", srv.lastTokenRequest().Get("grant_type"))

	saved, err := cache.Load(cacheKey(cfg))
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, srv.issuedAccessToken+"_refreshed", saved.Token.AccessToken, "refreshed token must be written back")

	out := logs()
	assertNoSecrets(t, out, append(srv.issuedSecrets(), "expired_access_token"))
	assertLogged(t, out,
		"[oauth] Token cache hit",
		"expired=true refreshable=true",
		"[oauth] Token refresh grant_type=refresh_token",
		"[oauth] Token cached",
		"has_refresh_token=true refreshable=true")
}

// TestAuthorizationCodeFlow_SessionSurvivesRestart runs DCR + auth-code with
// a confidential registered client, then a second handler (the next
// mcp-tui run) finds the session expired and refreshes it with the client
// credentials the first run registered.
func TestAuthorizationCodeFlow_SessionSurvivesRestart(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.allowDCR = true
	srv.requireSecretOnRefresh = true
	cfg := &Config{ServerURL: srv.ResourceURL(), EnableDynamicRegistration: true, CachePath: t.TempDir()}
	cache, err := NewFileTokenCache(cfg.CachePath)
	require.NoError(t, err)

	first, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	installAutoApproveFetcher(t, first)
	driveAuthCode(t, first, srv)
	require.Equal(t, StateAuthorized, first.Status().State, "flow failed: %v", first.Status().LastError)

	saved, err := cache.Load(cacheKey(cfg))
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.NotNil(t, saved.Client, "an auth-code session must carry its refresh client")
	assert.Equal(t, srv.clientID, saved.Client.ClientID)
	assert.Equal(t, srv.AuthURL()+"/token", saved.Client.TokenURL)

	// Age the token as if the next run started after it expired.
	saved.Token.Expiry = time.Now().Add(-time.Minute)
	require.NoError(t, cache.Save(cacheKey(cfg), saved))

	second, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	src, err := second.TokenSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src, "the cached session must be installed before any 401")
	tok, err := src.Token()
	require.NoError(t, err)
	assert.Equal(t, srv.issuedAccessToken+"_refreshed", tok.AccessToken)
	assert.Equal(t, srv.clientSecret, srv.lastTokenRequest().Get("client_secret"),
		"the refresh must authenticate with the registered client's secret")

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out, "[oauth] Token refresh grant_type=refresh_token", "client_auth=client_secret_post")
}

// TestClientCredentials_CachesTokenWithoutClient: a client-credentials
// token is re-requested, not refreshed, so its session carries no client.
func TestClientCredentials_CachesTokenWithoutClient(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	cfg := &Config{ServerURL: srv.ResourceURL(), ClientID: srv.clientID, ClientSecret: srv.clientSecret, CachePath: t.TempDir()}
	cache, err := NewFileTokenCache(cfg.CachePath)
	require.NoError(t, err)

	h, err := NewHandler(cfg, http.DefaultClient, cache)
	require.NoError(t, err)
	driveClientCredentials(t, h, srv)

	saved, err := cache.Load(cacheKey(cfg))
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, srv.issuedAccessToken, saved.Token.AccessToken)
	assert.Nil(t, saved.Client)
	assertNoSecrets(t, logs(), srv.issuedSecrets())
}
