package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// State describes the current authentication state for status indicators.
type State int

const (
	// StateIdle means no authorization has been attempted yet.
	StateIdle State = iota
	// StateAuthorizing means an Authorize() call is in flight.
	StateAuthorizing
	// StateAuthorized means a TokenSource has been installed and the most
	// recent Authorize() call succeeded.
	StateAuthorized
	// StateError means the most recent Authorize() call failed; LastError
	// holds the cause.
	StateError
)

// String returns a human-readable state name.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateAuthorizing:
		return "authorizing"
	case StateAuthorized:
		return "authorized"
	case StateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Status is a snapshot of the handler's auth state, returned by
// (*Handler).Status() for TUI/CLI consumers.
type Status struct {
	Mode      Mode
	State     State
	LastError error
}

// ErrorText renders LastError for display with every credential masked;
// token-endpoint failures can quote the whole response body. Empty when
// there is no error.
func (s Status) ErrorText() string {
	return redact.Error(s.LastError)
}

// Handler is mcp-tui's OAuth handler. It satisfies auth.OAuthHandler and
// dispatches to either the SDK's client-credentials or authorization-code
// implementation based on Config.Mode().
//
// The handler additionally exposes Status() for the TUI status indicator
// and Reauthenticate() which clears the cached token source so the next
// request triggers a fresh Authorize() call.
type Handler struct {
	cfg        *Config
	httpClient *http.Client
	cache      TokenCache

	// fetcherFactory builds the AuthorizationCodeFetcher for auth-code
	// mode. Default is newLocalServerFetcher; tests override it to install
	// a stubbed browser opener.
	fetcherFactory func(host string, port int) AuthorizationCodeFetcher

	// stepUp is set while Authorize handles a 403 (SEP-2350 step-up), so
	// selectScopes passes the challenged scope through.
	stepUp atomic.Bool

	mu       sync.Mutex
	delegate tokenSourcer
	// sdk is the SDK handler that runs Authorize. It is built on first use
	// and kept across calls: it records the scopes each issuer granted,
	// which step-up unions with the newly challenged ones. fetcher is the
	// callback server it redirects to, released by Reauthenticate.
	sdk     auth.OAuthHandler
	fetcher AuthorizationCodeFetcher
	state   State
	lastErr error
}

// AuthorizationCodeFetcher is the abstraction over the SDK's
// auth.AuthorizationCodeFetcher function type. The local-server
// implementation in this package satisfies it; tests can substitute a
// fake.
type AuthorizationCodeFetcher interface {
	RedirectURL() string
	Fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error)
	Close() error
}

// internal alias so the compile-time assertion in oauth.go has a target.
// (auth.OAuthHandler is an interface; we want to confirm we implement it.)
type handler = Handler

// NewHandler builds a Handler. httpClient may be nil, in which case a client
// with the standard 30s timeout is used; either way the handler works on a copy whose
// transport traces every auth exchange (see newAuthHTTPClient). cache may be
// nil to disable persistence.
func NewHandler(cfg *Config, httpClient *http.Client, cache TokenCache) (*Handler, error) {
	if cfg == nil {
		return nil, fmt.Errorf("oauth: config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Mode() == ModeNone {
		return nil, fmt.Errorf("oauth: config produces ModeNone (nothing to do)")
	}
	h := &Handler{
		cfg:        cfg,
		httpClient: newAuthHTTPClient(httpClient, cfg.AllowPrivateNetwork),
		cache:      cache,
		state:      StateIdle,
		fetcherFactory: func(host string, port int) AuthorizationCodeFetcher {
			return newLocalServerFetcher(host, port)
		},
	}

	logModeSelected(cfg, cache)

	// Try to populate the delegate from a cached token before any 401 hits
	// the wire. If the cache returns a usable refresh token the delegate
	// will refresh it lazily on the first TokenSource() call; if the
	// access token is still valid the SDK transport sends it on the very
	// first request and skips the round-trip Authorize() entirely.
	if err := h.tryPopulateFromCache(); err != nil {
		// Cache hits are best-effort. Surface the error to debug logs but
		// fall back to a clean Authorize() on the first 401.
		authLog().Warn("Token cache unreadable; will authorize on first 401", debug.F("error", redact.Error(err)))
		h.lastErr = err
	}

	return h, nil
}

// Mode returns the configured mode.
func (h *Handler) Mode() Mode {
	if h == nil || h.cfg == nil {
		return ModeNone
	}
	return h.cfg.Mode()
}

// Status returns a snapshot of the handler's auth state for display.
func (h *Handler) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Status{
		Mode:      h.cfg.Mode(),
		State:     h.state,
		LastError: h.lastErr,
	}
}

// Reauthenticate clears the cached delegate (and on-disk token cache, if
// configured) so the next outbound request triggers a fresh Authorize()
// call. Used by the TUI "Re-authenticate" keybinding.
func (h *Handler) Reauthenticate() error {
	h.mu.Lock()
	h.delegate = nil
	h.sdk = nil
	fetcher := h.fetcher
	h.fetcher = nil
	h.state = StateIdle
	h.lastErr = nil
	cache := h.cache
	cfg := h.cfg
	h.mu.Unlock()

	if fetcher != nil {
		if err := fetcher.Close(); err != nil {
			return fmt.Errorf("oauth: release callback listener: %w", err)
		}
	}

	if cache != nil {
		if err := cache.Delete(cacheKey(cfg)); err != nil {
			return fmt.Errorf("oauth: clear cache: %w", err)
		}
	}
	authLog().Info("Reauthentication requested; token dropped", debug.F("cache_cleared", cache != nil))
	return nil
}

// TokenSource implements auth.OAuthHandler. The SDK calls this before each
// request; we forward to whichever sub-handler ran Authorize() most
// recently. Returning a nil source instructs the transport not to add an
// Authorization header, which is the correct behavior for the very first
// request (we don't have a token yet — the server's 401 will trigger
// Authorize()).
func (h *Handler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	delegate := h.delegate
	h.mu.Unlock()
	if delegate == nil {
		return nil, nil
	}
	return delegate.TokenSource(ctx)
}

// Authorize implements auth.OAuthHandler. Dispatches to either client-
// credentials or authorization-code based on the configured mode.
func (h *Handler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	logAuthorizationRequired(req, resp)
	h.stepUp.Store(resp != nil && resp.StatusCode == http.StatusForbidden)
	h.mu.Lock()
	h.state = StateAuthorizing
	h.lastErr = nil
	h.mu.Unlock()

	delegate, err := h.sdkHandler()
	if err != nil {
		h.recordError(err)
		return err
	}

	if err := delegate.Authorize(ctx, req, resp); err != nil {
		authLog().Error("Authorization failed", debug.F("mode", h.cfg.Mode()), debug.F("error", redact.Error(err)))
		h.recordError(err)
		return err
	}

	h.mu.Lock()
	h.delegate = delegate
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()

	tok := currentToken(ctx, delegate)
	authLog().Info("Authorization succeeded",
		append([]debug.Field{debug.F("mode", h.cfg.Mode())}, tokenSummary(tok)...)...)

	// The auth-code token source saves itself (NewTokenSource). The
	// client-credentials and enterprise handlers have no such hook, and
	// their tokens are re-requested rather than refreshed (enterprise re-runs
	// the IdP flow so its policy applies each time), so only the access
	// token is cached: never the ID token or ID-JAG, which nothing reuses.
	if mode := h.cfg.Mode(); mode == ModeClientCredentials || mode == ModeEnterprise {
		h.saveSession(nil, tok)
	}
	return nil
}

func (h *Handler) recordError(err error) {
	h.mu.Lock()
	h.state = StateError
	h.lastErr = err
	h.mu.Unlock()
}

// sdkHandler returns the SDK handler for the configured grant, building it
// on first use.
func (h *Handler) sdkHandler() (auth.OAuthHandler, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sdk != nil {
		return h.sdk, nil
	}
	sdk, err := h.buildSDKHandler()
	if err != nil {
		return nil, err
	}
	h.sdk = sdk
	return sdk, nil
}

// buildSDKHandler constructs the SDK handler; h.mu is held.
func (h *Handler) buildSDKHandler() (auth.OAuthHandler, error) {
	switch h.cfg.Mode() {
	case ModeClientCredentials:
		return extauth.NewClientCredentialsHandler(&extauth.ClientCredentialsHandlerConfig{
			Credentials: h.cfg.preregistered(),
			HTTPClient:  h.httpClient,
		})
	case ModeAuthorizationCode:
		return h.buildAuthCodeHandler()
	case ModeEnterprise:
		return h.buildEnterpriseHandler()
	default:
		return nil, fmt.Errorf("oauth: unsupported mode %s", h.cfg.Mode())
	}
}

// buildAuthCodeHandler wires AuthorizationCodeHandler with a fetcher (the
// loopback callback server in production, a stub in tests) and the
// configured registration paths; h.mu is held.
func (h *Handler) buildAuthCodeHandler() (*auth.AuthorizationCodeHandler, error) {
	fetcher, redirectURL, err := h.bindFetcher()
	if err != nil {
		return nil, err
	}
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: redirectURL,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			logRegistrationResolved(h.cfg, args.URL)
			return fetcher.Fetch(ctx, args)
		},
		Client:      h.httpClient,
		ScopeFilter: h.selectScopes,
		// SEP-2207: mcp-tui keeps refresh tokens (in the token cache), so it
		// asks for offline_access when the AS lists it in scopes_supported.
		RequestRefreshToken:   true,
		AcceptUnadvertisedIss: h.cfg.AcceptUnadvertisedIss,
		// Every token the SDK obtains, and every refresh of it, is written
		// back to the cache with the client and token endpoint a later run
		// needs to refresh it.
		NewTokenSource: func(ctx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			h.saveSession(oc, tok)
			return h.newSavingTokenSource(ctx, oc, tok), nil
		},
	}
	if h.cfg.ClientMetadataURL != "" {
		cfg.ClientIDMetadataDocumentConfig = &auth.ClientIDMetadataDocumentConfig{URL: h.cfg.ClientMetadataURL}
	}
	if pre := h.cfg.preregistered(); pre != nil {
		cfg.PreregisteredClient = pre
	}
	if h.cfg.EnableDynamicRegistration {
		cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   "mcp-tui",
				RedirectURIs: []string{redirectURL},
				GrantTypes:   []string{"authorization_code", "refresh_token"},
			},
		}
	}
	authLog().Info("Authorization-code handler ready",
		debug.F("redirect_url", redirectURL),
		debug.F("request_refresh_token", cfg.RequestRefreshToken),
		debug.F("registration_order", registrationOrder(h.cfg)))
	return auth.NewAuthorizationCodeHandler(cfg)
}

// buildEnterpriseHandler wires the SEP-990 flow to the loopback callback
// server for the IdP sign-in; h.mu is held.
func (h *Handler) buildEnterpriseHandler() (*enterpriseAuthorizer, error) {
	fetcher, redirectURL, err := h.bindFetcher()
	if err != nil {
		return nil, err
	}
	authLog().Info("Enterprise handler ready",
		debug.F("redirect_url", redirectURL),
		debug.F("idp_issuer", h.cfg.IdPIssuer),
		debug.F("mcp_scopes", h.cfg.scopeList()))
	return &enterpriseAuthorizer{h: h, fetcher: fetcher, redirectURL: redirectURL}, nil
}

// bindFetcher builds the callback fetcher (the loopback server in
// production, a stub in tests), binds its redirect URL and records it for
// Reauthenticate to release; h.mu is held.
func (h *Handler) bindFetcher() (AuthorizationCodeFetcher, string, error) {
	fetcher := h.fetcherFactory(h.cfg.RedirectHost, h.cfg.RedirectPort)
	redirectURL := fetcher.RedirectURL()
	if redirectURL == "" {
		return nil, "", fmt.Errorf("oauth: failed to bind callback listener (host=%s, port=%d)",
			h.cfg.RedirectHost, h.cfg.RedirectPort)
	}
	h.fetcher = fetcher
	return fetcher, redirectURL, nil
}

// selectScopes is the SDK's ScopeFilter: it picks the scopes to request
// from those the SDK discovered (WWW-Authenticate challenge, else PRM
// scopes_supported). Configured scopes (--oauth-scopes) replace the
// discovered set, except on step-up, where the discovered set is the
// challenged scope and must be requested. The SDK adds offline_access and
// unions in the scopes already granted after this.
func (h *Handler) selectScopes(discovered []string) []string {
	source, selected := "discovered", discovered
	switch configured := h.cfg.scopeList(); {
	case h.stepUp.Load():
		source = "step_up"
	case len(configured) > 0:
		source, selected = "configured", configured
	}
	authLog().Info("Scopes selected",
		debug.F("source", source),
		debug.F("discovered", discovered),
		debug.F("selected", selected))
	return selected
}

// tryPopulateFromCache looks up a cached session and, on hit, installs its
// token source so the very first request goes out with an Authorization
// header. A session that carries its client and token endpoint refreshes
// an expired access token with its refresh token and writes the new token
// back; a client-credentials session is replayed until the server 401s.
func (h *Handler) tryPopulateFromCache() error {
	if h.cache == nil {
		return nil
	}
	session, err := h.cache.Load(cacheKey(h.cfg))
	if err != nil {
		return err
	}
	if session == nil {
		authLog().Info("Token cache miss")
		return nil
	}
	authLog().Info("Token cache hit", append(tokenSummary(session.Token),
		debug.F("expired", !session.Token.Expiry.IsZero() && session.Token.Expiry.Before(time.Now())),
		debug.F("refreshable", session.Client != nil))...)

	src := oauth2.StaticTokenSource(session.Token)
	if session.Client != nil {
		oc := session.Client.oauth2Config()
		refreshCtx := context.WithValue(context.Background(), oauth2.HTTPClient, h.httpClient)
		src = h.newSavingTokenSource(refreshCtx, oc, session.Token)
	}
	h.mu.Lock()
	h.delegate = &cachedDelegate{src: src}
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()
	return nil
}

// newSavingTokenSource returns oc's refreshing token source for tok, wrapped
// so each new token it produces is saved to the cache. ctx carries the auth
// HTTP client and outlives any one request: refreshes happen long after.
func (h *Handler) newSavingTokenSource(ctx context.Context, oc *oauth2.Config, tok *oauth2.Token) oauth2.TokenSource {
	s := &savingTokenSource{src: oc.TokenSource(ctx, tok), save: func(t *oauth2.Token) { h.saveSession(oc, t) }}
	s.last.Store(tok)
	return s
}

// savingTokenSource calls save whenever the wrapped source yields a token
// with a new access token (a refresh). Swap makes the check-and-record
// atomic, so concurrent callers save a refreshed token once.
type savingTokenSource struct {
	src  oauth2.TokenSource
	save func(*oauth2.Token)
	last atomic.Pointer[oauth2.Token]
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if prev := s.last.Swap(tok); prev == nil || prev.AccessToken != tok.AccessToken {
		s.save(tok)
	}
	return tok, nil
}

// oauth2Config rebuilds the refresh configuration of a cached session.
func (c *SessionClient) oauth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: c.TokenURL, AuthStyle: c.AuthStyle},
		Scopes:       c.Scopes,
	}
}

// currentToken returns the token the delegate currently holds, or nil when
// it cannot produce one (the failure is logged, not returned: the request can
// still complete with whatever the SDK holds in memory).
func currentToken(ctx context.Context, delegate tokenSourcer) *oauth2.Token {
	src, err := delegate.TokenSource(ctx)
	if err != nil || src == nil {
		authLog().Warn("No token source after authorization", debug.F("error", redact.Error(err)))
		return nil
	}
	tok, err := src.Token()
	if err != nil {
		authLog().Warn("Token source failed after authorization", debug.F("error", redact.Error(err)))
		return nil
	}
	return tok
}

// saveSession writes tok to the cache, with the client and token endpoint
// from oc when the token can be refreshed (oc nil: client-credentials).
// Failures are logged, not returned: the request can still complete with
// the in-memory token.
func (h *Handler) saveSession(oc *oauth2.Config, tok *oauth2.Token) {
	if h.cache == nil || tok == nil {
		return
	}
	session := &Session{Token: tok}
	if oc != nil {
		session.Client = &SessionClient{
			ClientID:     oc.ClientID,
			ClientSecret: oc.ClientSecret,
			TokenURL:     oc.Endpoint.TokenURL,
			AuthStyle:    oc.Endpoint.AuthStyle,
			Scopes:       oc.Scopes,
		}
	}
	if err := h.cache.Save(cacheKey(h.cfg), session); err != nil {
		authLog().Warn("Token cache save failed", debug.F("error", redact.Error(err)))
		return
	}
	authLog().Info("Token cached", append(tokenSummary(tok), debug.F("refreshable", session.Client != nil))...)
}

// tokenSummary describes a token without any of its credential values.
func tokenSummary(tok *oauth2.Token) []debug.Field {
	if tok == nil {
		return []debug.Field{debug.F("token", noneValue)}
	}
	scope, ok := tok.Extra("scope").(string)
	if !ok {
		scope = ""
	}
	return []debug.Field{
		debug.F("token_type", tok.Type()),
		debug.F("expiry", tok.Expiry),
		debug.F("granted_scope", scope),
		debug.F("has_refresh_token", tok.RefreshToken != ""),
	}
}

// Client registration paths, in the order the SDK tries them.
const (
	registrationCIMD          = "client_id_metadata_document"
	registrationPreregistered = "preregistered"
	registrationDynamic       = "dynamic"
)

// registrationOrder lists the configured ways the client can identify
// itself to the AS, in the SDK's order of preference: a Client ID Metadata
// Document (used only when the AS advertises support), then a
// pre-registered client, then dynamic registration.
func registrationOrder(cfg *Config) []string {
	order := make([]string, 0, 3)
	if cfg.ClientMetadataURL != "" {
		order = append(order, registrationCIMD)
	}
	if cfg.ClientID != "" {
		order = append(order, registrationPreregistered)
	}
	if cfg.EnableDynamicRegistration && cfg.Mode() == ModeAuthorizationCode {
		order = append(order, registrationDynamic)
	}
	return order
}

// logRegistrationResolved records which registration path the SDK took and
// why, read off the client_id in the authorization URL: the SDK picks the
// first path in registrationOrder the AS supports, so a later path means
// every earlier one was unsupported.
func logRegistrationResolved(cfg *Config, authURL string) {
	u, err := url.Parse(authURL)
	if err != nil {
		authLog().Warn("Authorization URL unparseable", debug.F("error", redact.Error(err)))
		return
	}
	clientID := u.Query().Get("client_id")
	path := registrationDynamic
	switch clientID {
	case cfg.ClientMetadataURL:
		path = registrationCIMD
	case cfg.ClientID:
		path = registrationPreregistered
	}
	authLog().Info("Client registration resolved",
		debug.F("path", path),
		debug.F("reason", registrationReason(cfg, path)),
		debug.F("client_id", clientID))
}

func registrationReason(cfg *Config, path string) string {
	switch path {
	case registrationCIMD:
		return "authorization server advertises client_id_metadata_document_supported"
	case registrationPreregistered:
		if cfg.ClientMetadataURL != "" {
			return "authorization server does not advertise client_id_metadata_document_supported"
		}
		return "configured client ID"
	default:
		if cfg.ClientMetadataURL != "" {
			return "authorization server does not advertise client_id_metadata_document_supported; no pre-registered client"
		}
		return "no pre-registered client"
	}
}

// logModeSelected records which grant the configuration selected and the
// inputs that decided it.
func logModeSelected(cfg *Config, cache TokenCache) {
	_, cacheDisabled := cache.(NoopCache)
	fields := []debug.Field{
		debug.F("mode", cfg.Mode()),
		debug.F("server_url", redact.URL(cfg.ServerURL)),
		debug.F("registration_order", registrationOrder(cfg)),
		debug.F("confidential_client", cfg.ClientSecret != ""),
		debug.F("configured_scopes", cfg.scopeList()),
		debug.F("client_issuer", cfg.Issuer),
		debug.F("token_cache", cache != nil && !cacheDisabled),
		debug.F("allow_private_network", cfg.AllowPrivateNetwork),
	}
	if cfg.Mode() == ModeEnterprise {
		fields = append(fields, enterpriseModeFields(cfg)...)
	}
	authLog().Info("OAuth mode selected", fields...)
	if cfg.AllowPrivateNetwork {
		authLog().Warn("Auth requests may reach private-network addresses (--oauth-allow-private-network); " +
			"a server's metadata can then point mcp-tui at internal hosts")
	}
	if cfg.AcceptUnadvertisedIss {
		authLog().Warn("Accepting unadvertised RFC 9207 iss (--oauth-accept-unadvertised-iss); " +
			"meant for testing non-conforming servers only")
	}
}

// logAuthorizationRequired records the 401/403 that made the SDK call
// Authorize, with its redacted challenge.
func logAuthorizationRequired(req *http.Request, resp *http.Response) {
	fields := []debug.Field{}
	if resp != nil {
		challenges := make([]string, 0, len(resp.Header.Values("WWW-Authenticate")))
		for _, c := range resp.Header.Values("WWW-Authenticate") {
			challenges = append(challenges, redact.Challenge(c))
		}
		fields = append(fields, debug.F("status", resp.StatusCode), debug.F("www_authenticate", challenges))
	}
	if req != nil {
		fields = append(fields, debug.F("method", req.Method), debug.F("url", redact.RedactedURL(req.URL)))
	}
	authLog().Info("Authorization required", fields...)
}

// tokenSourcer is the part of auth.OAuthHandler Handler.TokenSource
// forwards to. Handler.Authorize always runs the SDK handler (sdkHandler),
// never the delegate, so the delegate need not authorize.
type tokenSourcer interface {
	TokenSource(context.Context) (oauth2.TokenSource, error)
}

// cachedDelegate serves a token hot-loaded from the on-disk cache. When
// the server rejects it, the SDK calls Handler.Authorize, which replaces
// this delegate with the SDK handler that ran the flow.
type cachedDelegate struct {
	src oauth2.TokenSource
}

func (c *cachedDelegate) TokenSource(_ context.Context) (oauth2.TokenSource, error) {
	return c.src, nil
}
